package models

import (
	"encoding/json"
	"time"
)

// User represents a client application user.
type User struct {
	ID           int        `json:"id"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"` // Never serialize
	FirstName    string     `json:"first_name,omitempty"`
	LastName     string     `json:"last_name,omitempty"`
	Role         string     `json:"role"` // admin, member
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// Session represents a user session.
type Session struct {
	ID        string    `json:"id"`
	UserID    int       `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RFPTracking represents a client's view/tracking of an RFP.
type RFPTracking struct {
	ID             int       `json:"id"`
	DiscoveryRFPID int       `json:"discovery_rfp_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	// Pipeline stage
	Stage          string     `json:"stage"` // new, reviewing, qualified, pursuing, submitted, won, lost, passed
	StageChangedAt *time.Time `json:"stage_changed_at,omitempty"`
	StageChangedBy *int       `json:"stage_changed_by,omitempty"`

	// Scoring
	AutoScore    *float64        `json:"auto_score,omitempty"`   // 1.0-5.0
	ManualScore  *float64        `json:"manual_score,omitempty"` // Override
	ScoreReasons json.RawMessage `json:"score_reasons,omitempty"`

	// Workflow
	AssignedTo   *int       `json:"assigned_to,omitempty"`
	Priority     string     `json:"priority"` // high, normal, low
	DecisionDate *time.Time `json:"decision_date,omitempty"`

	// Visibility
	IsHidden bool `json:"is_hidden"`
}

// Note represents a note on an RFP.
type Note struct {
	ID            int       `json:"id"`
	RFPTrackingID int       `json:"rfp_tracking_id"`
	AuthorID      int       `json:"author_id"`
	Content       string    `json:"content"`
	CreatedAt     time.Time `json:"created_at"`
}

// Attachment represents an uploaded file on an RFP.
type Attachment struct {
	ID            int       `json:"id"`
	RFPTrackingID int       `json:"rfp_tracking_id"`
	UploadedBy    int       `json:"uploaded_by"`
	Filename      string    `json:"filename"`
	FilePath      string    `json:"file_path"` // R2 path
	FileSize      int       `json:"file_size,omitempty"`
	ContentType   string    `json:"content_type,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ScoringRule represents a client-specific scoring rule.
type ScoringRule struct {
	ID          int             `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	RuleType    string          `json:"rule_type"` // venue_match, scope_match, geography, term_length
	Config      json.RawMessage `json:"config"`
	Weight      float64         `json:"weight"` // Relative weight in scoring
	Enabled     bool            `json:"enabled"`
	CreatedAt   time.Time       `json:"created_at"`
}

// EmailSubscription represents user email preferences.
type EmailSubscription struct {
	ID              int       `json:"id"`
	UserID          int       `json:"user_id"`
	DigestEnabled   bool      `json:"digest_enabled"`
	DigestFrequency string    `json:"digest_frequency"` // daily, weekly, never
	CreatedAt       time.Time `json:"created_at"`
}
