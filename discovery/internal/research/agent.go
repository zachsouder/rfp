// Package research provides a multi-step research agent for investigating
// search results and extracting RFP details.
package research

import (
	"context"
	"fmt"
	"time"

	"github.com/zachsouder/rfp/shared/models"
)

const (
	defaultMaxSteps = 5
	defaultModel    = "gemini-1.5-flash"
)

// Status represents the research outcome status.
type Status string

const (
	StatusResearching      Status = "researching"
	StatusResearched       Status = "researched"
	StatusNeedsManual      Status = "needs_manual"
	StatusNeedsManualUpload Status = "needs_manual_upload"
	StatusExhausted        Status = "research_exhausted"
	StatusFailed           Status = "failed"
)

// Agent is a multi-step research agent that investigates search results.
type Agent struct {
	geminiClient *GeminiClient
	maxSteps     int
}

// NewAgent creates a new research agent.
func NewAgent(apiKey string) *Agent {
	return &Agent{
		geminiClient: NewGeminiClient(apiKey),
		maxSteps:     defaultMaxSteps,
	}
}

// WithMaxSteps sets the maximum number of research steps.
func (a *Agent) WithMaxSteps(n int) *Agent {
	a.maxSteps = n
	return a
}

// ResearchContext holds the state during research.
type ResearchContext struct {
	ResultID         int
	OriginalURL      string
	CurrentURL       string
	Title            string
	Snippet          string
	HintAgency       string
	HintState        string
	HintDueDate      string
	PageContent      string
	ExtractedDetails *ExtractedDetails
	FoundPDFs        []string
	Status           Status

	// Internal tracking
	fetchFailed      bool
	fetchError       string
	pdfSearchDone    bool
	sourceSearchDone bool
}

// ExtractedDetails contains structured RFP information extracted by the agent.
type ExtractedDetails struct {
	Title          string  `json:"title,omitempty"`
	Agency         string  `json:"agency,omitempty"`
	City           string  `json:"location_city,omitempty"`
	State          string  `json:"location_state,omitempty"`
	DueDate        string  `json:"due_date,omitempty"`
	ScopeSummary   string  `json:"scope_summary,omitempty"`
	EstimatedValue string  `json:"estimated_value,omitempty"`
	Incumbent      string  `json:"incumbent,omitempty"`
	Category       string  `json:"category,omitempty"`
	VenueType      string  `json:"venue_type,omitempty"`
}

// ResearchResult contains the outcome of researching a search result.
type ResearchResult struct {
	Success          bool              `json:"success"`
	ResultID         int               `json:"result_id"`
	Status           Status            `json:"status"`
	StepsTaken       int               `json:"steps_taken"`
	TotalTokens      int               `json:"total_tokens"`
	ExtractedDetails *ExtractedDetails `json:"extracted_details,omitempty"`
	FoundPDFs        []string          `json:"found_pdfs,omitempty"`
	Steps            []ResearchStep    `json:"steps"`
	Error            string            `json:"error,omitempty"`
}

// ResearchStep records a single step in the research process.
type ResearchStep struct {
	StepNumber    int           `json:"step_number"`
	Action        string        `json:"action"`
	InputSummary  string        `json:"input_summary,omitempty"`
	OutputSummary string        `json:"output_summary,omitempty"`
	Reasoning     string        `json:"reasoning"`
	Success       bool          `json:"success"`
	TokensUsed    int           `json:"tokens_used,omitempty"`
	DurationMs    int64         `json:"duration_ms"`
}

// Research investigates a search result to extract RFP details.
func (a *Agent) Research(ctx context.Context, result *models.SearchResult) (*ResearchResult, error) {
	// Initialize context
	rc := &ResearchContext{
		ResultID:    result.ID,
		OriginalURL: result.URL,
		CurrentURL:  result.FinalURL,
		Title:       result.Title,
		Snippet:     result.Snippet,
		HintAgency:  result.HintAgency,
		HintState:   result.HintState,
		Status:      StatusResearching,
	}

	// Use final URL if available, otherwise original
	if rc.CurrentURL == "" {
		rc.CurrentURL = rc.OriginalURL
	}

	res := &ResearchResult{
		ResultID: result.ID,
		Steps:    make([]ResearchStep, 0),
	}

	stepCount := 0
	totalTokens := 0

	// Research loop
	for stepCount < a.maxSteps && rc.Status == StatusResearching {
		stepCount++

		// Decide and execute next action
		step, err := a.executeStep(ctx, rc, stepCount)
		if err != nil {
			res.Success = false
			res.Error = err.Error()
			res.Status = StatusFailed
			res.StepsTaken = stepCount
			return res, nil
		}

		res.Steps = append(res.Steps, *step)
		totalTokens += step.TokensUsed

		// Check if we should stop
		if rc.Status != StatusResearching {
			break
		}
	}

	// Handle max steps reached
	if stepCount >= a.maxSteps && rc.Status == StatusResearching {
		rc.Status = StatusExhausted
		res.Steps = append(res.Steps, ResearchStep{
			StepNumber: stepCount + 1,
			Action:     "give_up",
			Reasoning:  fmt.Sprintf("Reached maximum of %d research steps without finding complete RFP details. Marking as exhausted for manual review.", a.maxSteps),
			Success:    false,
		})
	}

	res.Success = true
	res.Status = rc.Status
	res.StepsTaken = stepCount
	res.TotalTokens = totalTokens
	res.ExtractedDetails = rc.ExtractedDetails
	res.FoundPDFs = rc.FoundPDFs

	return res, nil
}

// executeStep decides and executes the next research action.
func (a *Agent) executeStep(ctx context.Context, rc *ResearchContext, stepNumber int) (*ResearchStep, error) {
	startTime := time.Now()

	// Decide what action to take
	action := a.decideAction(rc)

	step := &ResearchStep{
		StepNumber: stepNumber,
		Action:     action.Name,
		Reasoning:  action.Reasoning,
	}

	// Execute the action
	var tokensUsed int
	var err error

	switch action.Name {
	case "fetch_page":
		err = a.actionFetchPage(ctx, rc)
		step.InputSummary = rc.CurrentURL
		if err == nil {
			step.OutputSummary = fmt.Sprintf("Fetched %d chars", len(rc.PageContent))
			step.Success = true
		} else {
			step.OutputSummary = err.Error()
		}

	case "extract_details":
		tokensUsed, err = a.actionExtractDetails(ctx, rc)
		step.InputSummary = "Page content analysis"
		if err == nil && rc.ExtractedDetails != nil {
			step.OutputSummary = fmt.Sprintf("Extracted: %s", rc.ExtractedDetails.Title)
			step.Success = true
		} else if err != nil {
			step.OutputSummary = err.Error()
		}

	case "discover_pdfs":
		a.actionDiscoverPDFs(rc)
		step.InputSummary = "Searching for PDF links"
		step.OutputSummary = fmt.Sprintf("Found %d PDFs", len(rc.FoundPDFs))
		step.Success = true

	case "mark_complete":
		rc.Status = StatusResearched
		step.InputSummary = "Research complete"
		step.OutputSummary = "Marked as researched"
		step.Success = true

	case "mark_needs_manual":
		rc.Status = StatusNeedsManual
		step.InputSummary = action.Reason
		step.OutputSummary = "Marked for manual review"
		step.Success = true

	case "mark_login_required":
		rc.Status = StatusNeedsManualUpload
		step.InputSummary = "Login wall detected"
		step.OutputSummary = "Requires manual document upload"
		step.Success = true

	default:
		err = fmt.Errorf("unknown action: %s", action.Name)
	}

	step.TokensUsed = tokensUsed
	step.DurationMs = time.Since(startTime).Milliseconds()

	return step, nil
}

// Action represents a decided research action.
type Action struct {
	Name      string
	Reasoning string
	Reason    string // For mark_needs_manual
}

// decideAction determines the next action based on context.
func (a *Agent) decideAction(rc *ResearchContext) Action {
	// If we failed to fetch the page, mark as needs manual
	if rc.fetchFailed {
		return Action{
			Name:      "mark_needs_manual",
			Reasoning: fmt.Sprintf("Unable to fetch page content from %s. Error: %s. Marking for manual review.", rc.CurrentURL, rc.fetchError),
			Reason:    rc.fetchError,
		}
	}

	// If we haven't fetched the page yet, do that first
	if rc.PageContent == "" {
		return Action{
			Name:      "fetch_page",
			Reasoning: fmt.Sprintf("First step is to fetch the page content from %s to analyze it for RFP details.", rc.CurrentURL),
		}
	}

	// Check for login wall
	if detectLoginWall(rc.PageContent) {
		return Action{
			Name:      "mark_login_required",
			Reasoning: "Detected login wall or authentication requirement. The page content indicates restricted access. Marking for manual upload.",
		}
	}

	// If we haven't extracted details yet
	if rc.ExtractedDetails == nil || rc.ExtractedDetails.Title == "" {
		return Action{
			Name:      "extract_details",
			Reasoning: "Have page content, now extracting structured RFP details (title, agency, due date, scope).",
		}
	}

	// Try to discover PDFs if we haven't
	if !rc.pdfSearchDone {
		return Action{
			Name:      "discover_pdfs",
			Reasoning: "Looking for PDF attachments or document links that contain the full RFP specification.",
		}
	}

	// If we have enough details, mark complete
	if rc.ExtractedDetails != nil && rc.ExtractedDetails.Title != "" {
		return Action{
			Name:      "mark_complete",
			Reasoning: fmt.Sprintf("Successfully extracted RFP details: %s. Research complete.", rc.ExtractedDetails.Title),
		}
	}

	// Otherwise, mark for manual review
	return Action{
		Name:      "mark_needs_manual",
		Reasoning: "Unable to extract sufficient RFP details from the page. Marking for manual review.",
		Reason:    "Could not extract sufficient RFP details",
	}
}
