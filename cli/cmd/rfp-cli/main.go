// Package main is the entry point for the RFP CLI tool.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zachsouder/rfp/discovery/cliapi"
	"github.com/zachsouder/rfp/shared/config"
	"github.com/zachsouder/rfp/shared/db"
	"github.com/zachsouder/rfp/shared/models"
)

var rootCmd = &cobra.Command{
	Use:   "rfp-cli",
	Short: "RFP Intelligence Platform CLI",
	Long:  `CLI tools for managing and inspecting the RFP Intelligence Platform.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(discoveryCmd)
}

// discoveryCmd is the parent command for discovery operations
var discoveryCmd = &cobra.Command{
	Use:   "discovery",
	Short: "Discovery service operations",
	Long:  `Commands for inspecting and managing the discovery pipeline.`,
}

func init() {
	discoveryCmd.AddCommand(statsCmd)
	discoveryCmd.AddCommand(inspectCmd)
	discoveryCmd.AddCommand(researchCmd)
	discoveryCmd.AddCommand(retryFailedCmd)
	discoveryCmd.AddCommand(recentCmd)
	discoveryCmd.AddCommand(exportCmd)
}

// connectDB loads config and connects to the database.
func connectDB(ctx context.Context) (*db.DB, error) {
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL not set")
	}
	return db.Connect(ctx, cfg.DatabaseURL)
}

// Stats command
var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show discovery statistics",
	Long:  `Display statistics about recent search and research activity.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		database, err := connectDB(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer database.Close()

		// Get search query stats
		var totalQueries, queriesLast24h, queriesLast7d int
		err = database.QueryRow(ctx, `
			SELECT
				COUNT(*),
				COUNT(*) FILTER (WHERE executed_at > NOW() - INTERVAL '24 hours'),
				COUNT(*) FILTER (WHERE executed_at > NOW() - INTERVAL '7 days')
			FROM discovery.search_queries
		`).Scan(&totalQueries, &queriesLast24h, &queriesLast7d)
		if err != nil {
			return fmt.Errorf("failed to query search stats: %w", err)
		}

		// Get search result stats by status
		type statusCount struct {
			Status string
			Count  int
		}
		rows, err := database.Query(ctx, `
			SELECT COALESCE(research_status, 'unknown'), COUNT(*)
			FROM discovery.search_results
			GROUP BY research_status
			ORDER BY COUNT(*) DESC
		`)
		if err != nil {
			return fmt.Errorf("failed to query result stats: %w", err)
		}
		defer rows.Close()

		var statusCounts []statusCount
		var totalResults int
		for rows.Next() {
			var sc statusCount
			if err := rows.Scan(&sc.Status, &sc.Count); err != nil {
				return fmt.Errorf("failed to scan status count: %w", err)
			}
			statusCounts = append(statusCounts, sc)
			totalResults += sc.Count
		}

		// Get RFP counts
		var totalRFPs, activeRFPs, rfpsLast7d int
		err = database.QueryRow(ctx, `
			SELECT
				COUNT(*),
				COUNT(*) FILTER (WHERE is_active = true),
				COUNT(*) FILTER (WHERE discovered_at > NOW() - INTERVAL '7 days')
			FROM discovery.rfps
		`).Scan(&totalRFPs, &activeRFPs, &rfpsLast7d)
		if err != nil {
			return fmt.Errorf("failed to query RFP stats: %w", err)
		}

		// Print stats
		fmt.Println("=== Discovery Statistics ===")
		fmt.Println()
		fmt.Println("Search Queries:")
		fmt.Printf("  Total:        %d\n", totalQueries)
		fmt.Printf("  Last 24h:     %d\n", queriesLast24h)
		fmt.Printf("  Last 7 days:  %d\n", queriesLast7d)
		fmt.Println()
		fmt.Printf("Search Results: %d total\n", totalResults)
		for _, sc := range statusCounts {
			fmt.Printf("  %-15s %d\n", sc.Status+":", sc.Count)
		}
		fmt.Println()
		fmt.Println("RFPs:")
		fmt.Printf("  Total:        %d\n", totalRFPs)
		fmt.Printf("  Active:       %d\n", activeRFPs)
		fmt.Printf("  Last 7 days:  %d\n", rfpsLast7d)

		return nil
	},
}

// Inspect command
var inspectCmd = &cobra.Command{
	Use:   "inspect [result-id]",
	Short: "Inspect research steps for a result",
	Long:  `View the research steps taken for a specific search result.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resultID, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid result ID: %w", err)
		}

		ctx := context.Background()
		database, err := connectDB(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer database.Close()

		// Get the search result
		var sr models.SearchResult
		err = database.QueryRow(ctx, `
			SELECT id, query_id, url, title, snippet, url_validated, url_valid, final_url, content_type,
			       hint_agency, hint_state, hint_due_date, research_status, promoted_rfp_id, duplicate_of_id, created_at
			FROM discovery.search_results
			WHERE id = $1
		`, resultID).Scan(
			&sr.ID, &sr.QueryID, &sr.URL, &sr.Title, &sr.Snippet,
			&sr.URLValidated, &sr.URLValid, &sr.FinalURL, &sr.ContentType,
			&sr.HintAgency, &sr.HintState, &sr.HintDueDate,
			&sr.ResearchStatus, &sr.PromotedRFPID, &sr.DuplicateOfID, &sr.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("search result not found: %w", err)
		}

		// Print result info
		fmt.Printf("=== Search Result #%d ===\n", sr.ID)
		fmt.Printf("URL:     %s\n", sr.URL)
		fmt.Printf("Title:   %s\n", sr.Title)
		fmt.Printf("Status:  %s\n", sr.ResearchStatus)
		if sr.FinalURL != "" && sr.FinalURL != sr.URL {
			fmt.Printf("Final:   %s\n", sr.FinalURL)
		}
		if sr.ContentType != "" {
			fmt.Printf("Type:    %s\n", sr.ContentType)
		}
		fmt.Printf("Created: %s\n", sr.CreatedAt.Format(time.RFC3339))
		fmt.Println()

		// Get research steps
		rows, err := database.Query(ctx, `
			SELECT id, step_number, action, input_summary, output_summary, reasoning, success, error_message, created_at
			FROM discovery.research_steps
			WHERE search_result_id = $1
			ORDER BY step_number
		`, resultID)
		if err != nil {
			return fmt.Errorf("failed to query research steps: %w", err)
		}
		defer rows.Close()

		fmt.Println("Research Steps:")
		stepCount := 0
		for rows.Next() {
			var step models.ResearchStep
			if err := rows.Scan(
				&step.ID, &step.StepNumber, &step.Action, &step.InputSummary, &step.OutputSummary,
				&step.Reasoning, &step.Success, &step.ErrorMessage, &step.CreatedAt,
			); err != nil {
				return fmt.Errorf("failed to scan research step: %w", err)
			}
			stepCount++

			status := "OK"
			if !step.Success {
				status = "FAIL"
			}
			fmt.Printf("\n  Step %d: %s [%s]\n", step.StepNumber, step.Action, status)
			if step.Reasoning != "" {
				fmt.Printf("    Reason: %s\n", truncate(step.Reasoning, 100))
			}
			if step.InputSummary != "" {
				fmt.Printf("    Input:  %s\n", truncate(step.InputSummary, 80))
			}
			if step.OutputSummary != "" {
				fmt.Printf("    Output: %s\n", truncate(step.OutputSummary, 80))
			}
			if step.ErrorMessage != "" {
				fmt.Printf("    Error:  %s\n", step.ErrorMessage)
			}
		}

		if stepCount == 0 {
			fmt.Println("  (no research steps recorded)")
		}

		// If promoted to RFP, show that
		if sr.PromotedRFPID != nil {
			fmt.Printf("\nPromoted to RFP #%d\n", *sr.PromotedRFPID)
		}

		return nil
	},
}

// Research command
var researchCmd = &cobra.Command{
	Use:   "research [url]",
	Short: "Manually research a URL",
	Long:  `Run the research agent on a specific URL.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "https://" + url
		}

		ctx := context.Background()
		cfg := config.Load()
		if cfg.GeminiAPIKey == "" {
			return fmt.Errorf("GEMINI_API_KEY not set")
		}

		fmt.Printf("Researching: %s\n\n", url)

		// Run research via public API
		res, err := cliapi.ResearchURL(ctx, cfg.GeminiAPIKey, url)
		if err != nil {
			return fmt.Errorf("research failed: %w", err)
		}

		// Print results
		fmt.Printf("Status: %s\n", res.Status)
		fmt.Printf("Steps:  %d\n", res.StepsTaken)
		fmt.Printf("Tokens: %d\n", res.TotalTokens)
		fmt.Println()

		for _, step := range res.Steps {
			status := "OK"
			if !step.Success {
				status = "FAIL"
			}
			fmt.Printf("Step %d: %s [%s] (%dms)\n", step.StepNumber, step.Action, status, step.DurationMs)
			if step.Reasoning != "" {
				fmt.Printf("  %s\n", truncate(step.Reasoning, 100))
			}
		}

		if res.ExtractedDetails != nil {
			fmt.Println("\nExtracted Details:")
			if res.ExtractedDetails.Title != "" {
				fmt.Printf("  Title:    %s\n", res.ExtractedDetails.Title)
			}
			if res.ExtractedDetails.Agency != "" {
				fmt.Printf("  Agency:   %s\n", res.ExtractedDetails.Agency)
			}
			if res.ExtractedDetails.City != "" || res.ExtractedDetails.State != "" {
				fmt.Printf("  Location: %s, %s\n", res.ExtractedDetails.City, res.ExtractedDetails.State)
			}
			if res.ExtractedDetails.DueDate != "" {
				fmt.Printf("  Due Date: %s\n", res.ExtractedDetails.DueDate)
			}
			if res.ExtractedDetails.Category != "" {
				fmt.Printf("  Category: %s\n", res.ExtractedDetails.Category)
			}
			if res.ExtractedDetails.VenueType != "" {
				fmt.Printf("  Venue:    %s\n", res.ExtractedDetails.VenueType)
			}
			if res.ExtractedDetails.ScopeSummary != "" {
				fmt.Printf("  Scope:    %s\n", truncate(res.ExtractedDetails.ScopeSummary, 100))
			}
		}

		if len(res.FoundPDFs) > 0 {
			fmt.Println("\nFound PDFs:")
			for _, pdf := range res.FoundPDFs {
				fmt.Printf("  %s\n", pdf)
			}
		}

		return nil
	},
}

// Retry-failed command
var retryLimit int

var retryFailedCmd = &cobra.Command{
	Use:   "retry-failed",
	Short: "Retry failed research",
	Long:  `Reset failed search results to pending so they can be retried.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		database, err := connectDB(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer database.Close()

		// Reset failed results to pending
		result, err := database.Exec(ctx, `
			UPDATE discovery.search_results
			SET research_status = 'pending'
			WHERE research_status = 'failed'
			AND id IN (
				SELECT id FROM discovery.search_results
				WHERE research_status = 'failed'
				ORDER BY created_at DESC
				LIMIT $1
			)
		`, retryLimit)
		if err != nil {
			return fmt.Errorf("failed to reset failed results: %w", err)
		}

		count := result.RowsAffected()
		fmt.Printf("Reset %d failed results to pending\n", count)

		return nil
	},
}

func init() {
	retryFailedCmd.Flags().IntVar(&retryLimit, "limit", 100, "Maximum number of results to retry")
}

// Recent command
var recentDays int

var recentCmd = &cobra.Command{
	Use:   "recent",
	Short: "List recent discoveries",
	Long:  `Show RFPs discovered in the last N days.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		database, err := connectDB(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer database.Close()

		rows, err := database.Query(ctx, `
			SELECT id, title, agency, state, city, source_url, due_date, category, venue_type, discovered_at, is_active
			FROM discovery.rfps
			WHERE discovered_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY discovered_at DESC
		`, recentDays)
		if err != nil {
			return fmt.Errorf("failed to query recent RFPs: %w", err)
		}
		defer rows.Close()

		fmt.Printf("=== RFPs Discovered in Last %d Days ===\n\n", recentDays)

		count := 0
		for rows.Next() {
			var rfp models.RFP
			if err := rows.Scan(
				&rfp.ID, &rfp.Title, &rfp.Agency, &rfp.State, &rfp.City,
				&rfp.SourceURL, &rfp.DueDate, &rfp.Category, &rfp.VenueType,
				&rfp.DiscoveredAt, &rfp.IsActive,
			); err != nil {
				return fmt.Errorf("failed to scan RFP: %w", err)
			}
			count++

			active := ""
			if !rfp.IsActive {
				active = " [inactive]"
			}
			fmt.Printf("#%d: %s%s\n", rfp.ID, truncate(rfp.Title, 60), active)
			if rfp.Agency != "" {
				fmt.Printf("    Agency: %s\n", rfp.Agency)
			}
			location := formatLocation(rfp.City, rfp.State)
			if location != "" {
				fmt.Printf("    Location: %s\n", location)
			}
			if rfp.DueDate != nil {
				fmt.Printf("    Due: %s\n", rfp.DueDate.Format("2006-01-02"))
			}
			if rfp.Category != "" || rfp.VenueType != "" {
				fmt.Printf("    Type: %s / %s\n", rfp.Category, rfp.VenueType)
			}
			fmt.Printf("    Discovered: %s\n", rfp.DiscoveredAt.Format("2006-01-02 15:04"))
			fmt.Println()
		}

		if count == 0 {
			fmt.Println("No RFPs found in the specified time range.")
		} else {
			fmt.Printf("Total: %d RFPs\n", count)
		}

		return nil
	},
}

func init() {
	recentCmd.Flags().IntVar(&recentDays, "days", 7, "Number of days to look back")
}

// Export command
var exportFormat string
var exportSince string

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export discoveries",
	Long:  `Export discovered RFPs to JSON or CSV format.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		database, err := connectDB(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer database.Close()

		// Build query
		query := `
			SELECT id, title, agency, state, city, source_url, portal, portal_id,
			       posted_date, due_date, category, venue_type, scope_keywords,
			       term_months, estimated_value, incumbent, login_required,
			       pdf_urls, discovered_at, is_active
			FROM discovery.rfps
			WHERE 1=1
		`
		var queryArgs []any

		if exportSince != "" {
			sinceDate, err := time.Parse("2006-01-02", exportSince)
			if err != nil {
				return fmt.Errorf("invalid date format (use YYYY-MM-DD): %w", err)
			}
			query += " AND discovered_at >= $1"
			queryArgs = append(queryArgs, sinceDate)
		}

		query += " ORDER BY discovered_at DESC"

		rows, err := database.Query(ctx, query, queryArgs...)
		if err != nil {
			return fmt.Errorf("failed to query RFPs: %w", err)
		}
		defer rows.Close()

		var rfps []exportRFP
		for rows.Next() {
			var rfp exportRFP
			var scopeKeywords, pdfURLs []string
			if err := rows.Scan(
				&rfp.ID, &rfp.Title, &rfp.Agency, &rfp.State, &rfp.City,
				&rfp.SourceURL, &rfp.Portal, &rfp.PortalID,
				&rfp.PostedDate, &rfp.DueDate, &rfp.Category, &rfp.VenueType, &scopeKeywords,
				&rfp.TermMonths, &rfp.EstimatedValue, &rfp.Incumbent, &rfp.LoginRequired,
				&pdfURLs, &rfp.DiscoveredAt, &rfp.IsActive,
			); err != nil {
				return fmt.Errorf("failed to scan RFP: %w", err)
			}
			rfp.ScopeKeywords = scopeKeywords
			rfp.PDFURLs = pdfURLs
			rfps = append(rfps, rfp)
		}

		switch exportFormat {
		case "json":
			return exportJSON(rfps)
		case "csv":
			return exportCSV(rfps)
		default:
			return fmt.Errorf("unknown format: %s (use json or csv)", exportFormat)
		}
	},
}

type exportRFP struct {
	ID             int        `json:"id"`
	Title          string     `json:"title"`
	Agency         string     `json:"agency"`
	State          string     `json:"state"`
	City           string     `json:"city"`
	SourceURL      string     `json:"source_url"`
	Portal         string     `json:"portal"`
	PortalID       string     `json:"portal_id"`
	PostedDate     *time.Time `json:"posted_date"`
	DueDate        *time.Time `json:"due_date"`
	Category       string     `json:"category"`
	VenueType      string     `json:"venue_type"`
	ScopeKeywords  []string   `json:"scope_keywords"`
	TermMonths     *int       `json:"term_months"`
	EstimatedValue *float64   `json:"estimated_value"`
	Incumbent      string     `json:"incumbent"`
	LoginRequired  bool       `json:"login_required"`
	PDFURLs        []string   `json:"pdf_urls"`
	DiscoveredAt   time.Time  `json:"discovered_at"`
	IsActive       bool       `json:"is_active"`
}

func exportJSON(rfps []exportRFP) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(rfps)
}

func exportCSV(rfps []exportRFP) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	// Header
	if err := w.Write([]string{
		"id", "title", "agency", "state", "city", "source_url",
		"portal", "due_date", "category", "venue_type", "discovered_at", "is_active",
	}); err != nil {
		return err
	}

	// Rows
	for _, rfp := range rfps {
		dueDate := ""
		if rfp.DueDate != nil {
			dueDate = rfp.DueDate.Format("2006-01-02")
		}
		active := "true"
		if !rfp.IsActive {
			active = "false"
		}
		if err := w.Write([]string{
			strconv.Itoa(rfp.ID),
			rfp.Title,
			rfp.Agency,
			rfp.State,
			rfp.City,
			rfp.SourceURL,
			rfp.Portal,
			dueDate,
			rfp.Category,
			rfp.VenueType,
			rfp.DiscoveredAt.Format(time.RFC3339),
			active,
		}); err != nil {
			return err
		}
	}

	return nil
}

func init() {
	exportCmd.Flags().StringVar(&exportFormat, "format", "json", "Output format (json, csv)")
	exportCmd.Flags().StringVar(&exportSince, "since", "", "Export RFPs discovered since date (YYYY-MM-DD)")
}

// Helper functions

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func formatLocation(city, state string) string {
	if city != "" && state != "" {
		return city + ", " + state
	}
	if city != "" {
		return city
	}
	return state
}
