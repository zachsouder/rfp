package handlers

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/zachsouder/rfp/client/internal/auth"
	"github.com/zachsouder/rfp/client/internal/middleware"
	"github.com/zachsouder/rfp/client/internal/templates"
	"github.com/zachsouder/rfp/shared/config"
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
	if msg := r.URL.Query().Get("success"); msg != "" {
		data.Flash = &templates.Flash{
			Type:    "success",
			Message: msg,
		}
	}
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

// ForgotPasswordPage renders the forgot password page.
func (h *Handlers) ForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	data := templates.PageData{
		Title: "Forgot Password",
	}

	if msg := r.URL.Query().Get("success"); msg != "" {
		data.Flash = &templates.Flash{
			Type:    "success",
			Message: msg,
		}
	}
	if msg := r.URL.Query().Get("error"); msg != "" {
		data.Flash = &templates.Flash{
			Type:    "error",
			Message: msg,
		}
	}

	if err := h.templates.Render(w, "forgot_password", data); err != nil {
		slog.Error("failed to render forgot password template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// ForgotPassword handles the forgot password form submission.
func (h *Handlers) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/forgot-password?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	email := r.FormValue("email")
	if email == "" {
		http.Redirect(w, r, "/forgot-password?error=Email+is+required", http.StatusSeeOther)
		return
	}

	// Create reset token (returns empty string if email not found, but we don't reveal that)
	token, err := h.authService.CreatePasswordResetToken(r.Context(), email)
	if err != nil {
		slog.Error("failed to create password reset token", "error", err)
		// Still show success to not reveal if email exists
	}

	// If we have a token, send the email
	if token != "" {
		cfg := config.Load()
		resetURL := fmt.Sprintf("%s/reset-password?token=%s", cfg.BaseURL, token)

		smtpCfg := auth.SMTPConfig{
			Host: cfg.SMTPHost,
			Port: cfg.SMTPPort,
			User: cfg.SMTPUser,
			Pass: cfg.SMTPPass,
			From: cfg.SMTPFrom,
		}

		if err := auth.SendPasswordResetEmail(smtpCfg, email, resetURL); err != nil {
			slog.Error("failed to send password reset email", "error", err, "email", email)
			// Still show success to not reveal if email delivery failed
		} else {
			slog.Info("password reset email sent", "email", email)
		}
	}

	// Always show success message (don't reveal if email exists or not)
	http.Redirect(w, r, "/forgot-password?success=If+an+account+with+that+email+exists,+we+sent+a+password+reset+link.", http.StatusSeeOther)
}

// ResetPasswordPage renders the password reset page.
func (h *Handlers) ResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Redirect(w, r, "/forgot-password?error=Invalid+reset+link", http.StatusSeeOther)
		return
	}

	// Validate the token
	_, err := h.authService.ValidateResetToken(r.Context(), token)
	if err != nil {
		slog.Info("invalid reset token", "error", err)
		http.Redirect(w, r, "/forgot-password?error=Invalid+or+expired+reset+link", http.StatusSeeOther)
		return
	}

	data := templates.PageData{
		Title: "Reset Password",
		Data:  map[string]string{"Token": token},
	}

	if msg := r.URL.Query().Get("error"); msg != "" {
		data.Flash = &templates.Flash{
			Type:    "error",
			Message: msg,
		}
	}

	if err := h.templates.Render(w, "reset_password", data); err != nil {
		slog.Error("failed to render reset password template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// ResetPassword handles the password reset form submission.
func (h *Handlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/forgot-password?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	token := r.FormValue("token")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if token == "" {
		http.Redirect(w, r, "/forgot-password?error=Invalid+reset+link", http.StatusSeeOther)
		return
	}

	if password == "" {
		http.Redirect(w, r, "/reset-password?token="+token+"&error=Password+is+required", http.StatusSeeOther)
		return
	}

	if len(password) < 8 {
		http.Redirect(w, r, "/reset-password?token="+token+"&error=Password+must+be+at+least+8+characters", http.StatusSeeOther)
		return
	}

	if password != confirmPassword {
		http.Redirect(w, r, "/reset-password?token="+token+"&error=Passwords+do+not+match", http.StatusSeeOther)
		return
	}

	if err := h.authService.ResetPassword(r.Context(), token, password); err != nil {
		slog.Error("failed to reset password", "error", err)
		http.Redirect(w, r, "/forgot-password?error=Invalid+or+expired+reset+link", http.StatusSeeOther)
		return
	}

	slog.Info("password reset successful")
	http.Redirect(w, r, "/login?success=Password+reset+successfully.+Please+log+in.", http.StatusSeeOther)
}
