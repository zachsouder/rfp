package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/zachsouder/rfp/client/internal/middleware"
	"github.com/zachsouder/rfp/client/internal/templates"
)

// DashboardData contains data for the dashboard template.
type DashboardData struct {
	NewThisWeek       int
	InPipeline        int
	DueThisWeek       int
	TotalActive       int
	NeedsReview       []RFPSummary
	UpcomingDeadlines []RFPDeadline
}

// RFPSummary is a brief RFP view for lists.
type RFPSummary struct {
	ID     int
	Title  string
	Agency string
	State  string
	Stage  string
}

// RFPDeadline is an RFP with deadline info.
type RFPDeadline struct {
	ID      int
	Title   string
	Agency  string
	DueDate string
	Urgent  bool
}

// Dashboard renders the dashboard page.
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)

	// Prepare dashboard data
	data := DashboardData{}

	// New this week - count RFPs discovered in last 7 days
	err := h.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM discovery.rfps
		WHERE discovered_at > NOW() - INTERVAL '7 days' AND is_active = true
	`).Scan(&data.NewThisWeek)
	if err != nil {
		slog.Error("failed to count new RFPs", "error", err)
	}

	// In pipeline - count tracking records not in terminal stages
	err = h.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM client.rfp_tracking
		WHERE stage NOT IN ('won', 'lost', 'passed') AND is_hidden = false
	`).Scan(&data.InPipeline)
	if err != nil {
		slog.Error("failed to count pipeline RFPs", "error", err)
	}

	// Due this week
	err = h.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM discovery.rfps r
		JOIN client.rfp_tracking t ON r.id = t.discovery_rfp_id
		WHERE r.due_date BETWEEN NOW() AND NOW() + INTERVAL '7 days'
		AND t.stage NOT IN ('won', 'lost', 'passed', 'submitted')
		AND t.is_hidden = false
	`).Scan(&data.DueThisWeek)
	if err != nil {
		slog.Error("failed to count due this week", "error", err)
	}

	// Total active
	err = h.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM discovery.rfps WHERE is_active = true
	`).Scan(&data.TotalActive)
	if err != nil {
		slog.Error("failed to count total active", "error", err)
	}

	// Needs review - RFPs in 'new' stage
	rows, err := h.db.Query(ctx, `
		SELECT r.id, r.title, r.agency, r.state, t.stage
		FROM discovery.rfps r
		JOIN client.rfp_tracking t ON r.id = t.discovery_rfp_id
		WHERE t.stage = 'new' AND t.is_hidden = false
		ORDER BY r.discovered_at DESC
		LIMIT 5
	`)
	if err != nil {
		slog.Error("failed to fetch needs review", "error", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var rfp RFPSummary
			if err := rows.Scan(&rfp.ID, &rfp.Title, &rfp.Agency, &rfp.State, &rfp.Stage); err != nil {
				slog.Error("failed to scan RFP summary", "error", err)
				continue
			}
			data.NeedsReview = append(data.NeedsReview, rfp)
		}
	}

	// Upcoming deadlines
	now := time.Now()
	weekFromNow := now.AddDate(0, 0, 7)
	rows, err = h.db.Query(ctx, `
		SELECT r.id, r.title, r.agency, r.due_date
		FROM discovery.rfps r
		JOIN client.rfp_tracking t ON r.id = t.discovery_rfp_id
		WHERE r.due_date IS NOT NULL
		AND r.due_date > NOW()
		AND t.stage NOT IN ('won', 'lost', 'passed', 'submitted')
		AND t.is_hidden = false
		ORDER BY r.due_date ASC
		LIMIT 5
	`)
	if err != nil {
		slog.Error("failed to fetch upcoming deadlines", "error", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var rfp RFPDeadline
			var dueDate time.Time
			if err := rows.Scan(&rfp.ID, &rfp.Title, &rfp.Agency, &dueDate); err != nil {
				slog.Error("failed to scan RFP deadline", "error", err)
				continue
			}
			rfp.DueDate = dueDate.Format("Jan 2")
			rfp.Urgent = dueDate.Before(weekFromNow)
			data.UpcomingDeadlines = append(data.UpcomingDeadlines, rfp)
		}
	}

	// Render template
	pageData := templates.PageData{
		Title:     "Dashboard",
		ActiveNav: "dashboard",
		User: &templates.User{
			ID:        user.ID,
			Email:     user.Email,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			IsAdmin:   user.IsAdmin(),
		},
		Data: data,
	}

	if err := h.templates.Render(w, "dashboard", pageData); err != nil {
		slog.Error("failed to render dashboard template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
