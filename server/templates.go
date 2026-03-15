package server

import (
	"embed"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	sprig "github.com/go-task/slim-sprig/v3"
	"github.com/jtarchie/pocketci/server/auth"
	"github.com/labstack/echo/v5"
)

//go:embed templates/*
var templatesFS embed.FS

//go:embed static/dist/*
var staticFS embed.FS

//go:embed all:docs/site
var docsFS embed.FS

type TemplateRender struct {
	templates *template.Template
}

func (t *TemplateRender) Render(c *echo.Context, w io.Writer, name string, data any) error {
	// Inject authenticated user into template data when available.
	if m, ok := data.(map[string]any); ok {
		if user := auth.GetUser(c); user != nil {
			m["CurrentUser"] = user
		}
	}

	// Clone templates and add context-aware functions for this render call.
	tmpl, err := t.templates.Clone()
	if err != nil {
		return fmt.Errorf("could not clone templates: %w", err)
	}

	tmpl = tmpl.Funcs(template.FuncMap{
		"currentUser": func() *auth.User {
			return auth.GetUser(c)
		},
	})

	err = tmpl.ExecuteTemplate(w, name, data)
	if err != nil {
		return fmt.Errorf("could not execute template: %w", err)
	}

	return nil
}

func newTemplates() (*TemplateRender, error) {
	templates, err := template.New("templates").
		Funcs(sprig.FuncMap()).
		Funcs(template.FuncMap{
			"durationBetween": func(start, end *time.Time) string {
				if start == nil {
					return "—"
				}
				e := time.Now()
				if end != nil {
					e = *end
				}
				d := e.Sub(*start).Round(time.Second)
				h := int(d.Hours())
				m := int(d.Minutes()) % 60
				s := int(d.Seconds()) % 60
				if h > 0 {
					return fmt.Sprintf("%dh %dm %ds", h, m, s)
				}
				if m > 0 {
					return fmt.Sprintf("%dm %ds", m, s)
				}
				return fmt.Sprintf("%ds", s)
			},
			"elapsedSince": func(v any) string {
				if v == nil {
					return "—"
				}
				s, ok := v.(string)
				if !ok || s == "" {
					return "—"
				}
				t, err := time.Parse(time.RFC3339, s)
				if err != nil {
					return "—"
				}
				d := time.Since(t).Round(time.Second)
				h := int(d.Hours())
				m := int(d.Minutes()) % 60
				sec := int(d.Seconds()) % 60
				if h > 0 {
					return fmt.Sprintf("%dh %dm %ds", h, m, sec)
				}
				if m > 0 {
					return fmt.Sprintf("%dm %ds", m, sec)
				}
				return fmt.Sprintf("%ds", sec)
			},
			"formatPath": func(path string) string {
				path = strings.ReplaceAll(path, " ", "")
				path = filepath.Clean(path)
				if path[0] != '/' {
					path = "/" + path
				}

				return strings.ReplaceAll(path, "/", " / ")
			},
			"safeHTML": func(s any) string {
				switch v := s.(type) {
				case string:
					return v
				default:
					return fmt.Sprintf("%v", s)
				}
			},
			// Placeholder — overridden per-render in Render() with the actual echo context.
			"currentUser": func() *auth.User {
				return nil
			},
		}).
		ParseFS(templatesFS, "templates/*")
	if err != nil {
		return nil, fmt.Errorf("could not parse templates: %w", err)
	}

	renderer := &TemplateRender{
		templates: templates,
	}

	return renderer, nil
}
