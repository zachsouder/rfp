// Package scheduler provides a cron-like runner for daily RFP discovery searches.
package scheduler

import "time"

// Config holds scheduler configuration options.
type Config struct {
	// Interval between discovery cycles. Default: 24 hours.
	Interval time.Duration

	// CycleTimeout is the maximum duration for a single discovery cycle.
	// Default: 30 minutes.
	CycleTimeout time.Duration

	// MaxConcurrency limits concurrent operations within a cycle.
	// Default: 5.
	MaxConcurrency int

	// ValidationDelay is the delay between URL validations to avoid rate limiting.
	// Default: 500ms.
	ValidationDelay time.Duration

	// QueryDelay is the delay between search queries to avoid API rate limiting.
	// Default: 500ms.
	QueryDelay time.Duration

	// RunOnStart controls whether to run a cycle immediately on startup.
	// Default: true.
	RunOnStart bool

	// SkipSeenURLs controls whether to skip URLs that have already been processed.
	// Default: true.
	SkipSeenURLs bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Interval:        24 * time.Hour,
		CycleTimeout:    30 * time.Minute,
		MaxConcurrency:  5,
		ValidationDelay: 500 * time.Millisecond,
		QueryDelay:      500 * time.Millisecond,
		RunOnStart:      true,
		SkipSeenURLs:    true,
	}
}

// Option is a function that modifies Config.
type Option func(*Config)

// WithInterval sets the cycle interval.
func WithInterval(d time.Duration) Option {
	return func(c *Config) {
		c.Interval = d
	}
}

// WithCycleTimeout sets the cycle timeout.
func WithCycleTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.CycleTimeout = d
	}
}

// WithMaxConcurrency sets the max concurrent operations.
func WithMaxConcurrency(n int) Option {
	return func(c *Config) {
		c.MaxConcurrency = n
	}
}

// WithValidationDelay sets the delay between URL validations.
func WithValidationDelay(d time.Duration) Option {
	return func(c *Config) {
		c.ValidationDelay = d
	}
}

// WithQueryDelay sets the delay between search queries.
func WithQueryDelay(d time.Duration) Option {
	return func(c *Config) {
		c.QueryDelay = d
	}
}

// WithRunOnStart controls whether to run immediately on start.
func WithRunOnStart(b bool) Option {
	return func(c *Config) {
		c.RunOnStart = b
	}
}

// WithSkipSeenURLs controls whether to skip already-processed URLs.
func WithSkipSeenURLs(b bool) Option {
	return func(c *Config) {
		c.SkipSeenURLs = b
	}
}
