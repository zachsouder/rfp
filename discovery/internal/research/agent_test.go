package research

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zachsouder/rfp/shared/models"
)

func TestHtmlToText(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		contains string
	}{
		{
			name:     "simple text",
			html:     "<p>Hello World</p>",
			contains: "Hello World",
		},
		{
			name:     "script removal",
			html:     "<p>Before</p><script>alert('hi')</script><p>After</p>",
			contains: "Before",
		},
		{
			name:     "style removal",
			html:     "<style>.foo{color:red}</style><p>Content</p>",
			contains: "Content",
		},
		{
			name:     "entity decoding",
			html:     "<p>Tom &amp; Jerry</p>",
			contains: "Tom & Jerry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := htmlToText(tt.html)
			if !contains(result, tt.contains) {
				t.Errorf("htmlToText() = %q, should contain %q", result, tt.contains)
			}
		})
	}
}

func TestDetectLoginWall(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "login form",
			content:  `<form><input type="password" required><button>Sign in</button></form>`,
			expected: true,
		},
		{
			name:     "access denied with form",
			content:  "Access denied. Please log in with your username and password.",
			expected: true,
		},
		{
			name:     "normal RFP page",
			content:  "Request for Proposal: Parking Services. Due Date: January 15, 2024.",
			expected: false,
		},
		{
			name:     "login mentioned but no form context",
			content:  "Contact us if you need login assistance. RFP details below.",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectLoginWall(tt.content)
			if result != tt.expected {
				t.Errorf("detectLoginWall() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAgent_DecideAction(t *testing.T) {
	agent := NewAgent("fake-key")

	tests := []struct {
		name           string
		context        *ResearchContext
		expectedAction string
	}{
		{
			name: "fetch page first",
			context: &ResearchContext{
				CurrentURL:  "https://example.com",
				PageContent: "",
			},
			expectedAction: "fetch_page",
		},
		{
			name: "extract details after fetch",
			context: &ResearchContext{
				CurrentURL:  "https://example.com",
				PageContent: "Some RFP content here",
			},
			expectedAction: "extract_details",
		},
		{
			name: "discover pdfs after extraction",
			context: &ResearchContext{
				CurrentURL:  "https://example.com",
				PageContent: "Content",
				ExtractedDetails: &ExtractedDetails{
					Title:  "Test RFP",
					Agency: "Test Agency",
				},
			},
			expectedAction: "discover_pdfs",
		},
		{
			name: "mark complete when done",
			context: &ResearchContext{
				CurrentURL:  "https://example.com",
				PageContent: "Content",
				ExtractedDetails: &ExtractedDetails{
					Title: "Test RFP",
				},
				pdfSearchDone: true,
			},
			expectedAction: "mark_complete",
		},
		{
			name: "handle fetch failure",
			context: &ResearchContext{
				CurrentURL:  "https://example.com",
				fetchFailed: true,
				fetchError:  "Connection refused",
			},
			expectedAction: "mark_needs_manual",
		},
		{
			name: "detect login wall",
			context: &ResearchContext{
				CurrentURL:  "https://example.com",
				PageContent: `<form><input type="password" required>Please log in</form>`,
			},
			expectedAction: "mark_login_required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := agent.decideAction(tt.context)
			if action.Name != tt.expectedAction {
				t.Errorf("decideAction() = %s, want %s", action.Name, tt.expectedAction)
			}
		})
	}
}

func TestAgent_ActionFetchPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body>
			<h1>Request for Proposal</h1>
			<p>Parking Management Services</p>
			<p>Due Date: January 15, 2024</p>
		</body></html>`))
	}))
	defer server.Close()

	agent := NewAgent("fake-key")
	rc := &ResearchContext{
		CurrentURL: server.URL,
	}

	err := agent.actionFetchPage(context.Background(), rc)
	if err != nil {
		t.Fatalf("actionFetchPage() error = %v", err)
	}

	if rc.PageContent == "" {
		t.Error("Expected PageContent to be populated")
	}
	if !contains(rc.PageContent, "Request for Proposal") {
		t.Error("Expected PageContent to contain 'Request for Proposal'")
	}
	if !contains(rc.PageContent, "Parking Management") {
		t.Error("Expected PageContent to contain 'Parking Management'")
	}
}

func TestAgent_ActionFetchPage_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	agent := NewAgent("fake-key")
	rc := &ResearchContext{
		CurrentURL: server.URL,
	}

	err := agent.actionFetchPage(context.Background(), rc)
	if err == nil {
		t.Error("Expected error for 404 response")
	}
	if !rc.fetchFailed {
		t.Error("Expected fetchFailed to be true")
	}
}

func TestAgent_ActionDiscoverPDFs(t *testing.T) {
	agent := NewAgent("fake-key")
	rc := &ResearchContext{
		PageContent: `
			<a href="https://example.com/rfp.pdf">RFP Document</a>
			<a href="https://example.com/specs.pdf">Specifications</a>
			<a href="https://example.com/other.html">Other Link</a>
		`,
	}

	agent.actionDiscoverPDFs(rc)

	if !rc.pdfSearchDone {
		t.Error("Expected pdfSearchDone to be true")
	}
	if len(rc.FoundPDFs) != 2 {
		t.Errorf("Expected 2 PDFs, got %d", len(rc.FoundPDFs))
	}
}

func TestAgent_Research_BasicFlow(t *testing.T) {
	// Create a mock server that returns RFP-like content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body>
			<h1>RFP: Parking Management Services</h1>
			<p>City of Springfield</p>
			<p>Due Date: 2024-03-15</p>
			<a href="https://example.com/rfp.pdf">Download RFP</a>
		</body></html>`))
	}))
	defer server.Close()

	// Note: This test won't actually call Gemini since we don't have a real API key
	// It tests the flow up to the extraction step
	agent := NewAgent("fake-key")
	agent.maxSteps = 1 // Limit to fetch only

	result := &models.SearchResult{
		ID:       1,
		URL:      server.URL,
		FinalURL: server.URL,
		Title:    "Test RFP",
	}

	res, err := agent.Research(context.Background(), result)
	if err != nil {
		t.Fatalf("Research() error = %v", err)
	}

	// With max 1 step, we should have fetched the page
	if res.StepsTaken != 1 {
		t.Errorf("Expected 1 step, got %d", res.StepsTaken)
	}
	if len(res.Steps) == 0 {
		t.Fatal("Expected at least one step")
	}
	if res.Steps[0].Action != "fetch_page" {
		t.Errorf("Expected first action to be fetch_page, got %s", res.Steps[0].Action)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
