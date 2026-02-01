// Package handlers provides HTTP handlers for the client app.
package handlers

import (
	"github.com/zachsouder/rfp/client/internal/auth"
	"github.com/zachsouder/rfp/client/internal/templates"
	"github.com/zachsouder/rfp/shared/db"
)

// Handlers provides HTTP handlers for the client app.
type Handlers struct {
	db          *db.DB
	authService *auth.Service
	templates   *templates.Engine
}

// New creates a new handlers instance.
func New(database *db.DB, authService *auth.Service, tmpl *templates.Engine) *Handlers {
	return &Handlers{
		db:          database,
		authService: authService,
		templates:   tmpl,
	}
}
