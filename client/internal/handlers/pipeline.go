package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/zachsouder/rfp/client/internal/middleware"
	"github.com/zachsouder/rfp/client/internal/templates"
	"github.com/zachsouder/rfp/client/internal/workflow"
)

// PipelineStage represents a column in the kanban board.
type PipelineStage struct {
	ID      string
	Name    string
	RFPs    []PipelineCard
	Count   int
	IsEnded bool
}

// PipelineCard represents an RFP card in the pipeline.
type PipelineCard struct {
	ID               int
	Title            string
	Agency           string
	State            string
	DueDate          string
	DaysUntilDue     *int
	IsUrgent         bool
	Score            *float64
	ScoreDisplay     string
	AssigneeName     string
	AssigneeInitials string
}

// PipelinePageData holds data for the pipeline template.
type PipelinePageData struct {
	Stages       []PipelineStage
	EndedStages  []PipelineStage
	Users        []PipelineUser
	FilterUser   int
	FilterMinScore float64
	ShowEnded    bool
}

// PipelineUser for filter dropdown.
type PipelineUser struct {
	ID   int
	Name string
}

// Pipeline renders the kanban pipeline view.
func (h *Handlers) Pipeline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)

	// Parse filters
	filterUser, _ := strconv.Atoi(r.URL.Query().Get("user"))
	filterMinScore, _ := strconv.ParseFloat(r.URL.Query().Get("min_score"), 64)
	showEnded := r.URL.Query().Get("show_ended") == "true"

	// Active pipeline stages
	activeStages := []string{
		workflow.StageNew,
		workflow.StageReviewing,
		workflow.StageQualified,
		workflow.StagePursuing,
		workflow.StageSubmitted,
	}

	// Ended stages (outcomes)
	endedStagesList := []string{
		workflow.StageWon,
		workflow.StageLost,
		workflow.StagePassed,
	}

	stageNames := map[string]string{
		workflow.StageNew:       "New",
		workflow.StageReviewing: "Reviewing",
		workflow.StageQualified: "Qualified",
		workflow.StagePursuing:  "Pursuing",
		workflow.StageSubmitted: "Submitted",
		workflow.StageWon:       "Won",
		workflow.StageLost:      "Lost",
		workflow.StagePassed:    "Passed",
	}

	// Build query
	query := `
		SELECT r.id, r.title, r.agency, r.state, r.due_date,
		       COALESCE(t.manual_score, t.auto_score) as score,
		       COALESCE(t.stage, 'new') as stage,
		       t.assigned_to,
		       COALESCE(u.first_name || ' ' || u.last_name, u.email) as assignee_name
		FROM discovery.rfps r
		LEFT JOIN client.rfp_tracking t ON r.id = t.discovery_rfp_id
		LEFT JOIN client.users u ON t.assigned_to = u.id
		WHERE r.is_active = true AND (t.is_hidden IS NULL OR t.is_hidden = false)
	`
	args := []any{}
	argIdx := 1

	if filterUser > 0 {
		query += " AND t.assigned_to = $" + strconv.Itoa(argIdx)
		args = append(args, filterUser)
		argIdx++
	}

	if filterMinScore > 0 {
		query += " AND COALESCE(t.manual_score, t.auto_score, 0) >= $" + strconv.Itoa(argIdx)
		args = append(args, filterMinScore)
		argIdx++
	}

	query += " ORDER BY r.due_date ASC NULLS LAST"

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		slog.Error("failed to query pipeline", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Group by stage
	stageCards := make(map[string][]PipelineCard)
	now := time.Now()

	for rows.Next() {
		var card PipelineCard
		var dueDate *time.Time
		var score *float64
		var stage string
		var assignedTo *int
		var assigneeName *string

		if err := rows.Scan(&card.ID, &card.Title, &card.Agency, &card.State, &dueDate,
			&score, &stage, &assignedTo, &assigneeName); err != nil {
			slog.Error("failed to scan pipeline row", "error", err)
			continue
		}

		card.Score = score
		if score != nil {
			card.ScoreDisplay = strconv.FormatFloat(*score, 'f', 1, 64)
		}

		if dueDate != nil {
			card.DueDate = dueDate.Format("Jan 2")
			daysUntil := int(dueDate.Sub(now).Hours() / 24)
			card.DaysUntilDue = &daysUntil
			card.IsUrgent = daysUntil <= 7 && daysUntil >= 0
		}

		if assigneeName != nil {
			card.AssigneeName = *assigneeName
			card.AssigneeInitials = getInitials(*assigneeName)
		}

		stageCards[stage] = append(stageCards[stage], card)
	}

	// Build stage structs
	var stages []PipelineStage
	for _, stageID := range activeStages {
		cards := stageCards[stageID]
		stages = append(stages, PipelineStage{
			ID:      stageID,
			Name:    stageNames[stageID],
			RFPs:    cards,
			Count:   len(cards),
			IsEnded: false,
		})
	}

	var endedStages []PipelineStage
	for _, stageID := range endedStagesList {
		cards := stageCards[stageID]
		endedStages = append(endedStages, PipelineStage{
			ID:      stageID,
			Name:    stageNames[stageID],
			RFPs:    cards,
			Count:   len(cards),
			IsEnded: true,
		})
	}

	// Get users for filter dropdown
	userRows, err := h.db.Query(ctx, `
		SELECT id, COALESCE(first_name || ' ' || last_name, email) as name
		FROM client.users
		ORDER BY name
	`)
	if err != nil {
		slog.Error("failed to get users for filter", "error", err)
	}
	var users []PipelineUser
	if userRows != nil {
		defer userRows.Close()
		for userRows.Next() {
			var u PipelineUser
			if err := userRows.Scan(&u.ID, &u.Name); err == nil {
				users = append(users, u)
			}
		}
	}

	data := PipelinePageData{
		Stages:         stages,
		EndedStages:    endedStages,
		Users:          users,
		FilterUser:     filterUser,
		FilterMinScore: filterMinScore,
		ShowEnded:      showEnded,
	}

	pageData := templates.PageData{
		Title:     "Pipeline",
		ActiveNav: "pipeline",
		User: &templates.User{
			ID:        user.ID,
			Email:     user.Email,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			IsAdmin:   user.IsAdmin(),
		},
		Data: data,
	}

	if err := h.templates.Render(w, "pipeline", pageData); err != nil {
		slog.Error("failed to render pipeline template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// PipelineMoveCard handles drag-drop stage changes via AJAX.
func (h *Handlers) PipelineMoveCard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)

	var req struct {
		RFPID int    `json:"rfp_id"`
		Stage string `json:"stage"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if !workflow.IsValidStage(req.Stage) {
		http.Error(w, "Invalid stage", http.StatusBadRequest)
		return
	}

	workflowSvc := workflow.NewService(h.db.Pool)
	if err := workflowSvc.UpdateStage(ctx, req.RFPID, req.Stage, user.ID); err != nil {
		slog.Error("failed to move card", "error", err, "rfp_id", req.RFPID, "stage", req.Stage)
		http.Error(w, "Failed to update stage", http.StatusInternalServerError)
		return
	}

	slog.Info("pipeline card moved", "rfp_id", req.RFPID, "stage", req.Stage, "user_id", user.ID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// getInitials returns initials from a name.
func getInitials(name string) string {
	if name == "" {
		return ""
	}
	initials := ""
	words := 0
	for i, r := range name {
		if i == 0 || (i > 0 && name[i-1] == ' ') {
			initials += string(r)
			words++
			if words >= 2 {
				break
			}
		}
	}
	return initials
}
