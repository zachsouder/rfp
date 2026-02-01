// Package main is the entry point for the Client application.
package main

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/zachsouder/rfp/client/internal/auth"
	"github.com/zachsouder/rfp/client/internal/handlers"
	"github.com/zachsouder/rfp/client/internal/middleware"
	"github.com/zachsouder/rfp/client/internal/templates"
	"github.com/zachsouder/rfp/client/web"
	"github.com/zachsouder/rfp/shared/config"
	"github.com/zachsouder/rfp/shared/db"
)

func main() {
	// Set up structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load configuration
	cfg := config.Load()

	// Connect to database
	ctx := context.Background()
	database, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	slog.Info("connected to database")

	// Initialize template engine
	tmpl, err := templates.New(web.TemplatesFS)
	if err != nil {
		slog.Error("failed to initialize templates", "error", err)
		os.Exit(1)
	}

	// Initialize services
	authService := auth.NewService(database.Pool)

	// Initialize handlers
	h := handlers.New(database, authService, tmpl)

	// Create router
	r := chi.NewRouter()

	// Middleware
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60 * time.Second))

	// Serve static files from embedded FS
	staticFS, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		slog.Error("failed to create static file sub-filesystem", "error", err)
		os.Exit(1)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Health check endpoint
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Public routes (authentication)
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.Login)
	r.Get("/logout", h.Logout)

	// Protected routes (require authentication)
	r.Group(func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(authService))

		// Dashboard
		r.Get("/", h.Dashboard)

		// RFPs
		r.Get("/rfps", h.RFPList)
		r.Get("/rfps/{id}", h.RFPDetail)
		r.Post("/rfps/{id}/stage", h.UpdateStage)
		r.Post("/rfps/{id}/score", h.UpdateScore)
		r.Post("/rfps/{id}/assign", notImplemented("assign RFP"))
		r.Post("/rfps/{id}/notes", h.AddNote)
		r.Post("/rfps/{id}/attachments", notImplemented("upload attachment"))

		// Pipeline view
		r.Get("/pipeline", notImplemented("pipeline view"))

		// Settings
		r.Get("/settings", notImplemented("user settings"))
		r.Post("/settings", notImplemented("update settings"))
		r.Post("/settings/password", notImplemented("change password"))

		// Admin routes
		r.Route("/admin", func(r chi.Router) {
			r.Use(middleware.AdminMiddleware)

			r.Get("/users", notImplemented("user management"))
			r.Post("/users/invite", notImplemented("invite user"))
			r.Post("/users/{id}/deactivate", notImplemented("deactivate user"))

			r.Get("/scoring", notImplemented("scoring rules"))
			r.Post("/scoring", notImplemented("update scoring rules"))
		})
	})

	// Determine server address
	addr := os.Getenv("CLIENT_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// Create HTTP server
	server := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		slog.Info("starting server", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}

// notImplemented returns a handler that responds with 501 Not Implemented
func notImplemented(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.Info("route not implemented", "route", name)
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte("Not Implemented: " + name))
	}
}
