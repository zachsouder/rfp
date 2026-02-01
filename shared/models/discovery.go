// Package models provides shared data types for the RFP platform.
package models

import (
	"encoding/json"
	"time"
)

// SearchQueryConfig represents a configurable search query template.
type SearchQueryConfig struct {
	ID            int       `json:"id"`
	Name          string    `json:"name"`
	QueryTemplate string    `json:"query_template"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
}

// SearchQuery represents a search query execution.
type SearchQuery struct {
	ID            int       `json:"id"`
	QueryText     string    `json:"query_text"`
	QueryConfigID *int      `json:"query_config_id,omitempty"`
	ExecutedAt    time.Time `json:"executed_at"`
	ResultsCount  int       `json:"results_count"`
	Status        string    `json:"status"` // running, completed, failed
}

// SearchResult represents a raw search result before research.
type SearchResult struct {
	ID        int       `json:"id"`
	QueryID   int       `json:"query_id"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Snippet   string    `json:"snippet"`
	CreatedAt time.Time `json:"created_at"`

	// Validation
	URLValidated bool    `json:"url_validated"`
	URLValid     *bool   `json:"url_valid,omitempty"`
	FinalURL     string  `json:"final_url,omitempty"`
	ContentType  string  `json:"content_type,omitempty"` // rfp_page, portal_listing, login_wall, pdf, other

	// Extracted hints
	HintAgency  string     `json:"hint_agency,omitempty"`
	HintState   string     `json:"hint_state,omitempty"`
	HintDueDate *time.Time `json:"hint_due_date,omitempty"`

	// Research status
	ResearchStatus string `json:"research_status"` // pending, in_progress, completed, failed, skipped
	PromotedRFPID  *int   `json:"promoted_rfp_id,omitempty"`
	DuplicateOfID  *int   `json:"duplicate_of_id,omitempty"`
}

// ResearchStep represents a step in the research agent's process.
type ResearchStep struct {
	ID             int       `json:"id"`
	SearchResultID int       `json:"search_result_id"`
	StepNumber     int       `json:"step_number"`
	Action         string    `json:"action"` // fetch_page, extract_details, find_pdf, check_login, decide
	InputSummary   string    `json:"input_summary,omitempty"`
	OutputSummary  string    `json:"output_summary,omitempty"`
	Reasoning      string    `json:"reasoning,omitempty"`
	Success        bool      `json:"success"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// RFP represents a discovered RFP opportunity.
type RFP struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Agency string `json:"agency,omitempty"`
	State  string `json:"state,omitempty"`
	City   string `json:"city,omitempty"`

	// Source
	SourceURL string `json:"source_url,omitempty"`
	Portal    string `json:"portal,omitempty"`    // bonfire, opengov, bidnet, planetbids, direct
	PortalID  string `json:"portal_id,omitempty"` // ID within portal

	// Dates
	PostedDate *time.Time `json:"posted_date,omitempty"`
	DueDate    *time.Time `json:"due_date,omitempty"`

	// Classification
	Category      string   `json:"category,omitempty"`   // parking, valet, event_ops, transit, enforcement
	VenueType     string   `json:"venue_type,omitempty"` // arena, stadium, convention_center, airport, municipal
	ScopeKeywords []string `json:"scope_keywords,omitempty"`

	// Contract details
	TermMonths     *int     `json:"term_months,omitempty"`
	EstimatedValue *float64 `json:"estimated_value,omitempty"`
	Incumbent      string   `json:"incumbent,omitempty"`

	// Access
	LoginRequired bool   `json:"login_required"`
	LoginNotes    string `json:"login_notes,omitempty"`

	// Documents
	PDFURLs []string `json:"pdf_urls,omitempty"`

	// Metadata
	RawContent   string    `json:"raw_content,omitempty"`
	DiscoveredAt time.Time `json:"discovered_at"`
	LastChecked  time.Time `json:"last_checked,omitempty"`
	IsActive     bool      `json:"is_active"`
}

// Source represents a monitored data source.
type Source struct {
	ID         int             `json:"id"`
	Name       string          `json:"name"`
	SourceType string          `json:"source_type"` // gemini_search, portal_scrape, manual
	Config     json.RawMessage `json:"config,omitempty"`
	Enabled    bool            `json:"enabled"`
	LastRun    *time.Time      `json:"last_run,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}
