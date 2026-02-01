// Package auth provides authentication and session management for the client app.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

const (
	// SessionDuration is how long sessions last (30 days).
	SessionDuration = 30 * 24 * time.Hour
	// SessionTokenLength is the byte length of session tokens.
	SessionTokenLength = 32
	// BcryptCost is the cost factor for bcrypt hashing.
	BcryptCost = 12
)

// Querier is an interface for database query methods.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// User represents an authenticated user.
type User struct {
	ID           int
	Email        string
	PasswordHash string
	FirstName    string
	LastName     string
	Role         string
	LastActiveAt *time.Time
	CreatedAt    time.Time
}

// IsAdmin returns true if the user has admin role.
func (u *User) IsAdmin() bool {
	return u.Role == "admin"
}

// Session represents a user session.
type Session struct {
	ID        string
	UserID    int
	CreatedAt time.Time
	ExpiresAt time.Time
}

// HashPassword hashes a password using bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword verifies a password against its hash.
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateSessionToken creates a new secure random session token.
func GenerateSessionToken() (string, error) {
	bytes := make([]byte, SessionTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// Service provides authentication operations.
type Service struct {
	db Querier
}

// NewService creates a new auth service.
func NewService(db Querier) *Service {
	return &Service{db: db}
}

// Authenticate verifies email and password, returns user if valid.
func (s *Service) Authenticate(ctx context.Context, email, password string) (*User, error) {
	user, err := s.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	if !CheckPassword(password, user.PasswordHash) {
		return nil, fmt.Errorf("invalid password")
	}

	// Update last active timestamp
	_, err = s.db.Exec(ctx, `
		UPDATE client.users SET last_active_at = NOW() WHERE id = $1
	`, user.ID)
	if err != nil {
		// Log but don't fail - this is a non-critical update
	}

	return user, nil
}

// CreateSession creates a new session for the user.
func (s *Service) CreateSession(ctx context.Context, userID int) (*Session, error) {
	token, err := GenerateSessionToken()
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().Add(SessionDuration)
	_, err = s.db.Exec(ctx, `
		INSERT INTO client.sessions (id, user_id, expires_at)
		VALUES ($1, $2, $3)
	`, token, userID, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &Session{
		ID:        token,
		UserID:    userID,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}, nil
}

// ValidateSession checks if a session token is valid and returns the user.
func (s *Service) ValidateSession(ctx context.Context, token string) (*User, error) {
	var userID int
	var expiresAt time.Time

	err := s.db.QueryRow(ctx, `
		SELECT user_id, expires_at FROM client.sessions WHERE id = $1
	`, token).Scan(&userID, &expiresAt)
	if err != nil {
		return nil, fmt.Errorf("session not found")
	}

	if time.Now().After(expiresAt) {
		// Clean up expired session
		s.db.Exec(ctx, `DELETE FROM client.sessions WHERE id = $1`, token)
		return nil, fmt.Errorf("session expired")
	}

	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	return user, nil
}

// DeleteSession removes a session (logout).
func (s *Service) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM client.sessions WHERE id = $1`, token)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

// GetUserByEmail fetches a user by email.
func (s *Service) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	user := &User{}
	err := s.db.QueryRow(ctx, `
		SELECT id, email, password_hash, first_name, last_name, role, last_active_at, created_at
		FROM client.users WHERE email = $1
	`, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash,
		&user.FirstName, &user.LastName, &user.Role,
		&user.LastActiveAt, &user.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return user, nil
}

// GetUserByID fetches a user by ID.
func (s *Service) GetUserByID(ctx context.Context, id int) (*User, error) {
	user := &User{}
	err := s.db.QueryRow(ctx, `
		SELECT id, email, password_hash, first_name, last_name, role, last_active_at, created_at
		FROM client.users WHERE id = $1
	`, id).Scan(
		&user.ID, &user.Email, &user.PasswordHash,
		&user.FirstName, &user.LastName, &user.Role,
		&user.LastActiveAt, &user.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return user, nil
}

// CleanExpiredSessions removes all expired sessions.
func (s *Service) CleanExpiredSessions(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `DELETE FROM client.sessions WHERE expires_at < NOW()`)
	if err != nil {
		return fmt.Errorf("failed to clean expired sessions: %w", err)
	}
	return nil
}
