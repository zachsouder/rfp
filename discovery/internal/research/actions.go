package research

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	fetchTimeout     = 15 * time.Second
	maxContentLength = 512 * 1024 // 512KB
	maxTextLength    = 15000      // Truncate for API calls
	userAgent        = "Mozilla/5.0 (compatible; RFPBot/1.0)"
)

// actionFetchPage fetches and processes the page content.
func (a *Agent) actionFetchPage(ctx context.Context, rc *ResearchContext) error {
	client := &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rc.CurrentURL, nil)
	if err != nil {
		rc.fetchFailed = true
		rc.fetchError = err.Error()
		return err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		rc.fetchFailed = true
		rc.fetchError = err.Error()
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		rc.fetchFailed = true
		rc.fetchError = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Update current URL after redirects
	rc.CurrentURL = resp.Request.URL.String()

	// Read body
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxContentLength))
	if err != nil {
		rc.fetchFailed = true
		rc.fetchError = err.Error()
		return err
	}

	// Convert HTML to text
	rc.PageContent = htmlToText(string(bodyBytes))

	return nil
}

// actionExtractDetails uses Gemini to extract structured RFP details.
func (a *Agent) actionExtractDetails(ctx context.Context, rc *ResearchContext) (int, error) {
	details, tokens, err := a.geminiClient.ExtractRFPDetails(ctx, rc.CurrentURL, rc.PageContent)
	if err != nil {
		return tokens, err
	}

	rc.ExtractedDetails = details
	return tokens, nil
}

// actionDiscoverPDFs finds PDF links in the page content.
func (a *Agent) actionDiscoverPDFs(rc *ResearchContext) {
	rc.pdfSearchDone = true

	// Pattern to match PDF URLs
	pdfPattern := regexp.MustCompile(`(?i)href\s*=\s*["']([^"']*\.pdf[^"']*)["']`)
	matches := pdfPattern.FindAllStringSubmatch(rc.PageContent, -1)

	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) > 1 {
			url := match[1]
			// Normalize URL
			if !strings.HasPrefix(url, "http") {
				// Relative URL - would need base URL handling
				continue
			}
			if !seen[url] {
				seen[url] = true
				rc.FoundPDFs = append(rc.FoundPDFs, url)
			}
		}
	}
}

// htmlToText converts HTML to plain text.
func htmlToText(htmlContent string) string {
	// Remove scripts and styles
	scriptPattern := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	stylePattern := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)

	htmlContent = scriptPattern.ReplaceAllString(htmlContent, "")
	htmlContent = stylePattern.ReplaceAllString(htmlContent, "")

	// Parse HTML and extract text
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		// Fallback: strip all tags
		tagPattern := regexp.MustCompile(`<[^>]+>`)
		text := tagPattern.ReplaceAllString(htmlContent, " ")
		return cleanText(text)
	}

	var sb strings.Builder
	extractText(doc, &sb)
	text := sb.String()

	return cleanText(text)
}

// extractText recursively extracts text from HTML nodes.
func extractText(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
		sb.WriteString(" ")
	}
	if n.Type == html.ElementNode {
		// Add newlines for block elements
		switch n.Data {
		case "p", "div", "br", "li", "h1", "h2", "h3", "h4", "h5", "h6", "tr":
			sb.WriteString("\n")
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractText(c, sb)
	}
}

// cleanText normalizes whitespace and truncates text.
func cleanText(text string) string {
	// Decode HTML entities
	text = html.UnescapeString(text)

	// Normalize whitespace
	spacePattern := regexp.MustCompile(`[ \t]+`)
	text = spacePattern.ReplaceAllString(text, " ")

	newlinePattern := regexp.MustCompile(`\n{3,}`)
	text = newlinePattern.ReplaceAllString(text, "\n\n")

	text = strings.TrimSpace(text)

	// Truncate if needed
	if len(text) > maxTextLength {
		text = text[:maxTextLength] + "\n...[truncated]"
	}

	return text
}

// Login wall detection indicators
var loginIndicators = []string{
	"sign in",
	"log in",
	"login",
	"signin",
	"create account",
	"register",
	"authentication required",
	"access denied",
	"subscription required",
	"members only",
	"please log in",
	"login to view",
	"sign in to continue",
}

// detectLoginWall checks if the page content indicates a login requirement.
func detectLoginWall(content string) bool {
	contentLower := strings.ToLower(content)

	for _, indicator := range loginIndicators {
		pos := strings.Index(contentLower, indicator)
		if pos == -1 {
			continue
		}

		// Check surrounding context for form-related terms
		start := pos - 100
		if start < 0 {
			start = 0
		}
		end := pos + 100
		if end > len(contentLower) {
			end = len(contentLower)
		}

		surrounding := contentLower[start:end]

		// If found with form-related terms, likely a login wall
		formTerms := regexp.MustCompile(`(form|password|email|username|required)`)
		if formTerms.MatchString(surrounding) {
			return true
		}
	}

	return false
}
