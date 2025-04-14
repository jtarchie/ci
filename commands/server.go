package commands

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	sprig "github.com/go-task/slim-sprig/v3"
	"github.com/jtarchie/ci/server"
	"github.com/jtarchie/ci/storage"
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

func (c *Server) Run(logger *slog.Logger) error {
	client, err := storage.NewSqlite(c.Storage, "")
	if err != nil {
		return fmt.Errorf("could not create sqlite client: %w", err)
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
		results, err := client.GetAll("")
		if err != nil {
			return fmt.Errorf("could not get all results: %w", err)
		}

		path := server.NewPath[storage.Payload]()
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
