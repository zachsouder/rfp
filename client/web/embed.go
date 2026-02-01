// Package web provides embedded static and template files for the client app.
package web

import "embed"

// TemplatesFS contains embedded HTML templates.
//
//go:embed templates/*
var TemplatesFS embed.FS

// StaticFS contains embedded static assets (CSS, JS, images).
//
//go:embed static/*
var StaticFS embed.FS
