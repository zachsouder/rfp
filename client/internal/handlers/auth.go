package handlers

import (
	"log/slog"
	"net/http"

	"github.com/zsouder/rfp/client/internal/middleware"
	"github.com/zsouder/rfp/client/internal/templates"
)

// LoginPage renders the login page.
func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect to dashboard
	cookie, err := r.Cookie(middleware.SessionCookieName)
	if err == nil && cookie.Value != "" {
		if _, err := h.authService.ValidateSession(r.Context(), cookie.Value); err == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	data := templates.PageData{
		Title: "Login",
	}

	// Check for flash message in query params
	if msg := r.URL.Query().Get("error"); msg != "" {
		data.Flash = &templates.Flash{
			Type:    "error",
			Message: msg,
		}
	}

	if err := h.templates.Render(w, "login", data); err != nil {
		slog.Error("failed to render login template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// Login handles the login form submission.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		http.Redirect(w, r, "/login?error=Email+and+password+are+required", http.StatusSeeOther)
		return
	}

	user, err := h.authService.Authenticate(r.Context(), email, password)
	if err != nil {
		slog.Info("failed login attempt", "email", email)
		http.Redirect(w, r, "/login?error=Invalid+email+or+password", http.StatusSeeOther)
		return
	}

	session, err := h.authService.CreateSession(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to create session", "error", err, "user_id", user.ID)
		http.Redirect(w, r, "/login?error=Failed+to+create+session", http.StatusSeeOther)
		return
	}

	middleware.SetSessionCookie(w, r, session.ID)
	slog.Info("user logged in", "user_id", user.ID, "email", user.Email)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout handles user logout.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(middleware.SessionCookieName)
	if err == nil && cookie.Value != "" {
		if err := h.authService.DeleteSession(r.Context(), cookie.Value); err != nil {
			slog.Error("failed to delete session", "error", err)
		}
	}

	middleware.ClearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
