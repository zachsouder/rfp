// Package search provides Gemini API integration with Google Search grounding
// for discovering RFP listings.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zachsouder/rfp/shared/models"
)

const (
	defaultModel  = "gemini-3-flash-preview"
	baseURL       = "https://generativelanguage.googleapis.com/v1beta"
	defaultTimeout = 60 * time.Second
)

// Client handles Gemini API calls with search grounding.
type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewClient creates a new Gemini search client.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  defaultModel,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// WithModel sets a custom model for the client.
func (c *Client) WithModel(model string) *Client {
	c.model = model
	return c
}

// Result represents a single search result from Gemini grounding.
type Result struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Source  string `json:"source"` // grounding_chunk or text_extraction
}

// SearchResponse contains the results of a search operation.
type SearchResponse struct {
	Query        string        `json:"query"`
	Model        string        `json:"model"`
	Results      []Result      `json:"results"`
	ResultsCount int           `json:"results_count"`
	TokensUsed   int           `json:"tokens_used"`
	DurationMs   int64         `json:"duration_ms"`
}

// geminiRequest represents the request payload to Gemini API.
type geminiRequest struct {
	Contents         []geminiContent         `json:"contents"`
	Tools            []geminiTool            `json:"tools"`
	GenerationConfig geminiGenerationConfig  `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiTool struct {
	GoogleSearch *struct{} `json:"google_search"`
}

type geminiGenerationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

// geminiResponse represents the response from Gemini API.
type geminiResponse struct {
	Candidates    []geminiCandidate   `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content           *geminiCandidateContent `json:"content,omitempty"`
	GroundingMetadata *groundingMetadata      `json:"groundingMetadata,omitempty"`
}

type geminiCandidateContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}

type groundingMetadata struct {
	GroundingChunks   []groundingChunk   `json:"groundingChunks,omitempty"`
	GroundingSupports []groundingSupport `json:"groundingSupports,omitempty"`
}

type groundingChunk struct {
	Web *webChunk `json:"web,omitempty"`
}

type webChunk struct {
	URI   string `json:"uri"`
	Title string `json:"title"`
}

type groundingSupport struct {
	Segment              *segment `json:"segment,omitempty"`
	GroundingChunkIndices []int   `json:"groundingChunkIndices,omitempty"`
}

type segment struct {
	Text string `json:"text"`
}

// Search executes a search query using Gemini with Google Search grounding.
func (c *Client) Search(ctx context.Context, query string) (*SearchResponse, error) {
	startTime := time.Now()

	prompt := buildSearchPrompt(query)
	resp, err := c.callGeminiWithGrounding(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("gemini API call failed: %w", err)
	}

	results := parseGroundingResults(resp)
	durationMs := time.Since(startTime).Milliseconds()

	tokensUsed := 0
	if resp.UsageMetadata != nil {
		tokensUsed = resp.UsageMetadata.PromptTokenCount + resp.UsageMetadata.CandidatesTokenCount
	}

	return &SearchResponse{
		Query:        query,
		Model:        c.model,
		Results:      results,
		ResultsCount: len(results),
		TokensUsed:   tokensUsed,
		DurationMs:   durationMs,
	}, nil
}

// ExecuteQueries runs multiple search queries from configs.
func (c *Client) ExecuteQueries(ctx context.Context, configs []models.SearchQueryConfig) ([]SearchResponse, error) {
	var responses []SearchResponse

	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}

		resp, err := c.Search(ctx, cfg.QueryTemplate)
		if err != nil {
			// Log error but continue with other queries
			responses = append(responses, SearchResponse{
				Query:        cfg.QueryTemplate,
				Model:        c.model,
				Results:      nil,
				ResultsCount: 0,
			})
			continue
		}

		responses = append(responses, *resp)

		// Rate limiting between queries
		select {
		case <-ctx.Done():
			return responses, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return responses, nil
}

// buildSearchPrompt constructs the prompt for Gemini search.
func buildSearchPrompt(query string) string {
	return fmt.Sprintf(`Search the web for: %s

Find relevant RFP (Request for Proposal) listings related to parking services. For each result found, I need:
- The exact URL of the listing
- The title of the page
- A brief description or snippet

Focus on actual procurement listings from portals like Bonfire, OpenGov, PlanetBids, BidNet, or government agency websites.`, query)
}

// callGeminiWithGrounding makes the API call with search grounding enabled.
func (c *Client) callGeminiWithGrounding(ctx context.Context, prompt string) (*geminiResponse, error) {
	endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s", baseURL, c.model, c.apiKey)

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
		Tools: []geminiTool{
			{GoogleSearch: &struct{}{}},
		},
		GenerationConfig: geminiGenerationConfig{
			Temperature:     0.1,
			MaxOutputTokens: 4096,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &geminiResp, nil
}

// parseGroundingResults extracts search results from Gemini response.
func parseGroundingResults(resp *geminiResponse) []Result {
	var results []Result
	seenURLs := make(map[string]bool)

	// Extract from grounding metadata (most reliable source)
	if len(resp.Candidates) > 0 && resp.Candidates[0].GroundingMetadata != nil {
		meta := resp.Candidates[0].GroundingMetadata
		for _, chunk := range meta.GroundingChunks {
			if chunk.Web != nil && chunk.Web.URI != "" {
				cleanedURL := cleanURL(chunk.Web.URI)
				if cleanedURL == "" || seenURLs[cleanedURL] {
					continue
				}
				// Skip Google redirect URLs
				if strings.Contains(cleanedURL, "vertexaisearch.cloud.google.com") {
					continue
				}
				seenURLs[cleanedURL] = true
				results = append(results, Result{
					URL:     cleanedURL,
					Title:   chunk.Web.Title,
					Snippet: "",
					Source:  "grounding_chunk",
				})
			}
		}
	}

	// Also extract URLs from response text as fallback
	if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
		for _, part := range resp.Candidates[0].Content.Parts {
			textResults := extractURLsFromText(part.Text)
			for _, r := range textResults {
				if !seenURLs[r.URL] {
					seenURLs[r.URL] = true
					results = append(results, r)
				}
			}
		}
	}

	return results
}

// extractURLsFromText finds URLs in response text.
func extractURLsFromText(text string) []Result {
	var results []Result

	// Match HTTP(S) URLs
	urlPattern := regexp.MustCompile(`https?://[^\s\)\]"'<>]+`)
	matches := urlPattern.FindAllString(text, -1)

	for _, match := range matches {
		cleanedURL := cleanURL(match)
		if cleanedURL == "" {
			continue
		}
		// Skip Google redirect URLs
		if strings.Contains(cleanedURL, "vertexaisearch.cloud.google.com") {
			continue
		}
		results = append(results, Result{
			URL:     cleanedURL,
			Title:   extractTitleFromURL(cleanedURL),
			Snippet: "",
			Source:  "text_extraction",
		})
	}

	return results
}

// cleanURL removes artifacts and normalizes the URL.
func cleanURL(rawURL string) string {
	// Remove trailing punctuation
	rawURL = strings.TrimRight(rawURL, ".,;:!?)]")

	// Handle markdown artifacts: url](url -> url
	if idx := strings.Index(rawURL, "]("); idx != -1 {
		rawURL = rawURL[:idx]
	}

	// Remove URL-encoded brackets at the end
	rawURL = strings.TrimSuffix(rawURL, "%5B")
	rawURL = strings.TrimSuffix(rawURL, "%5D")

	return canonicalizeURL(rawURL)
}

// canonicalizeURL normalizes a URL for deduplication.
func canonicalizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return ""
	}

	// Normalize scheme and host
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)

	// Remove default ports
	if (parsed.Scheme == "http" && parsed.Port() == "80") ||
		(parsed.Scheme == "https" && parsed.Port() == "443") {
		parsed.Host = parsed.Hostname()
	}

	// Remove tracking parameters
	if parsed.RawQuery != "" {
		params := parsed.Query()
		trackingParams := []string{
			"utm_source", "utm_medium", "utm_campaign",
			"utm_term", "utm_content", "fbclid",
			"gclid", "ref",
		}
		for _, p := range trackingParams {
			params.Del(p)
		}
		// Sort remaining params for consistency
		var keys []string
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			parsed.RawQuery = params.Encode()
		} else {
			parsed.RawQuery = ""
		}
	}

	return parsed.String()
}

// extractTitleFromURL creates a title from URL path.
func extractTitleFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return parsed.Host
	}

	parts := strings.Split(path, "/")
	lastPart := parts[len(parts)-1]

	// Clean up the last part for display
	title := strings.ReplaceAll(lastPart, "-", " ")
	title = strings.ReplaceAll(title, "_", " ")
	// Remove file extension
	if idx := strings.LastIndex(title, "."); idx != -1 {
		title = title[:idx]
	}

	// URL decode
	if decoded, err := url.PathUnescape(title); err == nil {
		title = decoded
	}

	return fmt.Sprintf("%s (%s)", strings.Title(title), parsed.Host)
}

// DefaultQueryConfigs returns default search query templates when none are configured.
func DefaultQueryConfigs() []models.SearchQueryConfig {
	return []models.SearchQueryConfig{
		{Name: "Bonfire Portal", QueryTemplate: "parking RFP site:bonfirehub.com", Enabled: true},
		{Name: "OpenGov Portal", QueryTemplate: "parking solicitation site:opengov.com", Enabled: true},
		{Name: "PlanetBids Portal", QueryTemplate: "parking RFP site:planetbids.com", Enabled: true},
		{Name: "PARCS Keyword", QueryTemplate: "PARCS parking access revenue control RFP", Enabled: true},
		{Name: "Management Services", QueryTemplate: "parking management services RFP", Enabled: true},
		{Name: "Garage Operations", QueryTemplate: "parking garage operations bid", Enabled: true},
		{Name: "Event Parking", QueryTemplate: "event parking stadium arena RFP", Enabled: true},
		{Name: "Municipal Parking", QueryTemplate: "municipal parking RFP", Enabled: true},
	}
}
