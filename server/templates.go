package server

import (
	"embed"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/template"

	sprig "github.com/go-task/slim-sprig/v3"
	"github.com/labstack/echo/v4"
)

//go:embed templates/*
var templatesFS embed.FS

//go:embed static/dist/*
var staticFS embed.FS

type TemplateRender struct {
	templates *template.Template
}

func (t *TemplateRender) Render(w io.Writer, name string, data any, c echo.Context) error {
	err := t.templates.ExecuteTemplate(w, name, data)
	if err != nil {
		return fmt.Errorf("could not execute template: %w", err)
	}

	return nil
}

func newTemplates() (*TemplateRender, error) {
	templates, err := template.New("templates").
		Funcs(sprig.FuncMap()).
		Funcs(template.FuncMap{
			"formatPath": func(path string) string {
				path = strings.ReplaceAll(path, " ", "")
				path = filepath.Clean(path)
				if path[0] != '/' {
					path = "/" + path
				}

				return strings.ReplaceAll(path, "/", " / ")
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
