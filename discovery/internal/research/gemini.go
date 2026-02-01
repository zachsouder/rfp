package research

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	geminiBaseURL    = "https://generativelanguage.googleapis.com/v1beta"
	geminiTimeout    = 30 * time.Second
)

// GeminiClient handles Gemini API calls for structured extraction.
type GeminiClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewGeminiClient creates a new Gemini client.
func NewGeminiClient(apiKey string) *GeminiClient {
	return &GeminiClient{
		apiKey: apiKey,
		model:  defaultModel,
		httpClient: &http.Client{
			Timeout: geminiTimeout,
		},
	}
}

// geminiRequest represents the request payload.
type geminiRequest struct {
	Contents         []geminiContent         `json:"contents"`
	GenerationConfig geminiGenerationConfig  `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	Temperature      float64          `json:"temperature"`
	ResponseMimeType string           `json:"responseMimeType,omitempty"`
	ResponseSchema   *json.RawMessage `json:"responseSchema,omitempty"`
}

// geminiResponse represents the response from Gemini.
type geminiResponse struct {
	Candidates    []geminiCandidate   `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content *geminiCandidateContent `json:"content,omitempty"`
}

type geminiCandidateContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}

// ExtractRFPDetails uses Gemini to extract structured RFP information.
func (c *GeminiClient) ExtractRFPDetails(ctx context.Context, pageURL, pageContent string) (*ExtractedDetails, int, error) {
	prompt := fmt.Sprintf(`Extract RFP (Request for Proposal) details from this page content:

Page URL: %s
Page Content:
%s

Extract the following if present:
- title: The RFP title/name
- agency: The issuing agency/organization
- location_city: City
- location_state: State (2-letter code)
- due_date: Due date/deadline (YYYY-MM-DD format if possible)
- scope_summary: Brief summary of what's being requested
- estimated_value: Budget/contract value if mentioned
- incumbent: Current contractor if mentioned
- category: Type of service (parking, valet, event_ops, transit, enforcement, etc.)
- venue_type: Type of venue (arena, stadium, convention_center, airport, municipal, etc.)

Return as JSON. Use null for fields that are not found.`, pageURL, pageContent)

	// Define the response schema
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"title": {"type": "string"},
			"agency": {"type": "string"},
			"location_city": {"type": "string"},
			"location_state": {"type": "string"},
			"due_date": {"type": "string"},
			"scope_summary": {"type": "string"},
			"estimated_value": {"type": "string"},
			"incumbent": {"type": "string"},
			"category": {"type": "string"},
			"venue_type": {"type": "string"}
		}
	}`)

	resp, tokens, err := c.callStructured(ctx, prompt, &schema)
	if err != nil {
		return nil, tokens, err
	}

	// Parse the response into ExtractedDetails
	var details ExtractedDetails
	if err := json.Unmarshal([]byte(resp), &details); err != nil {
		return nil, tokens, fmt.Errorf("failed to parse extracted details: %w", err)
	}

	return &details, tokens, nil
}

// callStructured makes a Gemini API call with structured JSON output.
func (c *GeminiClient) callStructured(ctx context.Context, prompt string, schema *json.RawMessage) (string, int, error) {
	endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s", geminiBaseURL, c.model, c.apiKey)

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			Temperature:      0.1,
			ResponseMimeType: "application/json",
			ResponseSchema:   schema,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return "", 0, fmt.Errorf("failed to parse response: %w", err)
	}

	// Calculate tokens used
	tokens := 0
	if geminiResp.UsageMetadata != nil {
		tokens = geminiResp.UsageMetadata.PromptTokenCount + geminiResp.UsageMetadata.CandidatesTokenCount
	}

	// Extract text from response
	if len(geminiResp.Candidates) == 0 || geminiResp.Candidates[0].Content == nil {
		return "{}", tokens, nil
	}

	var text string
	for _, part := range geminiResp.Candidates[0].Content.Parts {
		text += part.Text
	}

	return text, tokens, nil
}
