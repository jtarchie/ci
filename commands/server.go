package commands

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/georgysavva/scany/v2/sqlscan"
	sprig "github.com/go-task/slim-sprig/v3"
	"github.com/jtarchie/ci/server"
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

type Payload map[string]any

// nolint: wrapcheck
func (p *Payload) Value() (driver.Value, error) {
	return json.Marshal(p)
}

// nolint: wrapcheck,err113
func (p *Payload) Scan(value any) error {
	switch x := value.(type) {
	case string:
		return json.NewDecoder(bytes.NewBufferString(x)).Decode(p)
	case []byte:
		return json.NewDecoder(bytes.NewBuffer(x)).Decode(p)
	case nil:
		return nil
	default:
		return fmt.Errorf("cannot scan type %T: %v", value, value)
	}
}

func (c *Server) Run(logger *slog.Logger) error {
	client, err := sql.Open("sqlite", c.Storage)
	if err != nil {
		return fmt.Errorf("could not open storage file: %w", err)
	}
	defer client.Close()

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
		return fmt.Errorf("could not parse templates: %w", err)
	}

	renderer := &TemplateRender{
		templates: templates,
	}

	router := echo.New()
	router.Use(slogecho.New(logger))
	router.Use(middleware.Recover())
	router.Renderer = renderer

	router.GET("/", func(ctx echo.Context) error {
		type result struct {
			Path    string  `db:"path"`
			Payload Payload `db:"payload"`
		}

		var results []result

		err := sqlscan.Select(
			context.Background(),
			client,
			&results,
			`
				SELECT
					path, json(payload) as payload
				FROM
					tasks
				ORDER BY
					path
			`,
		)
		if err != nil {
			return fmt.Errorf("could not select: %w", err)
		}

		logger.Info("results", "results", len(results))

		path := server.NewPath[Payload]()
		for _, result := range results {
			path.AddChild(result.Path, result.Payload)
		}

		return ctx.Render(http.StatusOK, "results.html", map[string]any{
			"Path": path,
		})
	})

	router.GET("/health", func(ctx echo.Context) error {
		return ctx.String(http.StatusOK, "OK")
	})

	err = router.Start(fmt.Sprintf(":%d", c.Port))
	if err != nil {
		return fmt.Errorf("could not start server: %w", err)
	}

	return nil
}
