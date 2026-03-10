package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/jtarchie/pocketci/orchestra"
	"github.com/jtarchie/pocketci/secrets"
	"github.com/jtarchie/pocketci/storage"
	"github.com/klauspost/compress/zstd"
	"github.com/labstack/echo/v5"
)

// PipelineRequest represents the JSON body for creating or updating a pipeline.
type PipelineRequest struct {
	Content       string            `json:"content"`
	ContentType   string            `json:"content_type"`
	DriverDSN     string            `json:"driver_dsn"`
	WebhookSecret *string           `json:"webhook_secret,omitempty"`
	Secrets       map[string]string `json:"secrets,omitempty"`
}

// PipelineAPIResponse is a sanitized pipeline representation for the public API.
type PipelineAPIResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Content     string    `json:"content"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toPipelineAPIResponse(pipeline *storage.Pipeline) PipelineAPIResponse {
	if pipeline == nil {
		return PipelineAPIResponse{}
	}

	return PipelineAPIResponse{
		ID:          pipeline.ID,
		Name:        pipeline.Name,
		Content:     pipeline.Content,
		ContentType: pipeline.ContentType,
		CreatedAt:   pipeline.CreatedAt,
		UpdatedAt:   pipeline.UpdatedAt,
	}
}

// APIPipelinesController handles JSON API endpoints for pipelines.
type APIPipelinesController struct {
	BaseController
	allowedDrivers  []string
	allowedFeatures []Feature
	secretsMgr      secrets.Manager
}

// Index handles GET /api/pipelines - List all pipelines.
func (c *APIPipelinesController) Index(ctx *echo.Context) error {
	page := 1
	perPage := 20

	if p := ctx.QueryParam("page"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &page)
	}
	if pp := ctx.QueryParam("per_page"); pp != "" {
		_, _ = fmt.Sscanf(pp, "%d", &perPage)
	}

	result, err := c.store.SearchPipelines(ctx.Request().Context(), "", page, perPage)
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
}

// Show handles GET /api/pipelines/:id - Get a specific pipeline.
func (c *APIPipelinesController) Show(ctx *echo.Context) error {
	id := ctx.Param("id")

	pipeline, err := c.store.GetPipeline(ctx.Request().Context(), id)
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
}

// Upsert handles PUT /api/pipelines/:name - Create or update a pipeline by name.
func (c *APIPipelinesController) Upsert(ctx *echo.Context) error {
	name := ctx.Param("name")

	if name == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "name is required",
		})
	}

	var req PipelineRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
	}

	if req.Content == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "content is required",
		})
	}

	if req.DriverDSN == "" {
		req.DriverDSN = c.execService.DefaultDriver
	}

	if err := orchestra.IsDriverAllowed(req.DriverDSN, c.allowedDrivers); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("driver not allowed: %v", err),
		})
	}

	if req.WebhookSecret != nil && *req.WebhookSecret != "" && !IsFeatureEnabled(FeatureWebhooks, c.allowedFeatures) {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "webhooks feature is not enabled",
		})
	}

	if req.WebhookSecret != nil && *req.WebhookSecret != "" && c.secretsMgr == nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "secrets backend is not configured on the server",
		})
	}

	// Validate secrets: require feature gate and secrets manager
	if len(req.Secrets) > 0 {
		if !IsFeatureEnabled(FeatureSecrets, c.allowedFeatures) {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "secrets feature is not enabled",
			})
		}

		if c.secretsMgr == nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "secrets backend is not configured on the server",
			})
		}
	}

	if len(req.Secrets) > 0 && c.secretsMgr != nil {
		existingPipeline, getErr := c.store.GetPipelineByName(ctx.Request().Context(), name)
		if getErr != nil && !errors.Is(getErr, storage.ErrNotFound) {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("failed to get existing pipeline by name: %v", getErr),
			})
		}

		if getErr == nil {
			scope := secrets.PipelineScope(existingPipeline.ID)

			existingKeys, listErr := c.secretsMgr.ListByScope(ctx.Request().Context(), scope)
			if listErr != nil {
				return ctx.JSON(http.StatusInternalServerError, map[string]string{
					"error": fmt.Sprintf("failed to list existing secrets: %v", listErr),
				})
			}

			for _, existingKey := range existingKeys {
				// webhook_secret is managed by req.WebhookSecret and should not be coupled
				// to generic pipeline secrets in req.Secrets.
				if existingKey == "webhook_secret" {
					continue
				}

				if _, ok := req.Secrets[existingKey]; !ok {
					return ctx.JSON(http.StatusBadRequest, map[string]string{
						"error": fmt.Sprintf("missing existing secret key %q: all existing secrets must be included on update", existingKey),
					})
				}
			}
		}
	}

	pipeline, err := c.store.SavePipeline(ctx.Request().Context(), name, req.Content, req.DriverDSN, req.ContentType)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to save pipeline: %v", err),
		})
	}

	if req.WebhookSecret != nil && c.secretsMgr != nil {
		scope := secrets.PipelineScope(pipeline.ID)
		if *req.WebhookSecret == "" {
			if err := c.secretsMgr.Delete(ctx.Request().Context(), scope, "webhook_secret"); err != nil && !errors.Is(err, secrets.ErrNotFound) {
				return ctx.JSON(http.StatusInternalServerError, map[string]string{
					"error": fmt.Sprintf("failed to delete webhook secret: %v", err),
				})
			}
		} else {
			if err := c.secretsMgr.Set(ctx.Request().Context(), scope, "webhook_secret", *req.WebhookSecret); err != nil {
				return ctx.JSON(http.StatusInternalServerError, map[string]string{
					"error": fmt.Sprintf("failed to store webhook secret: %v", err),
				})
			}
		}
	}

	// Store per-pipeline secrets if provided
	if len(req.Secrets) > 0 && c.secretsMgr != nil {
		scope := secrets.PipelineScope(pipeline.ID)

		// Write all secrets
		// Sort keys for deterministic ordering
		sortedKeys := make([]string, 0, len(req.Secrets))
		for k := range req.Secrets {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Strings(sortedKeys)

		for _, key := range sortedKeys {
			if err := c.secretsMgr.Set(ctx.Request().Context(), scope, key, req.Secrets[key]); err != nil {
				return ctx.JSON(http.StatusInternalServerError, map[string]string{
					"error": fmt.Sprintf("failed to store secret %q: %v", key, err),
				})
			}
		}
	}

	return ctx.JSON(http.StatusOK, toPipelineAPIResponse(pipeline))
}

// Destroy handles DELETE /api/pipelines/:id - Delete a pipeline.
func (c *APIPipelinesController) Destroy(ctx *echo.Context) error {
	id := ctx.Param("id")

	err := c.store.DeletePipeline(ctx.Request().Context(), id)
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

	// Cascade delete pipeline-scoped secrets
	if c.secretsMgr != nil {
		_ = c.secretsMgr.DeleteByScope(ctx.Request().Context(), secrets.PipelineScope(id))
	}

	return ctx.NoContent(http.StatusNoContent)
}

// Trigger handles POST /api/pipelines/:id/trigger - Trigger pipeline execution.
func (c *APIPipelinesController) Trigger(ctx *echo.Context) error {
	id := ctx.Param("id")

	if !c.execService.CanExecute() {
		if isHtmxRequest(ctx) {
			return ctx.String(http.StatusTooManyRequests, "Max concurrent executions reached")
		}
		return ctx.JSON(http.StatusTooManyRequests, map[string]any{
			"error":         "max concurrent executions reached",
			"in_flight":     c.execService.CurrentInFlight(),
			"max_in_flight": c.execService.MaxInFlight(),
		})
	}

	pipeline, err := c.store.GetPipeline(ctx.Request().Context(), id)
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

	run, err := c.execService.TriggerPipeline(ctx.Request().Context(), pipeline)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to trigger pipeline: %v", err),
		})
	}

	if isHtmxRequest(ctx) {
		ctx.Response().Header().Set("HX-Trigger", fmt.Sprintf(`{"showToast":{"message":"%s triggered successfully","type":"success"}}`, pipeline.Name))

		return ctx.NoContent(http.StatusOK)
	}

	return ctx.JSON(http.StatusAccepted, map[string]any{
		"run_id":      run.ID,
		"pipeline_id": pipeline.ID,
		"status":      run.Status,
		"message":     "pipeline execution started",
	})
}

// Run handles POST /api/pipelines/:name/run - Run a stored pipeline by name (synchronous SSE stream).
func (c *APIPipelinesController) Run(ctx *echo.Context) error {
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

	w := ctx.Response()

	err := c.execService.RunByNameSync(ctx.Request().Context(), name, args, workdirTar, w)
	if err != nil {
		echoResp, _ := echo.UnwrapResponse(ctx.Response())
		if echoResp == nil || !echoResp.Committed {
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
}

// RegisterRoutes registers all pipeline API routes on the given group.
func (c *APIPipelinesController) RegisterRoutes(api *echo.Group) {
	api.GET("/pipelines", c.Index)
	api.GET("/pipelines/:id", c.Show)
	api.PUT("/pipelines/:name", c.Upsert)
	api.DELETE("/pipelines/:id", c.Destroy)
	api.POST("/pipelines/:id/trigger", c.Trigger)
	api.POST("/pipelines/:name/run", c.Run)
}
