// Package templates provides HTML template rendering for the client app.
package templates

import (
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"strings"
)

// Engine is an HTML template rendering engine.
type Engine struct {
	templates map[string]*template.Template
	baseFS    fs.FS
}

// templateFuncs returns the common template functions.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"plus": func(a, b int) int {
			return a + b
		},
		"minus": func(a, b int) int {
			return a - b
		},
	}
}

// New creates a new template engine using the provided filesystem.
// It expects a base.html template and other templates that extend it.
func New(fsys fs.FS) (*Engine, error) {
	engine := &Engine{
		templates: make(map[string]*template.Template),
		baseFS:    fsys,
	}

	// Parse base template
	baseContent, err := fs.ReadFile(fsys, "templates/base.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read base template: %w", err)
	}

	// Find all page templates
	entries, err := fs.ReadDir(fsys, "templates")
	if err != nil {
		return nil, fmt.Errorf("failed to read templates directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "base.html" || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".html")
		pageContent, err := fs.ReadFile(fsys, "templates/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read template %s: %w", entry.Name(), err)
		}

		// Combine base + page template
		combined := string(baseContent) + "\n" + string(pageContent)
		tmpl, err := template.New(name).Funcs(templateFuncs()).Parse(combined)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", entry.Name(), err)
		}

		engine.templates[name] = tmpl
	}

	return engine, nil
}

// Render renders the named template with the given data.
func (e *Engine) Render(w io.Writer, name string, data any) error {
	tmpl, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("template %s not found", name)
	}
	return tmpl.Execute(w, data)
}

// PageData provides common data for page templates.
type PageData struct {
	Title     string
	ActiveNav string
	User      *User
	Flash     *Flash
	Data      any
}

// User represents a logged-in user for template rendering.
type User struct {
	ID        int
	Email     string
	FirstName string
	LastName  string
	IsAdmin   bool
}

// Flash represents a flash message to display.
type Flash struct {
	Type    string // "success", "error", "warning", "info"
	Message string
}
