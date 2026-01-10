package server

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/jtarchie/ci/storage"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	slogecho "github.com/samber/slog-echo"
)

// PipelineRequest represents the JSON body for creating a pipeline.
type PipelineRequest struct {
	Name      string `json:"name"`
	Content   string `json:"content"`
	DriverDSN string `json:"driver_dsn"`
}

func NewRouter(logger *slog.Logger, store storage.Driver) (*echo.Echo, error) {
	router := echo.New()
	router.Pre(middleware.AddTrailingSlashWithConfig(middleware.TrailingSlashConfig{
		Skipper: func(c echo.Context) bool {
			// Skip trailing slash middleware for static files and API routes
			path := c.Request().URL.Path
			if len(path) >= 7 && path[:7] == "/static" {
				return true
			}
			if len(path) >= 4 && path[:4] == "/api" {
				return true
			}
			return false
		},
	}))
	router.Use(slogecho.New(logger))
	router.Use(middleware.Recover())

	renderer, err := newTemplates()
	if err != nil {
		return nil, fmt.Errorf("could not create templates: %w", err)
	}

	router.Renderer = renderer

	// Serve static files from embedded filesystem
	staticFiles, err := fs.Sub(staticFS, "static/dist")
	if err != nil {
		return nil, fmt.Errorf("could not create static files: %w", err)
	}
	router.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles)))))

	router.GET("/health", func(ctx echo.Context) error {
		return ctx.String(http.StatusOK, "OK")
	})

	// Pipeline API endpoints
	api := router.Group("/api")
	registerPipelineRoutes(api, store)

	return router, nil
}

func registerPipelineRoutes(api *echo.Group, store storage.Driver) {
	// POST /api/pipelines - Create a new pipeline
	api.POST("/pipelines", func(ctx echo.Context) error {
		var req PipelineRequest
		if err := ctx.Bind(&req); err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid request body",
			})
		}

		if req.Name == "" {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "name is required",
			})
		}

		if req.Content == "" {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "content is required",
			})
		}

		pipeline, err := store.SavePipeline(req.Name, req.Content, req.DriverDSN)
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to save pipeline: %v", err),
			})
		}

		return ctx.JSON(http.StatusCreated, pipeline)
	})

	// GET /api/pipelines - List all pipelines
	api.GET("/pipelines", func(ctx echo.Context) error {
		pipelines, err := store.ListPipelines()
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to list pipelines: %v", err),
			})
		}

		if pipelines == nil {
			pipelines = []storage.Pipeline{}
		}

		return ctx.JSON(http.StatusOK, pipelines)
	})

	// GET /api/pipelines/:id - Get a specific pipeline
	api.GET("/pipelines/:id", func(ctx echo.Context) error {
		id := ctx.Param("id")

		pipeline, err := store.GetPipeline(id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ctx.JSON(http.StatusNotFound, map[string]string{
					"error": "pipeline not found",
				})
			}

			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to get pipeline: %v", err),
			})
		}

		return ctx.JSON(http.StatusOK, pipeline)
	})

	// DELETE /api/pipelines/:id - Delete a pipeline
	api.DELETE("/pipelines/:id", func(ctx echo.Context) error {
		id := ctx.Param("id")

		err := store.DeletePipeline(id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ctx.JSON(http.StatusNotFound, map[string]string{
					"error": "pipeline not found",
				})
			}

			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to delete pipeline: %v", err),
			})
		}

		return ctx.NoContent(http.StatusNoContent)
	})
}
