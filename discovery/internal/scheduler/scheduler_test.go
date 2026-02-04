package scheduler

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Interval != 24*time.Hour {
		t.Errorf("expected Interval to be 24h, got %v", cfg.Interval)
	}

	if cfg.CycleTimeout != 30*time.Minute {
		t.Errorf("expected CycleTimeout to be 30m, got %v", cfg.CycleTimeout)
	}

	if cfg.MaxConcurrency != 5 {
		t.Errorf("expected MaxConcurrency to be 5, got %d", cfg.MaxConcurrency)
	}

	if cfg.ValidationDelay != 500*time.Millisecond {
		t.Errorf("expected ValidationDelay to be 500ms, got %v", cfg.ValidationDelay)
	}

	if cfg.QueryDelay != 500*time.Millisecond {
		t.Errorf("expected QueryDelay to be 500ms, got %v", cfg.QueryDelay)
	}

	if !cfg.RunOnStart {
		t.Error("expected RunOnStart to be true")
	}

	if !cfg.SkipSeenURLs {
		t.Error("expected SkipSeenURLs to be true")
	}
}

func TestConfigOptions(t *testing.T) {
	cfg := DefaultConfig()

	WithInterval(12 * time.Hour)(cfg)
	if cfg.Interval != 12*time.Hour {
		t.Errorf("expected Interval to be 12h, got %v", cfg.Interval)
	}

	WithCycleTimeout(45 * time.Minute)(cfg)
	if cfg.CycleTimeout != 45*time.Minute {
		t.Errorf("expected CycleTimeout to be 45m, got %v", cfg.CycleTimeout)
	}

	WithMaxConcurrency(10)(cfg)
	if cfg.MaxConcurrency != 10 {
		t.Errorf("expected MaxConcurrency to be 10, got %d", cfg.MaxConcurrency)
	}

	WithValidationDelay(1 * time.Second)(cfg)
	if cfg.ValidationDelay != 1*time.Second {
		t.Errorf("expected ValidationDelay to be 1s, got %v", cfg.ValidationDelay)
	}

	WithQueryDelay(2 * time.Second)(cfg)
	if cfg.QueryDelay != 2*time.Second {
		t.Errorf("expected QueryDelay to be 2s, got %v", cfg.QueryDelay)
	}

	WithRunOnStart(false)(cfg)
	if cfg.RunOnStart {
		t.Error("expected RunOnStart to be false")
	}

	WithSkipSeenURLs(false)(cfg)
	if cfg.SkipSeenURLs {
		t.Error("expected SkipSeenURLs to be false")
	}
}

func TestCycleStats(t *testing.T) {
	stats := &CycleStats{
		StartTime:        time.Now(),
		QueriesExecuted:  5,
		QueriesFailed:    1,
		ResultsFound:     20,
		ResultsSkipped:   5,
		ResultsNew:       15,
		Validated:        12,
		ValidationFailed: 3,
	}

	stats.EndTime = stats.StartTime.Add(10 * time.Minute)
	stats.Duration = stats.EndTime.Sub(stats.StartTime)

	if stats.Duration != 10*time.Minute {
		t.Errorf("expected duration to be 10m, got %v", stats.Duration)
	}

	if stats.QueriesExecuted != 5 {
		t.Errorf("expected QueriesExecuted to be 5, got %d", stats.QueriesExecuted)
	}

	if stats.ResultsNew != 15 {
		t.Errorf("expected ResultsNew to be 15, got %d", stats.ResultsNew)
	}
}
