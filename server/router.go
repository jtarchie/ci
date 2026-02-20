package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/storage"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	slogecho "github.com/samber/slog-echo"
)

// PipelineRequest represents the JSON body for creating a pipeline.
type PipelineRequest struct {
	Name          string `json:"name"`
	Content       string `json:"content"`
	DriverDSN     string `json:"driver_dsn"`
	WebhookSecret string `json:"webhook_secret"`
}

// RouterOptions configures the router.
type RouterOptions struct {
	MaxInFlight    int
	WebhookTimeout time.Duration
}

// Router wraps echo.Echo and provides access to the execution service.
type Router struct {
	*echo.Echo
	execService *ExecutionService
}

// WaitForExecutions blocks until all in-flight pipeline executions have completed.
// This is useful for graceful shutdown or testing.
func (r *Router) WaitForExecutions() {
	r.execService.Wait()
}

// ExecutionService returns the execution service for testing purposes.
func (r *Router) ExecutionService() *ExecutionService {
	return r.execService
}

// isHtmxRequest checks if the request is from htmx.
func isHtmxRequest(ctx echo.Context) bool {
	return ctx.Request().Header.Get("HX-Request") == "true"
}

func NewRouter(logger *slog.Logger, store storage.Driver, opts RouterOptions) (*Router, error) {
	router := echo.New()

	// Create execution service
	execService := NewExecutionService(store, logger, opts.MaxInFlight)
	router.Pre(middleware.AddTrailingSlashWithConfig(middleware.TrailingSlashConfig{
		Skipper: func(c echo.Context) bool {
			// Skip trailing slash middleware for static files, API routes, runs, and health
			path := c.Request().URL.Path
			if len(path) >= 7 && path[:7] == "/static" {
				return true
			}
			if len(path) >= 4 && path[:4] == "/api" {
				return true
			}
			if len(path) >= 5 && path[:5] == "/runs" {
				return true
			}
			if path == "/health" || path == "/health/" {
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
	router.GET("/health/", func(ctx echo.Context) error {
		return ctx.String(http.StatusOK, "OK")
	})

	// Redirect root to pipelines list
	router.GET("/", func(ctx echo.Context) error {
		return ctx.Redirect(http.StatusMovedPermanently, "/pipelines/")
	})

	// Pipeline web UI routes
	router.GET("/pipelines/", func(ctx echo.Context) error {
		pipelines, err := store.ListPipelines(ctx.Request().Context())
		if err != nil {
			return fmt.Errorf("could not list pipelines: %w", err)
		}

		if pipelines == nil {
			pipelines = []storage.Pipeline{}
		}

		return ctx.Render(http.StatusOK, "pipelines.html", map[string]any{
			"Pipelines": pipelines,
		})
	})

	router.GET("/pipelines/:id/", func(ctx echo.Context) error {
		id := ctx.Param("id")

		pipeline, err := store.GetPipeline(ctx.Request().Context(), id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ctx.String(http.StatusNotFound, "Pipeline not found")
			}
			return fmt.Errorf("could not get pipeline: %w", err)
		}

		runs, err := store.ListRunsByPipeline(ctx.Request().Context(), id)
		if err != nil {
			return fmt.Errorf("could not list runs: %w", err)
		}

		if runs == nil {
			runs = []storage.PipelineRun{}
		}

		return ctx.Render(http.StatusOK, "pipeline_detail.html", map[string]any{
			"Pipeline": pipeline,
			"Runs":     runs,
		})
	})

	// GET /pipelines/:id/runs-section - Returns just the runs section partial for htmx
	router.GET("/pipelines/:id/runs-section", func(ctx echo.Context) error {
		id := ctx.Param("id")

		runs, err := store.ListRunsByPipeline(ctx.Request().Context(), id)
		if err != nil {
			return fmt.Errorf("could not list runs: %w", err)
		}

		if runs == nil {
			runs = []storage.PipelineRun{}
		}

		return ctx.Render(http.StatusOK, "runs-section", map[string]any{
			"PipelineID": id,
			"Runs":       runs,
		})
	})

	// Pipeline API endpoints
	api := router.Group("/api")
	webhookTimeout := opts.WebhookTimeout
	if webhookTimeout == 0 {
		webhookTimeout = 5 * time.Second
	}
	registerPipelineRoutes(api, store, execService, webhookTimeout)

	// Run-specific views that look up tasks at /pipeline/<runID>/...
	router.GET("/runs/:id/tasks", func(ctx echo.Context) error {
		runID := ctx.Param("id")
		lookupPath := "/pipeline/" + runID + "/"

		results, err := store.GetAll(ctx.Request().Context(), lookupPath, []string{"status"})
		if err != nil {
			return fmt.Errorf("could not get all results: %w", err)
		}

		// Get run status for polling control
		run, runErr := store.GetRun(ctx.Request().Context(), runID)
		isActive := runErr == nil && (run.Status == storage.RunStatusQueued || run.Status == storage.RunStatusRunning)

		return ctx.Render(http.StatusOK, "results.html", map[string]any{
			"Tree":     results.AsTree(),
			"Path":     lookupPath,
			"RunID":    runID,
			"IsActive": isActive,
		})
	})

	router.GET("/runs/:id/graph", func(ctx echo.Context) error {
		runID := ctx.Param("id")
		lookupPath := "/pipeline/" + runID + "/"

		results, err := store.GetAll(ctx.Request().Context(), lookupPath, []string{"status", "dependsOn"})
		if err != nil {
			return fmt.Errorf("could not get all results: %w", err)
		}

		// Get run status for polling control
		run, runErr := store.GetRun(ctx.Request().Context(), runID)
		isActive := runErr == nil && (run.Status == storage.RunStatusQueued || run.Status == storage.RunStatusRunning)

		tree := results.AsTree()
		treeJSON, err := json.Marshal(tree)
		if err != nil {
			return fmt.Errorf("could not marshal tree: %w", err)
		}

		return ctx.Render(http.StatusOK, "graph.html", map[string]any{
			"Tree":     tree,
			"TreeJSON": string(treeJSON),
			"Path":     lookupPath,
			"RunID":    runID,
			"IsActive": isActive,
		})
	})

	// GET /runs/:id/tasks-partial - Returns just the tasks container for htmx polling
	router.GET("/runs/:id/tasks-partial", func(ctx echo.Context) error {
		runID := ctx.Param("id")
		lookupPath := "/pipeline/" + runID + "/"

		results, err := store.GetAll(ctx.Request().Context(), lookupPath, []string{"status"})
		if err != nil {
			return fmt.Errorf("could not get all results: %w", err)
		}

		// Get run status for polling control
		run, runErr := store.GetRun(ctx.Request().Context(), runID)
		isActive := runErr == nil && (run.Status == storage.RunStatusQueued || run.Status == storage.RunStatusRunning)

		return ctx.Render(http.StatusOK, "tasks-partial", map[string]any{
			"Tree":     results.AsTree(),
			"Path":     lookupPath,
			"RunID":    runID,
			"IsActive": isActive,
		})
	})

	// GET /runs/:id/graph-data - Returns just the graph data JSON for htmx polling
	router.GET("/runs/:id/graph-data", func(ctx echo.Context) error {
		runID := ctx.Param("id")
		lookupPath := "/pipeline/" + runID + "/"

		results, err := store.GetAll(ctx.Request().Context(), lookupPath, []string{"status", "dependsOn"})
		if err != nil {
			return fmt.Errorf("could not get all results: %w", err)
		}

		// Get run status for polling control
		run, runErr := store.GetRun(ctx.Request().Context(), runID)
		isActive := runErr == nil && (run.Status == storage.RunStatusQueued || run.Status == storage.RunStatusRunning)

		tree := results.AsTree()
		treeJSON, err := json.Marshal(tree)
		if err != nil {
			return fmt.Errorf("could not marshal tree: %w", err)
		}

		return ctx.Render(http.StatusOK, "graph-partial", map[string]any{
			"Tree":     tree,
			"TreeJSON": string(treeJSON),
			"Path":     lookupPath,
			"RunID":    runID,
			"IsActive": isActive,
		})
	})

	return &Router{Echo: router, execService: execService}, nil
}

func registerPipelineRoutes(api *echo.Group, store storage.Driver, execService *ExecutionService, webhookTimeout time.Duration) {
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

		pipeline, err := store.SavePipeline(ctx.Request().Context(), req.Name, req.Content, req.DriverDSN, req.WebhookSecret)
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to save pipeline: %v", err),
			})
		}

		return ctx.JSON(http.StatusCreated, pipeline)
	})

	// GET /api/pipelines - List all pipelines
	api.GET("/pipelines", func(ctx echo.Context) error {
		pipelines, err := store.ListPipelines(ctx.Request().Context())
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

		pipeline, err := store.GetPipeline(ctx.Request().Context(), id)
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

		err := store.DeletePipeline(ctx.Request().Context(), id)
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

	// POST /api/pipelines/:id/trigger - Trigger pipeline execution
	api.POST("/pipelines/:id/trigger", func(ctx echo.Context) error {
		id := ctx.Param("id")

		// Check if we can execute more pipelines
		if !execService.CanExecute() {
			if isHtmxRequest(ctx) {
				return ctx.String(http.StatusTooManyRequests, "Max concurrent executions reached")
			}
			return ctx.JSON(http.StatusTooManyRequests, map[string]any{
				"error":         "max concurrent executions reached",
				"in_flight":     execService.CurrentInFlight(),
				"max_in_flight": execService.MaxInFlight(),
			})
		}

		// Get the pipeline
		pipeline, err := store.GetPipeline(ctx.Request().Context(), id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				if isHtmxRequest(ctx) {
					return ctx.String(http.StatusNotFound, "Pipeline not found")
				}
				return ctx.JSON(http.StatusNotFound, map[string]string{
					"error": "pipeline not found",
				})
			}

			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to get pipeline: %v", err),
			})
		}

		// Trigger the pipeline
		run, err := execService.TriggerPipeline(ctx.Request().Context(), pipeline)
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to trigger pipeline: %v", err),
			})
		}

		// For htmx requests, return the updated runs section
		if isHtmxRequest(ctx) {
			runs, err := store.ListRunsByPipeline(ctx.Request().Context(), id)
			if err != nil {
				return fmt.Errorf("could not list runs: %w", err)
			}

			if runs == nil {
				runs = []storage.PipelineRun{}
			}

			return ctx.Render(http.StatusOK, "runs-section", map[string]any{
				"PipelineID": id,
				"Runs":       runs,
			})
		}

		return ctx.JSON(http.StatusAccepted, map[string]any{
			"run_id":      run.ID,
			"pipeline_id": pipeline.ID,
			"status":      run.Status,
			"message":     "pipeline execution started",
		})
	})

	// GET /api/runs/:run_id/status - Get run status
	api.GET("/runs/:run_id/status", func(ctx echo.Context) error {
		runID := ctx.Param("run_id")

		run, err := store.GetRun(ctx.Request().Context(), runID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ctx.JSON(http.StatusNotFound, map[string]string{
					"error": "run not found",
				})
			}

			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to get run: %v", err),
			})
		}

		return ctx.JSON(http.StatusOK, run)
	})

	// ANY /api/webhooks/:id - Trigger pipeline execution via webhook
	api.Any("/webhooks/:id", func(ctx echo.Context) error {
		id := ctx.Param("id")

		// Get the pipeline
		pipeline, err := store.GetPipeline(ctx.Request().Context(), id)
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

		// Read request body
		body, err := io.ReadAll(ctx.Request().Body)
		if err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "failed to read request body",
			})
		}

		// Validate webhook signature if secret is configured
		if pipeline.WebhookSecret != "" {
			signature := ctx.Request().Header.Get("X-Webhook-Signature")
			if signature == "" {
				signature = ctx.QueryParam("signature")
			}

			if signature == "" {
				return ctx.JSON(http.StatusUnauthorized, map[string]string{
					"error": "missing webhook signature",
				})
			}

			if !validateWebhookSignature(body, pipeline.WebhookSecret, signature) {
				return ctx.JSON(http.StatusUnauthorized, map[string]string{
					"error": "invalid webhook signature",
				})
			}
		}

		// Check if we can execute more pipelines
		if !execService.CanExecute() {
			return ctx.JSON(http.StatusTooManyRequests, map[string]any{
				"error":         "max concurrent executions reached",
				"in_flight":     execService.CurrentInFlight(),
				"max_in_flight": execService.MaxInFlight(),
			})
		}

		// Build webhook data from request
		headers := make(map[string]string)
		for key, values := range ctx.Request().Header {
			if len(values) > 0 {
				headers[key] = values[0]
			}
		}

		query := make(map[string]string)
		for key, values := range ctx.QueryParams() {
			if len(values) > 0 {
				query[key] = values[0]
			}
		}

		webhookData := &runtime.WebhookData{
			Method:  ctx.Request().Method,
			URL:     ctx.Request().URL.String(),
			Headers: headers,
			Body:    string(body),
			Query:   query,
		}

		// Create response channel for pipeline to send HTTP response
		responseChan := make(chan *runtime.HTTPResponse, 1)

		// Trigger the pipeline with webhook data
		run, err := execService.TriggerWebhookPipeline(ctx.Request().Context(), pipeline, webhookData, responseChan)
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to trigger pipeline: %v", err),
			})
		}

		// Wait for pipeline to respond or timeout
		select {
		case resp := <-responseChan:
			// Pipeline sent a response
			for key, value := range resp.Headers {
				ctx.Response().Header().Set(key, value)
			}

			if resp.Body != "" {
				return ctx.String(resp.Status, resp.Body)
			}

			return ctx.NoContent(resp.Status)

		case <-time.After(webhookTimeout):
			// Pipeline didn't respond in time, return 202 Accepted
			return ctx.JSON(http.StatusAccepted, map[string]any{
				"run_id":      run.ID,
				"pipeline_id": pipeline.ID,
				"status":      run.Status,
				"message":     "pipeline execution started",
			})
		}
	})
}

// validateWebhookSignature validates an HMAC-SHA256 signature of the request body.
func validateWebhookSignature(body []byte, secret, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedMAC := mac.Sum(nil)
	expectedSignature := hex.EncodeToString(expectedMAC)

	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}
