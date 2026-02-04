// Package cliapi provides a public API for CLI tools to access discovery functionality.
package cliapi

import (
	"context"

	"github.com/zachsouder/rfp/discovery/internal/research"
	"github.com/zachsouder/rfp/shared/models"
)

// ResearchResult contains the outcome of researching a URL.
type ResearchResult struct {
	Success          bool              `json:"success"`
	ResultID         int               `json:"result_id"`
	Status           string            `json:"status"`
	StepsTaken       int               `json:"steps_taken"`
	TotalTokens      int               `json:"total_tokens"`
	ExtractedDetails *ExtractedDetails `json:"extracted_details,omitempty"`
	FoundPDFs        []string          `json:"found_pdfs,omitempty"`
	Steps            []ResearchStep    `json:"steps"`
	Error            string            `json:"error,omitempty"`
}

// ExtractedDetails contains structured RFP information.
type ExtractedDetails struct {
	Title          string `json:"title,omitempty"`
	Agency         string `json:"agency,omitempty"`
	City           string `json:"location_city,omitempty"`
	State          string `json:"location_state,omitempty"`
	DueDate        string `json:"due_date,omitempty"`
	ScopeSummary   string `json:"scope_summary,omitempty"`
	EstimatedValue string `json:"estimated_value,omitempty"`
	Incumbent      string `json:"incumbent,omitempty"`
	Category       string `json:"category,omitempty"`
	VenueType      string `json:"venue_type,omitempty"`
}

// ResearchStep records a single step in the research process.
type ResearchStep struct {
	StepNumber    int    `json:"step_number"`
	Action        string `json:"action"`
	InputSummary  string `json:"input_summary,omitempty"`
	OutputSummary string `json:"output_summary,omitempty"`
	Reasoning     string `json:"reasoning"`
	Success       bool   `json:"success"`
	TokensUsed    int    `json:"tokens_used,omitempty"`
	DurationMs    int64  `json:"duration_ms"`
}

// ResearchURL runs the research agent on a URL and returns the results.
func ResearchURL(ctx context.Context, apiKey string, url string) (*ResearchResult, error) {
	agent := research.NewAgent(apiKey)

	// Create a synthetic search result
	result := &models.SearchResult{
		ID:       0,
		URL:      url,
		FinalURL: url,
		Title:    "Manual research",
	}

	res, err := agent.Research(ctx, result)
	if err != nil {
		return nil, err
	}

	// Convert to public types
	out := &ResearchResult{
		Success:     res.Success,
		ResultID:    res.ResultID,
		Status:      string(res.Status),
		StepsTaken:  res.StepsTaken,
		TotalTokens: res.TotalTokens,
		FoundPDFs:   res.FoundPDFs,
		Error:       res.Error,
	}

	// Convert extracted details
	if res.ExtractedDetails != nil {
		out.ExtractedDetails = &ExtractedDetails{
			Title:          res.ExtractedDetails.Title,
			Agency:         res.ExtractedDetails.Agency,
			City:           res.ExtractedDetails.City,
			State:          res.ExtractedDetails.State,
			DueDate:        res.ExtractedDetails.DueDate,
			ScopeSummary:   res.ExtractedDetails.ScopeSummary,
			EstimatedValue: res.ExtractedDetails.EstimatedValue,
			Incumbent:      res.ExtractedDetails.Incumbent,
			Category:       res.ExtractedDetails.Category,
			VenueType:      res.ExtractedDetails.VenueType,
		}
	}

	// Convert steps
	for _, s := range res.Steps {
		out.Steps = append(out.Steps, ResearchStep{
			StepNumber:    s.StepNumber,
			Action:        s.Action,
			InputSummary:  s.InputSummary,
			OutputSummary: s.OutputSummary,
			Reasoning:     s.Reasoning,
			Success:       s.Success,
			TokensUsed:    s.TokensUsed,
			DurationMs:    s.DurationMs,
		})
	}

	return out, nil
}
