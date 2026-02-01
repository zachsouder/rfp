package dedup

import (
	"testing"
	"time"

	"github.com/zachsouder/rfp/shared/models"
)

func TestNormalizeAgency(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"City of Springfield", "springfield"},
		{"CITY OF SPRINGFIELD", "springfield"},
		{"Springfield", "springfield"},
		{"Town of Greenville", "greenville"},
		{"County of Los Angeles", "los angeles"},
		{"Tampa, City of", "tampa"},
		{"Los Angeles, County of", "los angeles"},
		{"City of New York", "new york"},
		{"  City of   Springfield  ", "springfield"},
		{"City of St. Louis", "st louis"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeAgency(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeAgency(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeState(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"CA", "CA"},
		{"ca", "CA"},
		{"California", "CA"},
		{"CALIFORNIA", "CA"},
		{"New York", "NY"},
		{"new york", "NY"},
		{"TX", "TX"},
		{"", ""},
		{"Invalid", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeState(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeState(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeDate(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2024-01-15", "2024-01-15"},
		{"01/15/2024", "2024-01-15"},
		{"1/15/2024", "2024-01-15"},
		{"January 15, 2024", "2024-01-15"},
		{"Jan 15, 2024", "2024-01-15"},
		{"2024/01/15", "2024-01-15"},
		{"", ""},
		{"invalid date", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeDate(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeDate(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAgencyMatches(t *testing.T) {
	tests := []struct {
		a        string
		b        string
		expected bool
	}{
		{"springfield", "springfield", true},
		{"springfield", "spring", true},  // contains
		{"spring", "springfield", true},  // contained
		{"springfield", "springfild", true}, // typo (Levenshtein)
		{"springfield", "chicago", false},
		{"", "springfield", false},
		{"springfield", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			result := AgencyMatches(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("AgencyMatches(%q, %q) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestDatesMatch(t *testing.T) {
	tests := []struct {
		date1    string
		date2    string
		expected bool
	}{
		{"2024-01-15", "2024-01-15", true},
		{"2024-01-15", "2024-01-14", true},  // Within 3 days
		{"2024-01-15", "2024-01-18", true},  // Within 3 days
		{"2024-01-15", "2024-01-20", false}, // More than 3 days
		{"2024-01-15", "invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.date1+"_"+tt.date2, func(t *testing.T) {
			result := DatesMatch(tt.date1, tt.date2)
			if result != tt.expected {
				t.Errorf("DatesMatch(%q, %q) = %v, want %v", tt.date1, tt.date2, result, tt.expected)
			}
		})
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a        string
		b        string
		expected int
	}{
		{"", "", 0},
		{"hello", "", 5},
		{"", "hello", 5},
		{"hello", "hello", 0},
		{"hello", "hallo", 1},
		{"hello", "helo", 1},
		{"sitting", "kitten", 3},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			result := levenshtein(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMatcher_CheckDuplicate(t *testing.T) {
	dueDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	existingRFPs := []models.RFP{
		{
			ID:      1,
			Title:   "Parking Management Services",
			Agency:  "City of Springfield",
			State:   "IL",
			DueDate: &dueDate,
		},
		{
			ID:     2,
			Title:  "Street Parking Operations",
			Agency: "City of Chicago",
			State:  "IL",
		},
	}

	matcher := NewMatcher(existingRFPs)

	tests := []struct {
		name       string
		agency     string
		state      string
		dueDate    string
		expectMatch bool
		matchID    int
	}{
		{
			name:        "exact match",
			agency:      "City of Springfield",
			state:       "IL",
			dueDate:     "2024-01-15",
			expectMatch: true,
			matchID:     1,
		},
		{
			name:        "normalized agency match",
			agency:      "Springfield",
			state:       "Illinois",
			dueDate:     "January 15, 2024",
			expectMatch: true,
			matchID:     1,
		},
		{
			name:        "date within tolerance",
			agency:      "City of Springfield",
			state:       "IL",
			dueDate:     "2024-01-14", // 1 day off
			expectMatch: true,
			matchID:     1,
		},
		{
			name:        "no match - different agency",
			agency:      "City of Boston",
			state:       "MA",
			dueDate:     "2024-01-15",
			expectMatch: false,
		},
		{
			name:        "no match - no agency",
			agency:      "",
			state:       "IL",
			dueDate:     "2024-01-15",
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.CheckDuplicate(tt.agency, tt.state, tt.dueDate)
			if result.FoundMatch != tt.expectMatch {
				t.Errorf("CheckDuplicate() FoundMatch = %v, want %v (reason: %s)", result.FoundMatch, tt.expectMatch, result.Reason)
			}
			if tt.expectMatch && result.MatchedRFPID != tt.matchID {
				t.Errorf("CheckDuplicate() MatchedRFPID = %d, want %d", result.MatchedRFPID, tt.matchID)
			}
		})
	}
}

func TestMatcher_EmptyRFPs(t *testing.T) {
	matcher := NewMatcher([]models.RFP{})

	result := matcher.CheckDuplicate("City of Springfield", "IL", "2024-01-15")

	if result.FoundMatch {
		t.Error("Expected no match with empty RFP list")
	}
	if result.CandidatesChecked != 0 {
		t.Errorf("Expected 0 candidates, got %d", result.CandidatesChecked)
	}
}
