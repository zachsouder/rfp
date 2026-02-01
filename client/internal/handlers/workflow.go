package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zsouder/rfp/client/internal/middleware"
	"github.com/zsouder/rfp/client/internal/workflow"
)

// UpdateStage handles stage change requests.
func (h *Handlers) UpdateStage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)
	idStr := chi.URLParam(r, "id")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid RFP ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	stage := r.FormValue("stage")
	if !workflow.IsValidStage(stage) {
		http.Error(w, "Invalid stage", http.StatusBadRequest)
		return
	}

	workflowSvc := workflow.NewService(h.db.Pool)
	if err := workflowSvc.UpdateStage(ctx, id, stage, user.ID); err != nil {
		slog.Error("failed to update stage", "error", err, "rfp_id", id, "stage", stage)
		http.Error(w, "Failed to update stage", http.StatusInternalServerError)
		return
	}

	slog.Info("stage updated", "rfp_id", id, "stage", stage, "user_id", user.ID)
	http.Redirect(w, r, "/rfps/"+idStr, http.StatusSeeOther)
}

// UpdateScore handles score change requests.
func (h *Handlers) UpdateScore(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)
	idStr := chi.URLParam(r, "id")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid RFP ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	scoreStr := r.FormValue("score")
	score, err := strconv.ParseFloat(scoreStr, 64)
	if err != nil || score < 1.0 || score > 5.0 {
		http.Error(w, "Score must be between 1.0 and 5.0", http.StatusBadRequest)
		return
	}

	workflowSvc := workflow.NewService(h.db.Pool)
	if err := workflowSvc.UpdateScore(ctx, id, score); err != nil {
		slog.Error("failed to update score", "error", err, "rfp_id", id, "score", score)
		http.Error(w, "Failed to update score", http.StatusInternalServerError)
		return
	}

	slog.Info("score updated", "rfp_id", id, "score", score, "user_id", user.ID)
	http.Redirect(w, r, "/rfps/"+idStr, http.StatusSeeOther)
}

// AddNote handles adding notes to an RFP.
func (h *Handlers) AddNote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)
	idStr := chi.URLParam(r, "id")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid RFP ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	content := r.FormValue("content")
	if content == "" {
		http.Redirect(w, r, "/rfps/"+idStr, http.StatusSeeOther)
		return
	}

	// Ensure tracking record exists
	workflowSvc := workflow.NewService(h.db.Pool)
	_, err = workflowSvc.GetTracking(ctx, id)
	if err != nil {
		// Create a tracking record first
		if err := workflowSvc.UpdateStage(ctx, id, workflow.StageNew, user.ID); err != nil {
			slog.Error("failed to create tracking record", "error", err, "rfp_id", id)
			http.Error(w, "Failed to add note", http.StatusInternalServerError)
			return
		}
	}

	// Get tracking ID
	tracking, err := workflowSvc.GetTracking(ctx, id)
	if err != nil {
		slog.Error("failed to get tracking record", "error", err, "rfp_id", id)
		http.Error(w, "Failed to add note", http.StatusInternalServerError)
		return
	}

	// Insert note
	_, err = h.db.Exec(ctx, `
		INSERT INTO client.notes (rfp_tracking_id, author_id, content)
		VALUES ($1, $2, $3)
	`, tracking.ID, user.ID, content)
	if err != nil {
		slog.Error("failed to add note", "error", err, "rfp_id", id)
		http.Error(w, "Failed to add note", http.StatusInternalServerError)
		return
	}

	slog.Info("note added", "rfp_id", id, "user_id", user.ID)
	http.Redirect(w, r, "/rfps/"+idStr, http.StatusSeeOther)
}
