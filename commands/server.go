package commands

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"

	"github.com/georgysavva/scany/v2/sqlscan"
	sprig "github.com/go-task/slim-sprig/v3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	slogecho "github.com/samber/slog-echo"
)

//go:embed templates/*
var templatesFS embed.FS

type Server struct {
	Port    int    `default:"8080"              help:"Port to run the server on"`
	Storage string `help:"Path to storage file" required:""`
}

type TemplateRender struct {
	templates *template.Template
}

func (t *TemplateRender) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	err := t.templates.ExecuteTemplate(w, name, data)
	if err != nil {
		return fmt.Errorf("could not execute template: %w", err)
	}

	return nil
}

func (c *Server) Run() error {
	client, err := sql.Open("sqlite", c.Storage)
	if err != nil {
		return fmt.Errorf("could not open storage file: %w", err)
	}
	defer client.Close()

	templates, err := template.New("templates").Funcs(sprig.FuncMap()).ParseFS(templatesFS, "templates/*")
	if err != nil {
		return fmt.Errorf("could not parse templates: %w", err)
	}

	renderer := &TemplateRender{
		templates: templates,
	}

	server := echo.New()
	server.Use(slogecho.New(slog.Default()))
	server.Use(middleware.Recover())
	server.Renderer = renderer

	server.GET("/", func(ctx echo.Context) error {
		type result struct {
			Namespace string `db:"namespace"`
		}

		var results []result

		err := sqlscan.Select(
			context.Background(),
			client,
			&results,
			`
				SELECT
					DISTINCT namespace
				FROM tasks
			`,
		)
		if err != nil {
			return fmt.Errorf("could not select: %w", err)
		}

		return ctx.Render(http.StatusOK, "namespaces.html", map[string]any{
			"Results": results,
		})
	})

	server.GET("/health", func(ctx echo.Context) error {
		return ctx.String(http.StatusOK, "OK")
	})

	err = server.Start(fmt.Sprintf(":%d", c.Port))
	if err != nil {
		return fmt.Errorf("could not start server: %w", err)
	}

	return nil
}
