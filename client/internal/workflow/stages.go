// Package workflow provides pipeline stage management for RFP tracking.
package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zachsouder/rfp/client/internal/scoring"
)

// Valid pipeline stages.
const (
	StageNew       = "new"
	StageReviewing = "reviewing"
	StageQualified = "qualified"
	StagePursuing  = "pursuing"
	StageSubmitted = "submitted"
	StageWon       = "won"
	StageLost      = "lost"
	StagePassed    = "passed"
)

// AllStages is the list of all valid stages.
var AllStages = []string{
	StageNew,
	StageReviewing,
	StageQualified,
	StagePursuing,
	StageSubmitted,
	StageWon,
	StageLost,
	StagePassed,
}

// Querier is an interface for database query methods.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Service provides workflow operations.
type Service struct {
	db Querier
}

// NewService creates a new workflow service.
func NewService(db Querier) *Service {
	return &Service{db: db}
}

// IsValidStage checks if a stage is valid.
func IsValidStage(stage string) bool {
	for _, s := range AllStages {
		if s == stage {
			return true
		}
	}
	return false
}

// UpdateStage updates the stage of an RFP tracking record.
// It creates a tracking record if one doesn't exist.
func (s *Service) UpdateStage(ctx context.Context, rfpID int, stage string, userID int) error {
	if !IsValidStage(stage) {
		return fmt.Errorf("invalid stage: %s", stage)
	}

	now := time.Now()

	// Try to update existing tracking record
	result, err := s.db.Exec(ctx, `
		UPDATE client.rfp_tracking
		SET stage = $1, stage_changed_at = $2, stage_changed_by = $3, updated_at = $2
		WHERE discovery_rfp_id = $4
	`, stage, now, userID, rfpID)
	if err != nil {
		return fmt.Errorf("failed to update stage: %w", err)
	}

	// If no rows updated, create a new tracking record and apply auto-scoring
	if result.RowsAffected() == 0 {
		_, err = s.db.Exec(ctx, `
			INSERT INTO client.rfp_tracking
			(discovery_rfp_id, stage, stage_changed_at, stage_changed_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $3, $3)
		`, rfpID, stage, now, userID)
		if err != nil {
			return fmt.Errorf("failed to create tracking record: %w", err)
		}

		// Apply auto-scoring to newly tracked RFP
		scoringSvc := scoring.NewService(s.db)
		if _, err := scoringSvc.ApplyScore(ctx, rfpID); err != nil {
			// Log but don't fail - scoring is non-critical
			slog.Warn("failed to apply auto-score", "error", err, "rfp_id", rfpID)
		}
	}

	return nil
}

// UpdateScore sets a manual score for an RFP.
func (s *Service) UpdateScore(ctx context.Context, rfpID int, score float64) error {
	if score < 1.0 || score > 5.0 {
		return fmt.Errorf("score must be between 1.0 and 5.0")
	}

	now := time.Now()

	// Try to update existing tracking record
	result, err := s.db.Exec(ctx, `
		UPDATE client.rfp_tracking
		SET manual_score = $1, updated_at = $2
		WHERE discovery_rfp_id = $3
	`, score, now, rfpID)
	if err != nil {
		return fmt.Errorf("failed to update score: %w", err)
	}

	// If no rows updated, create a new tracking record
	if result.RowsAffected() == 0 {
		_, err = s.db.Exec(ctx, `
			INSERT INTO client.rfp_tracking
			(discovery_rfp_id, manual_score, stage, created_at, updated_at)
			VALUES ($1, $2, 'new', $3, $3)
		`, rfpID, score, now)
		if err != nil {
			return fmt.Errorf("failed to create tracking record: %w", err)
		}
	}

	return nil
}

// GetTracking gets the tracking record for an RFP.
func (s *Service) GetTracking(ctx context.Context, rfpID int) (*Tracking, error) {
	var t Tracking
	err := s.db.QueryRow(ctx, `
		SELECT id, discovery_rfp_id, stage, stage_changed_at, stage_changed_by,
		       auto_score, manual_score, assigned_to, priority, is_hidden,
		       created_at, updated_at
		FROM client.rfp_tracking
		WHERE discovery_rfp_id = $1
	`, rfpID).Scan(
		&t.ID, &t.DiscoveryRFPID, &t.Stage, &t.StageChangedAt, &t.StageChangedBy,
		&t.AutoScore, &t.ManualScore, &t.AssignedTo, &t.Priority, &t.IsHidden,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Tracking represents an RFP tracking record.
type Tracking struct {
	ID             int
	DiscoveryRFPID int
	Stage          string
	StageChangedAt *time.Time
	StageChangedBy *int
	AutoScore      *float64
	ManualScore    *float64
	AssignedTo     *int
	Priority       string
	IsHidden       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// EffectiveScore returns the manual score if set, otherwise the auto score.
func (t *Tracking) EffectiveScore() *float64 {
	if t.ManualScore != nil {
		return t.ManualScore
	}
	return t.AutoScore
}
