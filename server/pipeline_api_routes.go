package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/storage"
	"github.com/klauspost/compress/zstd"
	"github.com/labstack/echo/v4"
)

// PipelineRequest represents the JSON body for creating a pipeline.
type PipelineRequest struct {
	Name          string `json:"name"`
	Content       string `json:"content"`
	DriverDSN     string `json:"driver_dsn"`
	WebhookSecret string `json:"webhook_secret"`
}

// PipelineAPIResponse is a sanitized pipeline representation for the public API.
type PipelineAPIResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toPipelineAPIResponse(pipeline *storage.Pipeline) PipelineAPIResponse {
	if pipeline == nil {
		return PipelineAPIResponse{}
	}

	return PipelineAPIResponse{
		ID:        pipeline.ID,
		Name:      pipeline.Name,
		Content:   pipeline.Content,
		CreatedAt: pipeline.CreatedAt,
		UpdatedAt: pipeline.UpdatedAt,
	}
}

func registerPipelineRoutes(api *echo.Group, store storage.Driver, execService *ExecutionService, webhookTimeout time.Duration, allowedDrivers []string, allowedFeatures []Feature) {
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

		if req.DriverDSN == "" {
			req.DriverDSN = execService.DefaultDriver
		}

		if err := orchestra.IsDriverAllowed(req.DriverDSN, allowedDrivers); err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("driver not allowed: %v", err),
			})
		}

		if req.WebhookSecret != "" && !IsFeatureEnabled(FeatureWebhooks, allowedFeatures) {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "webhooks feature is not enabled",
			})
		}

		pipeline, err := store.SavePipeline(ctx.Request().Context(), req.Name, req.Content, req.DriverDSN, req.WebhookSecret)
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to save pipeline: %v", err),
			})
		}

		return ctx.JSON(http.StatusCreated, toPipelineAPIResponse(pipeline))
	})

	// GET /api/pipelines - List all pipelines
	api.GET("/pipelines", func(ctx echo.Context) error {
		page := 1
		perPage := 20

		if p := ctx.QueryParam("page"); p != "" {
			_, _ = fmt.Sscanf(p, "%d", &page)
		}
		if pp := ctx.QueryParam("per_page"); pp != "" {
			_, _ = fmt.Sscanf(pp, "%d", &perPage)
		}

		result, err := store.ListPipelines(ctx.Request().Context(), page, perPage)
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to list pipelines: %v", err),
			})
		}

		if result == nil {
			result = &storage.PaginationResult[storage.Pipeline]{
				Items:      []storage.Pipeline{},
				Page:       page,
				PerPage:    perPage,
				TotalItems: 0,
				TotalPages: 0,
				HasNext:    false,
			}
		}

		items := make([]PipelineAPIResponse, 0, len(result.Items))
		for i := range result.Items {
			item := result.Items[i]
			items = append(items, toPipelineAPIResponse(&item))
		}

		return ctx.JSON(http.StatusOK, storage.PaginationResult[PipelineAPIResponse]{
			Items:      items,
			Page:       result.Page,
			PerPage:    result.PerPage,
			TotalItems: result.TotalItems,
			TotalPages: result.TotalPages,
			HasNext:    result.HasNext,
		})
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

		return ctx.JSON(http.StatusOK, toPipelineAPIResponse(pipeline))
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

		run, err := execService.TriggerPipeline(ctx.Request().Context(), pipeline)
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to trigger pipeline: %v", err),
			})
		}

		if isHtmxRequest(ctx) {
			result, err := store.ListRunsByPipeline(ctx.Request().Context(), id, 1, 20)
			if err != nil {
				return fmt.Errorf("could not list runs: %w", err)
			}

			if result == nil || result.Items == nil {
				result = &storage.PaginationResult[storage.PipelineRun]{Items: []storage.PipelineRun{}}
			}

			return ctx.Render(http.StatusOK, "runs-section", map[string]any{
				"PipelineID": id,
				"Runs":       result.Items,
				"Pagination": result,
				"Query":      "",
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

	// POST /api/pipelines/:name/run - Run a stored pipeline by name (synchronous SSE stream)
	api.POST("/pipelines/:name/run", func(ctx echo.Context) error {
		name := ctx.Param("name")

		var args []string
		var workdirTar io.Reader

		// Try multipart streaming first (preferred: allows large workdir tars without buffering).
		if mr, err := ctx.Request().MultipartReader(); err == nil {
			for {
				part, partErr := mr.NextPart()
				if partErr == io.EOF {
					break
				}
				if partErr != nil {
					break
				}

				switch part.FormName() {
				case "args":
					data, _ := io.ReadAll(part)
					_ = json.Unmarshal(data, &args)
				case "workdir":
					zr, zErr := zstd.NewReader(part)
					if zErr != nil {
						break
					}
					defer zr.Close()
					workdirTar = zr
				}

				if workdirTar != nil {
					// workdir part found, stop iterating to preserve the reader.
					break
				}
			}
		} else {
			// Fall back to JSON body (no workdir support in this path).
			var req struct {
				Args []string `json:"args"`
			}
			_ = json.NewDecoder(ctx.Request().Body).Decode(&req)
			args = req.Args
		}

		w := ctx.Response().Writer

		err := execService.RunByNameSync(ctx.Request().Context(), name, args, workdirTar, w)
		if err != nil {
			if !ctx.Response().Committed {
				if errors.Is(err, storage.ErrNotFound) {
					return ctx.JSON(http.StatusNotFound, map[string]string{
						"error": "pipeline not found",
					})
				}
				return ctx.JSON(http.StatusInternalServerError, map[string]string{
					"error": err.Error(),
				})
			}

			errData, _ := json.Marshal(map[string]string{"event": "error", "message": err.Error()})
			fmt.Fprintf(w, "data: %s\n\n", errData) //nolint:errcheck
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}

		return nil
	})
}
