package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zachsouder/rfp/client/internal/middleware"
	"github.com/zachsouder/rfp/client/internal/scoring"
	"github.com/zachsouder/rfp/client/internal/templates"
)

// ScoringRulesPage renders the scoring rules admin page.
func (h *Handlers) ScoringRulesPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)

	// Fetch all rules (including disabled)
	rows, err := h.db.Query(ctx, `
		SELECT id, name, description, rule_type, config, weight, enabled
		FROM client.scoring_rules
		ORDER BY weight DESC
	`)
	if err != nil {
		slog.Error("failed to fetch scoring rules", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rules []ScoringRuleData
	for rows.Next() {
		var rule ScoringRuleData
		var config json.RawMessage
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Description, &rule.RuleType, &config, &rule.Weight, &rule.Enabled); err != nil {
			slog.Error("failed to scan rule", "error", err)
			continue
		}
		rule.ConfigJSON = string(config)
		rule.WeightPercent = int(rule.Weight * 100)
		rules = append(rules, rule)
	}

	// Get score distribution
	distribution, err := h.getScoreDistribution(ctx)
	if err != nil {
		slog.Error("failed to get score distribution", "error", err)
	}

	data := ScoringPageData{
		Rules:        rules,
		Distribution: distribution,
	}

	pageData := templates.PageData{
		Title:     "Scoring Rules",
		ActiveNav: "settings",
		User: &templates.User{
			ID:        user.ID,
			Email:     user.Email,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			IsAdmin:   user.IsAdmin(),
		},
		Data: data,
	}

	if err := h.templates.Render(w, "admin_scoring", pageData); err != nil {
		slog.Error("failed to render scoring template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// UpdateScoringRule handles updates to a scoring rule.
func (h *Handlers) UpdateScoringRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	ruleID, err := strconv.Atoi(r.FormValue("rule_id"))
	if err != nil {
		http.Error(w, "Invalid rule ID", http.StatusBadRequest)
		return
	}

	// Parse weight (comes as percentage 0-100)
	weightPercent, err := strconv.Atoi(r.FormValue("weight"))
	if err != nil || weightPercent < 0 || weightPercent > 100 {
		http.Error(w, "Invalid weight (must be 0-100)", http.StatusBadRequest)
		return
	}
	weight := float64(weightPercent) / 100.0

	enabled := r.FormValue("enabled") == "true"
	configJSON := r.FormValue("config")

	// Validate JSON
	var js json.RawMessage
	if err := json.Unmarshal([]byte(configJSON), &js); err != nil {
		http.Error(w, "Invalid JSON configuration", http.StatusBadRequest)
		return
	}

	// Update rule
	_, err = h.db.Exec(ctx, `
		UPDATE client.scoring_rules
		SET weight = $1, enabled = $2, config = $3
		WHERE id = $4
	`, weight, enabled, configJSON, ruleID)
	if err != nil {
		slog.Error("failed to update scoring rule", "error", err, "rule_id", ruleID)
		http.Error(w, "Failed to update rule", http.StatusInternalServerError)
		return
	}

	slog.Info("scoring rule updated", "rule_id", ruleID, "weight", weight, "enabled", enabled)
	http.Redirect(w, r, "/admin/scoring", http.StatusSeeOther)
}

// RefreshAllScores recalculates scores for all active RFPs.
func (h *Handlers) RefreshAllScores(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	scoringSvc := scoring.NewService(h.db.Pool)
	count, err := scoringSvc.RefreshAllScores(ctx)
	if err != nil {
		slog.Error("failed to refresh scores", "error", err)
		http.Error(w, "Failed to refresh scores", http.StatusInternalServerError)
		return
	}

	slog.Info("refreshed all scores", "count", count)
	http.Redirect(w, r, "/admin/scoring", http.StatusSeeOther)
}

// TestScoring tests scoring rules against a sample RFP.
func (h *Handlers) TestScoring(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	rfpID, err := strconv.Atoi(r.FormValue("rfp_id"))
	if err != nil {
		http.Error(w, "Invalid RFP ID", http.StatusBadRequest)
		return
	}

	scoringSvc := scoring.NewService(h.db.Pool)
	result, err := scoringSvc.ScoreRFP(ctx, rfpID)
	if err != nil {
		slog.Error("failed to test scoring", "error", err, "rfp_id", rfpID)
		http.Error(w, "Failed to score RFP: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Return JSON response for AJAX
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// getScoreDistribution returns score counts by bucket for chart display.
func (h *Handlers) getScoreDistribution(ctx context.Context) ([]ScoreDistributionBucket, error) {
	// Get distribution of scores in 0.5 increments from 1.0 to 5.0
	rows, err := h.db.Query(ctx, `
		SELECT
			FLOOR(COALESCE(manual_score, auto_score, 3) * 2) / 2 as bucket,
			COUNT(*) as count
		FROM client.rfp_tracking t
		JOIN discovery.rfps r ON t.discovery_rfp_id = r.id
		WHERE r.is_active = true
		GROUP BY bucket
		ORDER BY bucket
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Initialize all buckets
	bucketMap := make(map[float64]int)
	for i := 1.0; i <= 5.0; i += 0.5 {
		bucketMap[i] = 0
	}

	for rows.Next() {
		var bucket float64
		var count int
		if err := rows.Scan(&bucket, &count); err != nil {
			continue
		}
		if bucket >= 1.0 && bucket <= 5.0 {
			bucketMap[bucket] = count
		}
	}

	// Convert to slice
	var distribution []ScoreDistributionBucket
	for i := 1.0; i <= 5.0; i += 0.5 {
		distribution = append(distribution, ScoreDistributionBucket{
			Score: i,
			Label: formatScoreLabel(i),
			Count: bucketMap[i],
		})
	}

	return distribution, nil
}

func formatScoreLabel(score float64) string {
	if score == float64(int(score)) {
		return strconv.Itoa(int(score))
	}
	return strconv.FormatFloat(score, 'f', 1, 64)
}

// ScoringRuleData holds rule data for template display.
type ScoringRuleData struct {
	ID            int
	Name          string
	Description   string
	RuleType      string
	ConfigJSON    string
	Weight        float64
	WeightPercent int
	Enabled       bool
}

// ScoreDistributionBucket holds score distribution data for chart.
type ScoreDistributionBucket struct {
	Score float64
	Label string
	Count int
}

// ScoringPageData holds all data for the scoring admin page.
type ScoringPageData struct {
	Rules        []ScoringRuleData
	Distribution []ScoreDistributionBucket
}

// UserManagementPage renders the user management admin page.
func (h *Handlers) UserManagementPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)

	users, err := h.authService.ListUsers(ctx)
	if err != nil {
		slog.Error("failed to list users", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var userList []UserData
	for _, u := range users {
		ud := UserData{
			ID:        u.ID,
			Email:     u.Email,
			FirstName: u.FirstName,
			LastName:  u.LastName,
			Role:      u.Role,
			CreatedAt: u.CreatedAt.Format("Jan 2, 2006"),
		}
		if u.FirstName != "" || u.LastName != "" {
			ud.Name = u.FirstName + " " + u.LastName
		} else {
			ud.Name = u.Email
		}
		if u.LastActiveAt != nil {
			ud.LastActive = u.LastActiveAt.Format("Jan 2, 2006 3:04 PM")
		} else {
			ud.LastActive = "Never"
		}
		ud.IsCurrentUser = u.ID == user.ID
		userList = append(userList, ud)
	}

	// Check for flash message (temp password from invite)
	flash := r.URL.Query().Get("flash")
	tempPassword := r.URL.Query().Get("temp_password")

	data := UserManagementPageData{
		Users:        userList,
		Flash:        flash,
		TempPassword: tempPassword,
	}

	pageData := templates.PageData{
		Title:     "User Management",
		ActiveNav: "settings",
		User: &templates.User{
			ID:        user.ID,
			Email:     user.Email,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			IsAdmin:   user.IsAdmin(),
		},
		Data: data,
	}

	if err := h.templates.Render(w, "admin_users", pageData); err != nil {
		slog.Error("failed to render users template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// InviteUser creates a new user and shows their temporary password.
func (h *Handlers) InviteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	email := r.FormValue("email")
	firstName := r.FormValue("first_name")
	lastName := r.FormValue("last_name")
	role := r.FormValue("role")

	if email == "" {
		http.Error(w, "Email is required", http.StatusBadRequest)
		return
	}

	if role != "admin" && role != "member" {
		role = "member"
	}

	_, tempPassword, err := h.authService.CreateUser(ctx, email, firstName, lastName, role)
	if err != nil {
		slog.Error("failed to create user", "error", err, "email", email)
		http.Error(w, "Failed to create user (email may already exist)", http.StatusBadRequest)
		return
	}

	slog.Info("user invited", "email", email, "role", role)

	// Redirect with temp password in query (for display only)
	http.Redirect(w, r, "/admin/users?flash=User+invited+successfully&temp_password="+tempPassword, http.StatusSeeOther)
}

// DeactivateUser logs out a user by deleting their sessions.
func (h *Handlers) DeactivateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := middleware.GetUser(ctx)

	userIDStr := chi.URLParam(r, "id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Prevent self-deactivation
	if userID == currentUser.ID {
		http.Error(w, "Cannot deactivate yourself", http.StatusBadRequest)
		return
	}

	if err := h.authService.DeactivateUser(ctx, userID); err != nil {
		slog.Error("failed to deactivate user", "error", err, "user_id", userID)
		http.Error(w, "Failed to deactivate user", http.StatusInternalServerError)
		return
	}

	slog.Info("user deactivated", "user_id", userID)
	http.Redirect(w, r, "/admin/users?flash=User+deactivated", http.StatusSeeOther)
}

// UserData holds user data for template display.
type UserData struct {
	ID            int
	Email         string
	Name          string
	FirstName     string
	LastName      string
	Role          string
	LastActive    string
	CreatedAt     string
	IsCurrentUser bool
}

// UserManagementPageData holds data for the user management page.
type UserManagementPageData struct {
	Users        []UserData
	Flash        string
	TempPassword string
}
