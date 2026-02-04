// Package handlers provides HTTP handlers for the client app.
package handlers

import (
	"github.com/zachsouder/rfp/client/internal/auth"
	"github.com/zachsouder/rfp/client/internal/templates"
	"github.com/zachsouder/rfp/shared/db"
	"github.com/zachsouder/rfp/shared/r2"
)

// Handlers provides HTTP handlers for the client app.
type Handlers struct {
	db          *db.DB
	authService *auth.Service
	templates   *templates.Engine
	r2Client    *r2.Client
	r2AccountID string
}

// New creates a new handlers instance.
func New(database *db.DB, authService *auth.Service, tmpl *templates.Engine, r2Client *r2.Client, r2AccountID string) *Handlers {
	return &Handlers{
		db:          database,
		authService: authService,
		templates:   tmpl,
		r2Client:    r2Client,
		r2AccountID: r2AccountID,
	}
}
