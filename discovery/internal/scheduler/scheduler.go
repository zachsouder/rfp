package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zachsouder/rfp/discovery/internal/research"
	"github.com/zachsouder/rfp/discovery/internal/search"
	"github.com/zachsouder/rfp/discovery/internal/validation"
	"github.com/zachsouder/rfp/shared/db"
	"github.com/zachsouder/rfp/shared/models"
)

// Scheduler manages the discovery cycle execution.
type Scheduler struct {
	config *Config
	store  *Store
	search *search.Client
	validator *validation.Validator
	research  *research.Agent

	mu      sync.Mutex
	running bool
}

// CycleStats holds statistics for a discovery cycle.
type CycleStats struct {
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	QueriesExecuted int
	QueriesFailed   int
	ResultsFound    int
	ResultsSkipped  int // Already seen URLs
	ResultsNew      int
	Validated       int
	ValidationFailed int
}

// New creates a new Scheduler.
func New(database *db.DB, searchClient *search.Client, validator *validation.Validator, researchAgent *research.Agent, opts ...Option) *Scheduler {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return &Scheduler{
		config:    cfg,
		store:     NewStore(database),
		search:    searchClient,
		validator: validator,
		research:  researchAgent,
	}
}

// Run starts the scheduler and blocks until the stop channel is closed.
func (s *Scheduler) Run(stop <-chan struct{}) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		slog.Warn("scheduler already running")
		return
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	slog.Info("scheduler started",
		"interval", s.config.Interval.String(),
		"run_on_start", s.config.RunOnStart,
		"max_concurrency", s.config.MaxConcurrency,
	)

	// Run immediately on start if configured
	if s.config.RunOnStart {
		s.runCycle()
	}

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			slog.Info("scheduler stopping")
			return
		case <-ticker.C:
			s.runCycle()
		}
	}
}

// RunOnce executes a single discovery cycle (for testing or manual runs).
func (s *Scheduler) RunOnce(ctx context.Context) (*CycleStats, error) {
	return s.executeCycle(ctx)
}

// runCycle executes a discovery cycle with timeout.
func (s *Scheduler) runCycle() {
	ctx, cancel := context.WithTimeout(context.Background(), s.config.CycleTimeout)
	defer cancel()

	slog.Info("starting scheduled discovery cycle")
	stats, err := s.executeCycle(ctx)
	if err != nil {
		slog.Error("discovery cycle failed", "error", err)
		return
	}

	slog.Info("discovery cycle complete",
		"duration", stats.Duration.String(),
		"queries_executed", stats.QueriesExecuted,
		"queries_failed", stats.QueriesFailed,
		"results_found", stats.ResultsFound,
		"results_new", stats.ResultsNew,
		"results_skipped", stats.ResultsSkipped,
		"validated", stats.Validated,
		"validation_failed", stats.ValidationFailed,
	)
}

// executeCycle runs the full discovery pipeline.
func (s *Scheduler) executeCycle(ctx context.Context) (*CycleStats, error) {
	stats := &CycleStats{
		StartTime: time.Now(),
	}

	// Load query configs
	configs, err := s.store.LoadQueryConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load query configs: %w", err)
	}
	slog.Info("loaded query configs", "count", len(configs))

	// Execute search phase
	allResults, err := s.executeSearchPhase(ctx, configs, stats)
	if err != nil {
		return nil, fmt.Errorf("search phase failed: %w", err)
	}

	// Validate phase
	if err := s.executeValidationPhase(ctx, allResults, stats); err != nil {
		return nil, fmt.Errorf("validation phase failed: %w", err)
	}

	stats.EndTime = time.Now()
	stats.Duration = stats.EndTime.Sub(stats.StartTime)

	return stats, nil
}

// executeSearchPhase runs all search queries and persists results.
func (s *Scheduler) executeSearchPhase(ctx context.Context, configs []models.SearchQueryConfig, stats *CycleStats) ([]SearchResultWithID, error) {
	var allNewResults []SearchResultWithID

	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}

		select {
		case <-ctx.Done():
			return allNewResults, ctx.Err()
		default:
		}

		slog.Debug("executing search query", "name", cfg.Name, "query", cfg.QueryTemplate)

		// Execute the search
		resp, err := s.search.Search(ctx, cfg.QueryTemplate)
		if err != nil {
			slog.Warn("search query failed", "name", cfg.Name, "error", err)
			stats.QueriesFailed++
			continue
		}

		stats.QueriesExecuted++
		stats.ResultsFound += resp.ResultsCount

		if resp.ResultsCount == 0 {
			// Save query with zero results
			configID := cfg.ID
			if configID == 0 {
				_, err = s.store.SaveSearchQuery(ctx, cfg.QueryTemplate, nil, 0, "completed")
			} else {
				_, err = s.store.SaveSearchQuery(ctx, cfg.QueryTemplate, &configID, 0, "completed")
			}
			if err != nil {
				slog.Warn("failed to save empty query", "error", err)
			}
			continue
		}

		// Filter out already-seen URLs
		var newResults []search.Result
		if s.config.SkipSeenURLs {
			urls := make([]string, len(resp.Results))
			for i, r := range resp.Results {
				urls[i] = r.URL
			}

			existingURLs, err := s.store.URLExistsBatch(ctx, urls)
			if err != nil {
				slog.Warn("failed to check existing URLs", "error", err)
				// Continue with all results if check fails
				newResults = resp.Results
			} else {
				for _, r := range resp.Results {
					if !existingURLs[r.URL] {
						newResults = append(newResults, r)
					} else {
						stats.ResultsSkipped++
					}
				}
			}
		} else {
			newResults = resp.Results
		}

		stats.ResultsNew += len(newResults)

		if len(newResults) == 0 {
			slog.Debug("all results already seen", "name", cfg.Name)
			continue
		}

		// Save query and results
		var configIDPtr *int
		if cfg.ID != 0 {
			configIDPtr = &cfg.ID
		}
		_, savedResults, err := s.store.SaveSearchQueryAndResults(ctx, cfg.QueryTemplate, configIDPtr, newResults, "completed")
		if err != nil {
			slog.Warn("failed to save query results", "name", cfg.Name, "error", err)
			continue
		}

		allNewResults = append(allNewResults, savedResults...)

		slog.Debug("search completed",
			"name", cfg.Name,
			"found", resp.ResultsCount,
			"new", len(newResults),
		)

		// Rate limiting between queries
		select {
		case <-ctx.Done():
			return allNewResults, ctx.Err()
		case <-time.After(s.config.QueryDelay):
		}
	}

	slog.Info("search phase complete",
		"queries_executed", stats.QueriesExecuted,
		"queries_failed", stats.QueriesFailed,
		"results_found", stats.ResultsFound,
		"results_new", stats.ResultsNew,
		"results_skipped", stats.ResultsSkipped,
	)

	return allNewResults, nil
}

// executeValidationPhase validates URLs with concurrency control.
func (s *Scheduler) executeValidationPhase(ctx context.Context, results []SearchResultWithID, stats *CycleStats) error {
	if len(results) == 0 {
		slog.Debug("no results to validate")
		return nil
	}

	slog.Info("starting validation phase", "count", len(results))

	// Use a semaphore for concurrency control
	sem := make(chan struct{}, s.config.MaxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, sr := range results {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(sr SearchResultWithID) {
			defer wg.Done()
			defer func() { <-sem }()

			vr := s.validator.Validate(ctx, sr.URL)

			// Update the database
			if err := s.store.UpdateValidationResult(ctx, sr.ID, vr); err != nil {
				slog.Warn("failed to update validation", "url", sr.URL, "error", err)
			}

			mu.Lock()
			if vr.Valid {
				stats.Validated++
			} else {
				stats.ValidationFailed++
			}
			mu.Unlock()

			slog.Debug("validated url",
				"url", sr.URL,
				"valid", vr.Valid,
				"status", vr.Status,
				"content_type", vr.ContentType,
			)

			// Rate limiting
			time.Sleep(s.config.ValidationDelay)
		}(sr)
	}

	wg.Wait()

	slog.Info("validation phase complete",
		"validated", stats.Validated,
		"failed", stats.ValidationFailed,
	)

	return nil
}
