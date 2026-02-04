package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/smtp"
	"time"
)

const (
	// ResetTokenDuration is how long password reset tokens are valid.
	ResetTokenDuration = 1 * time.Hour
	// ResetTokenLength is the byte length of reset tokens (32 bytes = 64 hex chars).
	ResetTokenLength = 32
)

// ResetToken represents a password reset token.
type ResetToken struct {
	ID        int
	UserID    int
	Token     string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// SMTPConfig holds SMTP configuration for sending emails.
type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

// GenerateResetToken creates a new secure random reset token.
func GenerateResetToken() (string, error) {
	bytes := make([]byte, ResetTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate reset token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// CreatePasswordResetToken creates a password reset token for the user.
// Returns the token string to be included in the reset email.
func (s *Service) CreatePasswordResetToken(ctx context.Context, email string) (string, error) {
	// Find user by email
	user, err := s.GetUserByEmail(ctx, email)
	if err != nil {
		// Don't reveal whether email exists
		return "", nil
	}

	// Invalidate any existing tokens for this user
	_, err = s.db.Exec(ctx, `
		DELETE FROM client.password_reset_tokens
		WHERE user_id = $1 AND used_at IS NULL
	`, user.ID)
	if err != nil {
		return "", fmt.Errorf("failed to invalidate existing tokens: %w", err)
	}

	// Generate new token
	token, err := GenerateResetToken()
	if err != nil {
		return "", err
	}

	expiresAt := time.Now().Add(ResetTokenDuration)
	_, err = s.db.Exec(ctx, `
		INSERT INTO client.password_reset_tokens (user_id, token, expires_at)
		VALUES ($1, $2, $3)
	`, user.ID, token, expiresAt)
	if err != nil {
		return "", fmt.Errorf("failed to create reset token: %w", err)
	}

	return token, nil
}

// ValidateResetToken checks if a reset token is valid and returns the user ID.
func (s *Service) ValidateResetToken(ctx context.Context, token string) (int, error) {
	var userID int
	var expiresAt time.Time
	var usedAt *time.Time

	err := s.db.QueryRow(ctx, `
		SELECT user_id, expires_at, used_at
		FROM client.password_reset_tokens
		WHERE token = $1
	`, token).Scan(&userID, &expiresAt, &usedAt)
	if err != nil {
		return 0, fmt.Errorf("invalid token")
	}

	if usedAt != nil {
		return 0, fmt.Errorf("token already used")
	}

	if time.Now().After(expiresAt) {
		return 0, fmt.Errorf("token expired")
	}

	return userID, nil
}

// ResetPassword validates the token and updates the user's password.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	userID, err := s.ValidateResetToken(ctx, token)
	if err != nil {
		return err
	}

	// Hash the new password
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	// Update password
	_, err = s.db.Exec(ctx, `
		UPDATE client.users SET password_hash = $1 WHERE id = $2
	`, hash, userID)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	// Mark token as used
	_, err = s.db.Exec(ctx, `
		UPDATE client.password_reset_tokens SET used_at = NOW() WHERE token = $1
	`, token)
	if err != nil {
		// Log but don't fail - password was already updated
	}

	// Invalidate all sessions for this user (force re-login)
	_, err = s.db.Exec(ctx, `
		DELETE FROM client.sessions WHERE user_id = $1
	`, userID)
	if err != nil {
		// Log but don't fail
	}

	return nil
}

// SendPasswordResetEmail sends a password reset email to the user.
func SendPasswordResetEmail(cfg SMTPConfig, toEmail, resetURL string) error {
	if cfg.Host == "" {
		return fmt.Errorf("SMTP not configured")
	}

	subject := "Password Reset Request"
	body := fmt.Sprintf(`Hello,

You requested to reset your password for RFP Intelligence.

Click the link below to reset your password:
%s

This link will expire in 1 hour.

If you did not request this reset, please ignore this email.

- RFP Intelligence Team
`, resetURL)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		cfg.From, toEmail, subject, body)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)

	err := smtp.SendMail(addr, auth, cfg.From, []string{toEmail}, []byte(msg))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// CleanExpiredResetTokens removes all expired reset tokens.
func (s *Service) CleanExpiredResetTokens(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `
		DELETE FROM client.password_reset_tokens
		WHERE expires_at < NOW() OR used_at IS NOT NULL
	`)
	if err != nil {
		return fmt.Errorf("failed to clean expired reset tokens: %w", err)
	}
	return nil
}
