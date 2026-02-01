// Package main is the entry point for the Discovery service.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zachsouder/rfp/discovery/internal/research"
	"github.com/zachsouder/rfp/discovery/internal/search"
	"github.com/zachsouder/rfp/discovery/internal/validation"
	"github.com/zachsouder/rfp/shared/config"
	"github.com/zachsouder/rfp/shared/db"
)

func main() {
	// Parse command line flags
	runOnce := flag.Bool("run-once", false, "Run discovery once and exit")
	httpPort := flag.Int("port", 8081, "HTTP port for health checks")
	flag.Parse()

	// Set up structured logging
	logLevel := slog.LevelInfo
	cfg := config.Load()
	if cfg.LogLevel == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Log startup
	slog.Info("starting discovery service",
		"run_once", *runOnce,
		"port", *httpPort,
	)

	// Validate required config
	if cfg.GeminiAPIKey == "" {
		slog.Error("GEMINI_API_KEY is required")
		os.Exit(1)
	}

	// Connect to database
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	database, err := db.Connect(ctx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	slog.Info("connected to database")

	// Initialize services
	searchClient := search.NewClient(cfg.GeminiAPIKey)
	validator := validation.NewValidator()
	researchAgent := research.NewAgent(cfg.GeminiAPIKey)

	// Create the discovery service
	svc := &DiscoveryService{
		db:            database,
		searchClient:  searchClient,
		validator:     validator,
		researchAgent: researchAgent,
		logger:        logger,
	}

	// Handle run-once mode
	if *runOnce {
		slog.Info("running single discovery cycle")
		if err := svc.RunDiscoveryCycle(context.Background()); err != nil {
			slog.Error("discovery cycle failed", "error", err)
			os.Exit(1)
		}
		slog.Info("discovery cycle complete")
		return
	}

	// Set up HTTP server for health checks
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		// Check database connection
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := database.Pool.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "database unavailable: %v", err)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", *httpPort),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start HTTP server in background
	go func() {
		slog.Info("starting health check server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("health server error", "error", err)
		}
	}()

	// Start scheduler
	stopScheduler := make(chan struct{})
	go svc.RunScheduler(stopScheduler)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	slog.Info("received shutdown signal", "signal", sig)

	// Graceful shutdown
	close(stopScheduler)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("error during HTTP server shutdown", "error", err)
	}

	slog.Info("discovery service stopped")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("healthy"))
}

// DiscoveryService coordinates the discovery pipeline.
type DiscoveryService struct {
	db            *db.DB
	searchClient  *search.Client
	validator     *validation.Validator
	researchAgent *research.Agent
	logger        *slog.Logger
}

// RunScheduler runs the discovery cycle on a schedule.
func (s *DiscoveryService) RunScheduler(stop <-chan struct{}) {
	// Run immediately on start
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	if err := s.RunDiscoveryCycle(ctx); err != nil {
		slog.Error("initial discovery cycle failed", "error", err)
	}
	cancel()

	// Then run daily
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			slog.Info("scheduler stopping")
			return
		case <-ticker.C:
			slog.Info("starting scheduled discovery cycle")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			if err := s.RunDiscoveryCycle(ctx); err != nil {
				slog.Error("scheduled discovery cycle failed", "error", err)
			}
			cancel()
		}
	}
}

// RunDiscoveryCycle executes one full discovery cycle:
// 1. Run configured search queries
// 2. Validate new URLs
// 3. Research validated results
// 4. Check for duplicates
// 5. Promote new RFPs
func (s *DiscoveryService) RunDiscoveryCycle(ctx context.Context) error {
	slog.Info("starting discovery cycle")
	startTime := time.Now()

	// Get configured queries (or use defaults)
	queryConfigs := search.DefaultQueryConfigs()
	slog.Info("executing search queries", "count", len(queryConfigs))

	// Execute searches
	responses, err := s.searchClient.ExecuteQueries(ctx, queryConfigs)
	if err != nil {
		return fmt.Errorf("search execution failed: %w", err)
	}

	totalResults := 0
	for _, resp := range responses {
		totalResults += resp.ResultsCount
		slog.Debug("search completed",
			"query", resp.Query,
			"results", resp.ResultsCount,
			"duration_ms", resp.DurationMs,
		)
	}
	slog.Info("search phase complete",
		"queries", len(responses),
		"total_results", totalResults,
	)

	// Validate URLs
	var validatedCount, invalidCount int
	for _, resp := range responses {
		for _, result := range resp.Results {
			validResult := s.validator.Validate(ctx, result.URL)
			if validResult.Valid {
				validatedCount++
			} else {
				invalidCount++
			}

			// Rate limit
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	slog.Info("validation phase complete",
		"validated", validatedCount,
		"invalid", invalidCount,
	)

	// Log completion
	duration := time.Since(startTime)
	slog.Info("discovery cycle complete",
		"duration", duration.String(),
		"total_results", totalResults,
		"validated", validatedCount,
	)

	return nil
}
