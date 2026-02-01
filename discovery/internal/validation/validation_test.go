package validation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidator_Validate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>Test page</body></html>"))
	}))
	defer server.Close()

	v := NewValidator()
	result := v.Validate(context.Background(), server.URL)

	if !result.Valid {
		t.Errorf("Expected valid=true, got false with status %s: %s", result.Status, result.Error)
	}
	if result.Status != StatusValid {
		t.Errorf("Expected status=%s, got %s", StatusValid, result.Status)
	}
	if result.HTTPCode != 200 {
		t.Errorf("Expected HTTPCode=200, got %d", result.HTTPCode)
	}
}

func TestValidator_Validate_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	v := NewValidator()
	result := v.Validate(context.Background(), server.URL)

	if result.Valid {
		t.Error("Expected valid=false for 404")
	}
	if result.Status != StatusNotFound {
		t.Errorf("Expected status=%s, got %s", StatusNotFound, result.Status)
	}
}

func TestValidator_Validate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	v := NewValidator()
	result := v.Validate(context.Background(), server.URL)

	if result.Valid {
		t.Error("Expected valid=false for 500")
	}
	if result.Status != StatusServerError {
		t.Errorf("Expected status=%s, got %s", StatusServerError, result.Status)
	}
}

func TestValidator_Validate_Redirect(t *testing.T) {
	finalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Final destination"))
	}))
	defer finalServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalServer.URL, http.StatusMovedPermanently)
	}))
	defer redirectServer.Close()

	v := NewValidator()
	result := v.Validate(context.Background(), redirectServer.URL)

	if !result.Valid {
		t.Errorf("Expected valid=true, got false: %s", result.Error)
	}
	if result.Status != StatusValidRedirected {
		t.Errorf("Expected status=%s, got %s", StatusValidRedirected, result.Status)
	}
	if !result.WasRedirected {
		t.Error("Expected WasRedirected=true")
	}
	if result.FinalURL != finalServer.URL {
		t.Errorf("Expected FinalURL=%s, got %s", finalServer.URL, result.FinalURL)
	}
}

func TestValidator_Validate_InvalidURL(t *testing.T) {
	v := NewValidator()

	tests := []string{
		"",
		"not-a-url",
		"ftp://example.com",
		"://missing-scheme.com",
	}

	for _, url := range tests {
		result := v.Validate(context.Background(), url)
		if result.Valid {
			t.Errorf("Expected valid=false for URL %q", url)
		}
		if result.Status != StatusInvalidURL {
			t.Errorf("Expected status=%s for URL %q, got %s", StatusInvalidURL, url, result.Status)
		}
	}
}

func TestValidator_Validate_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the context timeout
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	v := NewValidator()

	// Use a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := v.Validate(ctx, server.URL)

	if result.Valid {
		t.Error("Expected valid=false for timeout")
	}
	if result.Status != StatusTimeout {
		t.Errorf("Expected status=%s, got %s", StatusTimeout, result.Status)
	}
}

func TestDetectContentType(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name        string
		mimeType    string
		url         string
		body        string
		expected    ContentType
	}{
		{
			name:     "PDF by MIME",
			mimeType: "application/pdf",
			url:      "https://example.com/doc",
			body:     "",
			expected: ContentTypePDF,
		},
		{
			name:     "PDF by extension",
			mimeType: "text/html",
			url:      "https://example.com/document.pdf",
			body:     "",
			expected: ContentTypePDF,
		},
		{
			name:     "Login wall",
			mimeType: "text/html",
			url:      "https://example.com/",
			body:     `<form><input type="password" name="pass"></form>`,
			expected: ContentTypeLoginWall,
		},
		{
			name:     "Portal listing",
			mimeType: "text/html",
			url:      "https://app.bonfirehub.com/portal/rfp/123",
			body:     "<html></html>",
			expected: ContentTypePortalListing,
		},
		{
			name:     "RFP page",
			mimeType: "text/html",
			url:      "https://city.gov/rfp",
			body:     "Request for Proposal - Parking Services. Due Date: January 15. Submission deadline.",
			expected: ContentTypeRFPPage,
		},
		{
			name:     "Other content",
			mimeType: "text/html",
			url:      "https://example.com/about",
			body:     "About us page",
			expected: ContentTypeOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.detectContentType(tt.mimeType, tt.url, tt.body)
			if result != tt.expected {
				t.Errorf("detectContentType() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestDetectLoginWall(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name     string
		body     string
		url      string
		expected bool
	}{
		{
			name:     "password field",
			body:     `<input type="password">`,
			url:      "https://example.com",
			expected: true,
		},
		{
			name:     "login redirect URL",
			body:     "Redirecting...",
			url:      "https://example.com/login?returnurl=/rfp",
			expected: true,
		},
		{
			name:     "normal page",
			body:     "Welcome to our RFP listing",
			url:      "https://example.com/rfp",
			expected: false,
		},
		{
			name:     "mentions login but no password field",
			body:     "Please log in to access this content",
			url:      "https://example.com/help",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.detectLoginWall(strings.ToLower(tt.body), tt.url)
			if result != tt.expected {
				t.Errorf("detectLoginWall() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectPortalListing(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://app.bonfirehub.com/portal/rfp/123", true},
		{"https://www.opengov.com/solicitation/456", true},
		{"https://planetbids.com/bid/789", true},
		{"https://example.com/rfp", false},
		{"https://city.gov/procurement", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := v.detectPortalListing(tt.url)
			if result != tt.expected {
				t.Errorf("detectPortalListing(%q) = %v, want %v", tt.url, result, tt.expected)
			}
		})
	}
}

func TestValidateBatch(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	v := NewValidator()
	urls := []string{server.URL + "/1", server.URL + "/2", server.URL + "/3"}

	results := v.ValidateBatch(context.Background(), urls, 0)

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}
	if requestCount != 3 {
		t.Errorf("Expected 3 requests, got %d", requestCount)
	}

	for i, result := range results {
		if !result.Valid {
			t.Errorf("Result %d: expected valid=true", i)
		}
	}
}

func TestValidateBatch_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	v := NewValidator()
	urls := []string{server.URL + "/1", server.URL + "/2", server.URL + "/3"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	results := v.ValidateBatch(ctx, urls, 100*time.Millisecond)

	// Should return empty or partial results due to cancellation
	if len(results) >= len(urls) {
		t.Errorf("Expected fewer results due to cancellation, got %d", len(results))
	}
}
