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
	"github.com/zachsouder/rfp/discovery/internal/scheduler"
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
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))

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

	// Create the scheduler
	sched := scheduler.New(
		database,
		searchClient,
		validator,
		researchAgent,
		scheduler.WithRunOnStart(!*runOnce), // Don't auto-run if doing run-once
	)

	// Handle run-once mode
	if *runOnce {
		slog.Info("running single discovery cycle")
		stats, err := sched.RunOnce(context.Background())
		if err != nil {
			slog.Error("discovery cycle failed", "error", err)
			os.Exit(1)
		}
		slog.Info("discovery cycle complete",
			"duration", stats.Duration.String(),
			"queries_executed", stats.QueriesExecuted,
			"results_new", stats.ResultsNew,
		)
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
	go sched.Run(stopScheduler)

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
