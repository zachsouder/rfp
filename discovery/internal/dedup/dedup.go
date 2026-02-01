// Package dedup provides fuzzy deduplication for RFP search results.
package dedup

import (
	"regexp"
	"strings"
	"time"

	"github.com/zachsouder/rfp/shared/models"
)

const (
	// MatchThreshold is the minimum score required to consider a match.
	MatchThreshold = 0.7 // 70%

	// AgencySimilarityThreshold for fuzzy matching.
	AgencySimilarityThreshold = 0.8 // 80%

	// DateToleranceDays for date matching.
	DateToleranceDays = 3
)

// Weights for match score calculation.
var weights = struct {
	Agency float64
	State  float64
	Date   float64
}{
	Agency: 0.5,
	State:  0.2,
	Date:   0.3,
}

// MatchResult contains the result of a deduplication check.
type MatchResult struct {
	FoundMatch        bool    `json:"found_match"`
	MatchedRFPID      int     `json:"rfp_id,omitempty"`
	MatchedRFPTitle   string  `json:"rfp_title,omitempty"`
	MatchScore        float64 `json:"match_score,omitempty"`
	Reason            string  `json:"reason,omitempty"`
	CandidatesChecked int     `json:"candidates_checked,omitempty"`
}

// Matcher handles deduplication matching.
type Matcher struct {
	existingRFPs []models.RFP
}

// NewMatcher creates a new deduplication matcher.
func NewMatcher(existingRFPs []models.RFP) *Matcher {
	return &Matcher{
		existingRFPs: existingRFPs,
	}
}

// CheckDuplicate checks if the given hints match an existing RFP.
func (m *Matcher) CheckDuplicate(agency, state, dueDate string) *MatchResult {
	// Need at least agency to do matching
	if agency == "" {
		return &MatchResult{
			FoundMatch: false,
			Reason:     "No agency hint available",
		}
	}

	// Normalize the hints
	normalizedAgency := NormalizeAgency(agency)
	normalizedState := NormalizeState(state)
	normalizedDate := NormalizeDate(dueDate)

	// Find candidates
	candidates := m.findCandidates(normalizedAgency, normalizedState)

	// Check each candidate for match
	var bestMatch *models.RFP
	bestScore := 0.0

	for i := range candidates {
		score := m.calculateMatchScore(&candidates[i], normalizedAgency, normalizedState, normalizedDate)
		if score > bestScore && score >= MatchThreshold {
			bestScore = score
			bestMatch = &candidates[i]
		}
	}

	if bestMatch != nil {
		return &MatchResult{
			FoundMatch:        true,
			MatchedRFPID:      bestMatch.ID,
			MatchedRFPTitle:   bestMatch.Title,
			MatchScore:        bestScore,
			CandidatesChecked: len(candidates),
		}
	}

	return &MatchResult{
		FoundMatch:        false,
		Reason:            "No matching RFP found",
		CandidatesChecked: len(candidates),
	}
}

// findCandidates finds RFPs that might match.
func (m *Matcher) findCandidates(normalizedAgency string, state string) []models.RFP {
	var candidates []models.RFP

	for _, rfp := range m.existingRFPs {
		// Filter by state if available
		if state != "" && rfp.State != "" && rfp.State != state {
			continue
		}

		// Check agency similarity
		rfpAgency := NormalizeAgency(rfp.Agency)
		if AgencyMatches(normalizedAgency, rfpAgency) {
			candidates = append(candidates, rfp)
		}
	}

	return candidates
}

// calculateMatchScore calculates the match score between an RFP and hints.
func (m *Matcher) calculateMatchScore(rfp *models.RFP, agency, state, date string) float64 {
	score := 0.0

	// Agency match (required)
	rfpAgency := NormalizeAgency(rfp.Agency)
	if rfpAgency == agency {
		score += weights.Agency
	} else if AgencyMatches(rfpAgency, agency) {
		score += weights.Agency * 0.8 // Partial match
	} else {
		return 0 // Agency must match
	}

	// State match
	if state != "" {
		rfpState := NormalizeState(rfp.State)
		if rfpState == state {
			score += weights.State
		}
	} else {
		// No state hint - give partial credit
		score += weights.State * 0.5
	}

	// Date match
	if date != "" && rfp.DueDate != nil {
		rfpDate := rfp.DueDate.Format("2006-01-02")
		if DatesMatch(date, rfpDate) {
			score += weights.Date
		}
	} else if date == "" {
		// No date hint - give partial credit
		score += weights.Date * 0.5
	}

	return score
}

// Common agency name prefixes to remove.
var agencyPrefixes = []string{"city of", "town of", "county of", "state of", "village of"}

// NormalizeAgency normalizes an agency name for comparison.
func NormalizeAgency(agency string) string {
	normalized := strings.ToLower(strings.TrimSpace(agency))

	// Remove common prefixes
	for _, prefix := range agencyPrefixes {
		if strings.HasPrefix(normalized, prefix+" ") {
			normalized = strings.TrimSpace(normalized[len(prefix)+1:])
			break
		}
	}

	// Handle "Tampa, City of" format
	commaPattern := regexp.MustCompile(`^([^,]+),\s*(city|town|county|village)\s+of$`)
	if matches := commaPattern.FindStringSubmatch(normalized); len(matches) > 1 {
		normalized = strings.TrimSpace(matches[1])
	}

	// Remove special characters
	specialChars := regexp.MustCompile(`[^a-z0-9\s]`)
	normalized = specialChars.ReplaceAllString(normalized, "")

	// Collapse whitespace
	whitespace := regexp.MustCompile(`\s+`)
	normalized = whitespace.ReplaceAllString(normalized, " ")

	return strings.TrimSpace(normalized)
}

// stateNames maps full state names to 2-letter codes.
var stateNames = map[string]string{
	"ALABAMA": "AL", "ALASKA": "AK", "ARIZONA": "AZ", "ARKANSAS": "AR",
	"CALIFORNIA": "CA", "COLORADO": "CO", "CONNECTICUT": "CT", "DELAWARE": "DE",
	"FLORIDA": "FL", "GEORGIA": "GA", "HAWAII": "HI", "IDAHO": "ID",
	"ILLINOIS": "IL", "INDIANA": "IN", "IOWA": "IA", "KANSAS": "KS",
	"KENTUCKY": "KY", "LOUISIANA": "LA", "MAINE": "ME", "MARYLAND": "MD",
	"MASSACHUSETTS": "MA", "MICHIGAN": "MI", "MINNESOTA": "MN", "MISSISSIPPI": "MS",
	"MISSOURI": "MO", "MONTANA": "MT", "NEBRASKA": "NE", "NEVADA": "NV",
	"NEW HAMPSHIRE": "NH", "NEW JERSEY": "NJ", "NEW MEXICO": "NM", "NEW YORK": "NY",
	"NORTH CAROLINA": "NC", "NORTH DAKOTA": "ND", "OHIO": "OH", "OKLAHOMA": "OK",
	"OREGON": "OR", "PENNSYLVANIA": "PA", "RHODE ISLAND": "RI", "SOUTH CAROLINA": "SC",
	"SOUTH DAKOTA": "SD", "TENNESSEE": "TN", "TEXAS": "TX", "UTAH": "UT",
	"VERMONT": "VT", "VIRGINIA": "VA", "WASHINGTON": "WA", "WEST VIRGINIA": "WV",
	"WISCONSIN": "WI", "WYOMING": "WY", "DISTRICT OF COLUMBIA": "DC",
}

// NormalizeState normalizes a state to 2-letter code.
func NormalizeState(state string) string {
	if state == "" {
		return ""
	}

	state = strings.ToUpper(strings.TrimSpace(state))

	// If already 2 letters, return as is
	if len(state) == 2 && regexp.MustCompile(`^[A-Z]{2}$`).MatchString(state) {
		return state
	}

	// Look up full name
	if code, ok := stateNames[state]; ok {
		return code
	}

	return ""
}

// Common date formats to try.
var dateFormats = []string{
	"2006-01-02",
	"01/02/2006",
	"1/2/2006",
	"January 2, 2006",
	"Jan 2, 2006",
	"2 January 2006",
	"2006/01/02",
}

// NormalizeDate normalizes a date to YYYY-MM-DD format.
func NormalizeDate(date string) string {
	if date == "" {
		return ""
	}

	date = strings.TrimSpace(date)

	for _, format := range dateFormats {
		if t, err := time.Parse(format, date); err == nil {
			return t.Format("2006-01-02")
		}
	}

	return ""
}

// AgencyMatches checks if two agency names match (fuzzy).
func AgencyMatches(a, b string) bool {
	if a == "" || b == "" {
		return false
	}

	// Exact match
	if a == b {
		return true
	}

	// One contains the other
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}

	// Levenshtein distance (for typos)
	distance := levenshtein(a, b)
	maxLen := max(len(a), len(b))
	similarity := 1.0 - float64(distance)/float64(maxLen)

	return similarity >= AgencySimilarityThreshold
}

// DatesMatch checks if two dates match within tolerance.
func DatesMatch(date1, date2 string) bool {
	// Exact match
	if date1 == date2 {
		return true
	}

	// Parse both dates
	t1, err1 := time.Parse("2006-01-02", date1)
	t2, err2 := time.Parse("2006-01-02", date2)

	if err1 != nil || err2 != nil {
		return false
	}

	// Check if within tolerance
	diff := t1.Sub(t2)
	if diff < 0 {
		diff = -diff
	}

	return diff <= time.Duration(DateToleranceDays)*24*time.Hour
}

// levenshtein calculates the Levenshtein distance between two strings.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Create matrix
	matrix := make([][]int, len(a)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(b)+1)
	}

	// Initialize first column
	for i := 0; i <= len(a); i++ {
		matrix[i][0] = i
	}

	// Initialize first row
	for j := 0; j <= len(b); j++ {
		matrix[0][j] = j
	}

	// Fill in the rest
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(a)][len(b)]
}

func min(values ...int) int {
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
