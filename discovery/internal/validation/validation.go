// Package validation provides URL validation for search results,
// detecting hallucinated URLs and following redirects.
package validation

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	defaultTimeout     = 15 * time.Second
	connectTimeout     = 10 * time.Second
	maxRedirects       = 10
	defaultUserAgent   = "Mozilla/5.0 (compatible; RFPBot/1.0)"
	maxBodyReadBytes   = 512 * 1024 // 512KB for content detection
)

// Status represents the validation status of a URL.
type Status string

const (
	StatusValid            Status = "valid"
	StatusValidRedirected  Status = "valid_redirected"
	StatusInvalidURL       Status = "invalid_url"
	StatusNotFound         Status = "not_found"
	StatusClientError      Status = "client_error"
	StatusServerError      Status = "server_error"
	StatusConnectionFailed Status = "connection_failed"
	StatusConnectionRefused Status = "connection_refused"
	StatusDNSError         Status = "dns_error"
	StatusTimeout          Status = "timeout"
	StatusSSLError         Status = "ssl_error"
	StatusTooManyRedirects Status = "too_many_redirects"
)

// ContentType represents the detected type of content at the URL.
type ContentType string

const (
	ContentTypeRFPPage       ContentType = "rfp_page"
	ContentTypePortalListing ContentType = "portal_listing"
	ContentTypeLoginWall     ContentType = "login_wall"
	ContentTypePDF           ContentType = "pdf"
	ContentTypeOther         ContentType = "other"
)

// Result contains the results of URL validation.
type Result struct {
	Valid          bool        `json:"valid"`
	Status         Status      `json:"status"`
	Error          string      `json:"error,omitempty"`
	HTTPCode       int         `json:"http_code,omitempty"`
	FinalURL       string      `json:"final_url,omitempty"`
	RedirectCount  int         `json:"redirect_count,omitempty"`
	WasRedirected  bool        `json:"was_redirected,omitempty"`
	ContentType    ContentType `json:"content_type,omitempty"`
	ContentMIME    string      `json:"content_mime,omitempty"`
	DurationMs     int64       `json:"duration_ms,omitempty"`
}

// Validator handles URL validation.
type Validator struct {
	httpClient *http.Client
	userAgent  string
}

// NewValidator creates a new URL validator.
func NewValidator() *Validator {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		DialContext: (&net.Dialer{
			Timeout: connectTimeout,
		}).DialContext,
		DisableKeepAlives:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: defaultTimeout,
	}

	return &Validator{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   defaultTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
		userAgent: defaultUserAgent,
	}
}

// Validate checks a URL and returns validation results.
func (v *Validator) Validate(ctx context.Context, rawURL string) *Result {
	startTime := time.Now()

	// Validate URL format
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return &Result{
			Valid:      false,
			Status:     StatusInvalidURL,
			Error:      "Invalid URL format",
			DurationMs: time.Since(startTime).Milliseconds(),
		}
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return &Result{
			Valid:      false,
			Status:     StatusInvalidURL,
			Error:      fmt.Sprintf("Failed to create request: %v", err),
			DurationMs: time.Since(startTime).Milliseconds(),
		}
	}

	req.Header.Set("User-Agent", v.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	// Execute request
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return v.handleError(err, rawURL, startTime)
	}
	defer resp.Body.Close()

	// Read a portion of the body for content detection
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyReadBytes))
	bodyText := string(bodyBytes)

	// Build result
	result := &Result{
		HTTPCode:      resp.StatusCode,
		FinalURL:      resp.Request.URL.String(),
		ContentMIME:   resp.Header.Get("Content-Type"),
		DurationMs:    time.Since(startTime).Milliseconds(),
		WasRedirected: resp.Request.URL.String() != rawURL,
	}

	// Count redirects from response chain
	if result.WasRedirected {
		// The Request in the Response is the final request after redirects
		// We don't have direct access to redirect count, but we know it happened
		result.RedirectCount = 1 // Minimum, actual may be higher
	}

	// Handle HTTP status codes
	switch {
	case resp.StatusCode == 404:
		result.Valid = false
		result.Status = StatusNotFound
		result.Error = "Page not found (404)"
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		result.Valid = false
		result.Status = StatusClientError
		result.Error = fmt.Sprintf("HTTP error %d", resp.StatusCode)
	case resp.StatusCode >= 500:
		result.Valid = false
		result.Status = StatusServerError
		result.Error = fmt.Sprintf("Server error %d", resp.StatusCode)
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		result.Valid = true
		if result.WasRedirected {
			result.Status = StatusValidRedirected
		} else {
			result.Status = StatusValid
		}
	default:
		// 3xx codes that weren't followed (shouldn't happen with our config)
		result.Valid = false
		result.Status = StatusClientError
		result.Error = fmt.Sprintf("Unexpected HTTP status %d", resp.StatusCode)
	}

	// Detect content type
	result.ContentType = v.detectContentType(result.ContentMIME, result.FinalURL, bodyText)

	return result
}

// handleError converts network errors to appropriate status codes.
func (v *Validator) handleError(err error, rawURL string, startTime time.Time) *Result {
	result := &Result{
		Valid:      false,
		Error:      err.Error(),
		DurationMs: time.Since(startTime).Milliseconds(),
	}

	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "no such host"):
		result.Status = StatusDNSError
		result.Error = "DNS lookup failed"
	case strings.Contains(errStr, "connection refused"):
		result.Status = StatusConnectionRefused
		result.Error = "Connection refused"
	case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded"):
		result.Status = StatusTimeout
		result.Error = "Request timed out"
	case strings.Contains(errStr, "certificate") || strings.Contains(errStr, "tls") || strings.Contains(errStr, "x509"):
		result.Status = StatusSSLError
		result.Error = "SSL/TLS error"
	case strings.Contains(errStr, "redirect"):
		result.Status = StatusTooManyRedirects
		result.Error = "Too many redirects"
	default:
		result.Status = StatusConnectionFailed
	}

	return result
}

// detectContentType analyzes the response to determine what kind of page this is.
func (v *Validator) detectContentType(mimeType, finalURL, bodyText string) ContentType {
	// Check MIME type first
	mimeType = strings.ToLower(mimeType)
	if strings.Contains(mimeType, "pdf") {
		return ContentTypePDF
	}

	// Check URL for PDF extension
	parsedURL, _ := url.Parse(finalURL)
	if parsedURL != nil && strings.HasSuffix(strings.ToLower(parsedURL.Path), ".pdf") {
		return ContentTypePDF
	}

	// Analyze body text for login indicators
	bodyLower := strings.ToLower(bodyText)
	if v.detectLoginWall(bodyLower, finalURL) {
		return ContentTypeLoginWall
	}

	// Check if it's a portal listing page
	if v.detectPortalListing(finalURL) {
		return ContentTypePortalListing
	}

	// Check for RFP-specific content
	if v.detectRFPPage(bodyLower) {
		return ContentTypeRFPPage
	}

	return ContentTypeOther
}

// detectLoginWall checks if the page requires login.
func (v *Validator) detectLoginWall(bodyLower, finalURL string) bool {
	// Login form indicators
	loginPatterns := []string{
		"sign in",
		"log in",
		"login",
		"password",
		"authenticate",
		"access denied",
		"restricted access",
		"registration required",
		"create an account",
		"you must be logged in",
		"please register",
		"session expired",
	}

	for _, pattern := range loginPatterns {
		if strings.Contains(bodyLower, pattern) {
			// Check if this is actually a login form vs just mentioning login
			if strings.Contains(bodyLower, "type=\"password\"") ||
				strings.Contains(bodyLower, "type='password'") ||
				strings.Contains(bodyLower, "name=\"password\"") ||
				strings.Contains(bodyLower, "name='password'") {
				return true
			}
		}
	}

	// Check for common authentication redirect URLs
	authURLPatterns := []string{
		"/login",
		"/signin",
		"/auth",
		"/sso",
		"/oauth",
		"/saml",
		"returnurl=",
		"redirect=",
		"auth.php",
		"login.aspx",
	}

	finalURLLower := strings.ToLower(finalURL)
	for _, pattern := range authURLPatterns {
		if strings.Contains(finalURLLower, pattern) {
			return true
		}
	}

	return false
}

// detectPortalListing checks if URL is from a known procurement portal.
func (v *Validator) detectPortalListing(finalURL string) bool {
	portalDomains := []string{
		"bonfirehub.com",
		"opengov.com",
		"planetbids.com",
		"bidnet.com",
		"publicpurchase.com",
		"bidsync.com",
		"ionwave.net",
		"vendorregistry.com",
		"negometrix.com",
		"procurato.com",
	}

	finalURLLower := strings.ToLower(finalURL)
	for _, domain := range portalDomains {
		if strings.Contains(finalURLLower, domain) {
			return true
		}
	}

	return false
}

// detectRFPPage checks if content appears to be an RFP page.
func (v *Validator) detectRFPPage(bodyLower string) bool {
	// Must have RFP-related terms
	rfpTerms := []string{
		"request for proposal",
		"request for quote",
		"request for bid",
		"rfp",
		"rfq",
		"rfb",
		"solicitation",
		"bid submission",
		"proposal submission",
		"procurement",
		"due date",
		"closing date",
		"submission deadline",
	}

	matchCount := 0
	for _, term := range rfpTerms {
		if strings.Contains(bodyLower, term) {
			matchCount++
		}
	}

	// Need at least 2 RFP-related terms to consider it an RFP page
	return matchCount >= 2
}

// ValidateBatch validates multiple URLs with rate limiting.
func (v *Validator) ValidateBatch(ctx context.Context, urls []string, delayBetween time.Duration) []*Result {
	results := make([]*Result, 0, len(urls))

	for i, rawURL := range urls {
		select {
		case <-ctx.Done():
			return results
		default:
		}

		result := v.Validate(ctx, rawURL)
		results = append(results, result)

		// Rate limiting between requests (except for the last one)
		if i < len(urls)-1 && delayBetween > 0 {
			select {
			case <-ctx.Done():
				return results
			case <-time.After(delayBetween):
			}
		}
	}

	return results
}

// loginPatternRegexp is a compiled regex for finding password fields.
var loginPatternRegexp = regexp.MustCompile(`(?i)(type\s*=\s*["']password["']|name\s*=\s*["']password["'])`)

// IsLoginPage performs a deeper check for login pages.
func IsLoginPage(body string) bool {
	return loginPatternRegexp.MatchString(body)
}
