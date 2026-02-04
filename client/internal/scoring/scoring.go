// Package scoring provides automated RFP scoring based on configurable rules.
package scoring

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is an interface for database query methods.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Rule represents a scoring rule from the database.
type Rule struct {
	ID          int
	Name        string
	Description string
	RuleType    string
	Config      json.RawMessage
	Weight      float64
	Enabled     bool
}

// RFPData holds the RFP fields needed for scoring.
type RFPData struct {
	ID             int
	VenueType      *string
	ScopeKeywords  []string
	State          *string
	TermMonths     *int
	DueDate        *time.Time
	EstimatedValue *float64
}

// ScoreResult holds the calculated score and breakdown.
type ScoreResult struct {
	Score   float64            `json:"score"`
	Reasons []ScoreReason      `json:"reasons"`
}

// ScoreReason explains how a single rule contributed to the score.
type ScoreReason struct {
	RuleName    string  `json:"rule_name"`
	RuleType    string  `json:"rule_type"`
	Score       float64 `json:"score"`
	Weight      float64 `json:"weight"`
	Contribution float64 `json:"contribution"`
	Explanation string  `json:"explanation"`
}

// Service provides scoring operations.
type Service struct {
	db Querier
}

// NewService creates a new scoring service.
func NewService(db Querier) *Service {
	return &Service{db: db}
}

// LoadRules fetches all enabled scoring rules from the database.
func (s *Service) LoadRules(ctx context.Context) ([]Rule, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, description, rule_type, config, weight, enabled
		FROM client.scoring_rules
		WHERE enabled = true
		ORDER BY weight DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to load scoring rules: %w", err)
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.RuleType, &r.Config, &r.Weight, &r.Enabled); err != nil {
			return nil, fmt.Errorf("failed to scan rule: %w", err)
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// GetRFPData fetches the RFP data needed for scoring.
func (s *Service) GetRFPData(ctx context.Context, rfpID int) (*RFPData, error) {
	var data RFPData
	data.ID = rfpID

	err := s.db.QueryRow(ctx, `
		SELECT venue_type, scope_keywords, state, term_months, due_date, estimated_value
		FROM discovery.rfps
		WHERE id = $1
	`, rfpID).Scan(&data.VenueType, &data.ScopeKeywords, &data.State, &data.TermMonths, &data.DueDate, &data.EstimatedValue)
	if err != nil {
		return nil, fmt.Errorf("failed to get RFP data: %w", err)
	}
	return &data, nil
}

// ScoreRFP calculates the score for an RFP based on all enabled rules.
func (s *Service) ScoreRFP(ctx context.Context, rfpID int) (*ScoreResult, error) {
	rules, err := s.LoadRules(ctx)
	if err != nil {
		return nil, err
	}

	rfp, err := s.GetRFPData(ctx, rfpID)
	if err != nil {
		return nil, err
	}

	return s.CalculateScore(rules, rfp)
}

// CalculateScore computes the weighted score from rules and RFP data.
func (s *Service) CalculateScore(rules []Rule, rfp *RFPData) (*ScoreResult, error) {
	result := &ScoreResult{
		Reasons: make([]ScoreReason, 0, len(rules)),
	}

	totalWeight := 0.0
	weightedSum := 0.0

	for _, rule := range rules {
		score, explanation := evaluateRule(rule, rfp)
		contribution := score * rule.Weight

		reason := ScoreReason{
			RuleName:     rule.Name,
			RuleType:     rule.RuleType,
			Score:        score,
			Weight:       rule.Weight,
			Contribution: contribution,
			Explanation:  explanation,
		}
		result.Reasons = append(result.Reasons, reason)

		totalWeight += rule.Weight
		weightedSum += contribution
	}

	// Normalize to 1-5 scale
	if totalWeight > 0 {
		// weightedSum is already in 1-5 scale per rule, so just normalize by total weight
		result.Score = weightedSum / totalWeight
	} else {
		result.Score = 3.0 // neutral score if no rules
	}

	// Clamp to valid range
	if result.Score < 1.0 {
		result.Score = 1.0
	}
	if result.Score > 5.0 {
		result.Score = 5.0
	}

	return result, nil
}

// evaluateRule applies a single rule to RFP data, returning score (1-5) and explanation.
func evaluateRule(rule Rule, rfp *RFPData) (float64, string) {
	switch rule.RuleType {
	case "venue_match":
		return evaluateVenueMatch(rule.Config, rfp)
	case "scope_match":
		return evaluateScopeMatch(rule.Config, rfp)
	case "geography":
		return evaluateGeography(rule.Config, rfp)
	case "term_length":
		return evaluateTermLength(rule.Config, rfp)
	case "time_to_due":
		return evaluateTimeToDue(rule.Config, rfp)
	case "value":
		return evaluateValue(rule.Config, rfp)
	default:
		return 3.0, "Unknown rule type"
	}
}

// VenueMatchConfig for venue_match rule type.
type VenueMatchConfig struct {
	Positive []string `json:"positive"`
	Negative []string `json:"negative"`
}

func evaluateVenueMatch(config json.RawMessage, rfp *RFPData) (float64, string) {
	var cfg VenueMatchConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return 3.0, "Invalid config"
	}

	if rfp.VenueType == nil || *rfp.VenueType == "" {
		return 3.0, "No venue type specified"
	}

	venueLower := strings.ToLower(*rfp.VenueType)

	// Check negatives first (they override positives)
	for _, neg := range cfg.Negative {
		if strings.Contains(venueLower, strings.ToLower(neg)) {
			return 1.5, fmt.Sprintf("Venue '%s' matches negative keyword '%s'", *rfp.VenueType, neg)
		}
	}

	// Check positives
	for _, pos := range cfg.Positive {
		if strings.Contains(venueLower, strings.ToLower(pos)) {
			return 5.0, fmt.Sprintf("Venue '%s' matches preferred keyword '%s'", *rfp.VenueType, pos)
		}
	}

	return 3.0, fmt.Sprintf("Venue '%s' is neutral", *rfp.VenueType)
}

// ScopeMatchConfig for scope_match rule type.
type ScopeMatchConfig struct {
	Positive []string `json:"positive"`
	Negative []string `json:"negative"`
}

func evaluateScopeMatch(config json.RawMessage, rfp *RFPData) (float64, string) {
	var cfg ScopeMatchConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return 3.0, "Invalid config"
	}

	if len(rfp.ScopeKeywords) == 0 {
		return 3.0, "No scope keywords specified"
	}

	positiveMatches := 0
	negativeMatches := 0

	for _, kw := range rfp.ScopeKeywords {
		kwLower := strings.ToLower(kw)
		for _, neg := range cfg.Negative {
			if strings.Contains(kwLower, strings.ToLower(neg)) {
				negativeMatches++
			}
		}
		for _, pos := range cfg.Positive {
			if strings.Contains(kwLower, strings.ToLower(pos)) {
				positiveMatches++
			}
		}
	}

	if negativeMatches > 0 && positiveMatches == 0 {
		return 1.5, fmt.Sprintf("Scope has %d negative matches, no positive", negativeMatches)
	}
	if positiveMatches > 0 && negativeMatches == 0 {
		score := 3.0 + float64(positiveMatches)*0.5
		if score > 5.0 {
			score = 5.0
		}
		return score, fmt.Sprintf("Scope has %d positive keyword matches", positiveMatches)
	}
	if positiveMatches > 0 && negativeMatches > 0 {
		return 2.5, fmt.Sprintf("Scope has mixed signals: %d positive, %d negative", positiveMatches, negativeMatches)
	}

	return 3.0, "Scope keywords are neutral"
}

// GeographyConfig for geography rule type.
type GeographyConfig struct {
	PreferredStates []string `json:"preferred_states"`
	ExcludedStates  []string `json:"excluded_states"`
}

func evaluateGeography(config json.RawMessage, rfp *RFPData) (float64, string) {
	var cfg GeographyConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return 3.0, "Invalid config"
	}

	if rfp.State == nil || *rfp.State == "" {
		return 3.0, "No state specified"
	}

	stateLower := strings.ToLower(*rfp.State)

	// Check excluded first
	for _, exc := range cfg.ExcludedStates {
		if strings.EqualFold(stateLower, exc) {
			return 1.0, fmt.Sprintf("State '%s' is excluded", *rfp.State)
		}
	}

	// Check preferred
	for _, pref := range cfg.PreferredStates {
		if strings.EqualFold(stateLower, pref) {
			return 5.0, fmt.Sprintf("State '%s' is preferred", *rfp.State)
		}
	}

	// If no preferences configured, neutral
	if len(cfg.PreferredStates) == 0 && len(cfg.ExcludedStates) == 0 {
		return 3.0, "No geographic preferences configured"
	}

	return 3.0, fmt.Sprintf("State '%s' is neutral", *rfp.State)
}

// TermLengthConfig for term_length rule type.
type TermLengthConfig struct {
	MinMonths   int `json:"min_months"`
	IdealMonths int `json:"ideal_months"`
}

func evaluateTermLength(config json.RawMessage, rfp *RFPData) (float64, string) {
	var cfg TermLengthConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return 3.0, "Invalid config"
	}

	if rfp.TermMonths == nil || *rfp.TermMonths == 0 {
		return 3.0, "No term length specified"
	}

	months := *rfp.TermMonths

	if months < cfg.MinMonths {
		return 2.0, fmt.Sprintf("Term %d months is below minimum %d", months, cfg.MinMonths)
	}

	if months >= cfg.IdealMonths {
		return 5.0, fmt.Sprintf("Term %d months meets or exceeds ideal %d", months, cfg.IdealMonths)
	}

	// Linear interpolation between min and ideal
	ratio := float64(months-cfg.MinMonths) / float64(cfg.IdealMonths-cfg.MinMonths)
	score := 2.0 + ratio*3.0
	return score, fmt.Sprintf("Term %d months (%.0f%% of ideal)", months, ratio*100)
}

// TimeToDueConfig for time_to_due rule type.
type TimeToDueConfig struct {
	MinDays   int `json:"min_days"`
	IdealDays int `json:"ideal_days"`
}

func evaluateTimeToDue(config json.RawMessage, rfp *RFPData) (float64, string) {
	var cfg TimeToDueConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return 3.0, "Invalid config"
	}

	if rfp.DueDate == nil {
		return 3.0, "No due date specified"
	}

	daysUntilDue := int(time.Until(*rfp.DueDate).Hours() / 24)

	if daysUntilDue < 0 {
		return 1.0, "RFP is past due"
	}

	if daysUntilDue < cfg.MinDays {
		return 1.5, fmt.Sprintf("Only %d days until due (minimum %d)", daysUntilDue, cfg.MinDays)
	}

	if daysUntilDue >= cfg.IdealDays {
		return 5.0, fmt.Sprintf("%d days until due (ideal %d+)", daysUntilDue, cfg.IdealDays)
	}

	// Linear interpolation
	ratio := float64(daysUntilDue-cfg.MinDays) / float64(cfg.IdealDays-cfg.MinDays)
	score := 2.0 + ratio*3.0
	return score, fmt.Sprintf("%d days until due (%.0f%% of ideal window)", daysUntilDue, ratio*100)
}

// ValueConfig for value rule type.
type ValueConfig struct {
	MinValue   float64 `json:"min_value"`
	IdealValue float64 `json:"ideal_value"`
}

func evaluateValue(config json.RawMessage, rfp *RFPData) (float64, string) {
	var cfg ValueConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return 3.0, "Invalid config"
	}

	if rfp.EstimatedValue == nil || *rfp.EstimatedValue == 0 {
		return 3.0, "No estimated value specified"
	}

	value := *rfp.EstimatedValue

	if value < cfg.MinValue {
		return 2.0, fmt.Sprintf("Value $%.0f is below minimum $%.0f", value, cfg.MinValue)
	}

	if value >= cfg.IdealValue {
		return 5.0, fmt.Sprintf("Value $%.0f meets or exceeds ideal $%.0f", value, cfg.IdealValue)
	}

	// Linear interpolation
	ratio := (value - cfg.MinValue) / (cfg.IdealValue - cfg.MinValue)
	score := 2.0 + ratio*3.0
	return score, fmt.Sprintf("Value $%.0f (%.0f%% of ideal)", value, ratio*100)
}

// ApplyScore calculates and stores the auto_score for an RFP.
func (s *Service) ApplyScore(ctx context.Context, rfpID int) (*ScoreResult, error) {
	result, err := s.ScoreRFP(ctx, rfpID)
	if err != nil {
		return nil, err
	}

	// Serialize reasons to JSON
	reasonsJSON, err := json.Marshal(result.Reasons)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal score reasons: %w", err)
	}

	now := time.Now()

	// Try to update existing tracking record
	res, err := s.db.Exec(ctx, `
		UPDATE client.rfp_tracking
		SET auto_score = $1, score_reasons = $2, updated_at = $3
		WHERE discovery_rfp_id = $4
	`, result.Score, reasonsJSON, now, rfpID)
	if err != nil {
		return nil, fmt.Errorf("failed to update auto_score: %w", err)
	}

	// If no rows updated, create a new tracking record
	if res.RowsAffected() == 0 {
		_, err = s.db.Exec(ctx, `
			INSERT INTO client.rfp_tracking
			(discovery_rfp_id, auto_score, score_reasons, stage, created_at, updated_at)
			VALUES ($1, $2, $3, 'new', $4, $4)
		`, rfpID, result.Score, reasonsJSON, now)
		if err != nil {
			return nil, fmt.Errorf("failed to create tracking record with score: %w", err)
		}
	}

	return result, nil
}

// RefreshAllScores recalculates auto_scores for all active RFPs.
func (s *Service) RefreshAllScores(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM discovery.rfps WHERE is_active = true`)
	if err != nil {
		return 0, fmt.Errorf("failed to get active RFPs: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var rfpID int
		if err := rows.Scan(&rfpID); err != nil {
			continue
		}
		if _, err := s.ApplyScore(ctx, rfpID); err != nil {
			// Log but continue
			continue
		}
		count++
	}

	return count, nil
}
