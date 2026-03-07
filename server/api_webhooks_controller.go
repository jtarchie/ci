package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/storage"
	"github.com/jtarchie/ci/webhooks"
	"github.com/labstack/echo/v5"
)

// APIWebhooksController handles webhook trigger endpoints.
type APIWebhooksController struct {
	BaseController
	allowedFeatures []Feature
	webhookTimeout  time.Duration
}

// Trigger handles ANY /api/webhooks/:id - Trigger pipeline execution via webhook.
func (c *APIWebhooksController) Trigger(ctx *echo.Context) error {
	if !IsFeatureEnabled(FeatureWebhooks, c.allowedFeatures) {
		return ctx.JSON(http.StatusForbidden, map[string]string{
			"error": "webhooks feature is not enabled",
		})
	}

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

	body, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "failed to read request body",
		})
	}

	event, err := webhooks.Detect(ctx.Request(), body, pipeline.WebhookSecret)
	if err != nil {
		if errors.Is(err, webhooks.ErrUnauthorized) {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{
				"error": "webhook signature validation failed",
			})
		}

		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("no webhook provider matched the request: %v", err),
		})
	}

	if !c.execService.CanExecute() {
		return ctx.JSON(http.StatusTooManyRequests, map[string]any{
			"error":         "max concurrent executions reached",
			"in_flight":     c.execService.CurrentInFlight(),
			"max_in_flight": c.execService.MaxInFlight(),
		})
	}

	webhookData := &runtime.WebhookData{
		Provider:  event.Provider,
		EventType: event.EventType,
		Method:    event.Method,
		URL:       event.URL,
		Headers:   event.Headers,
		Body:      event.Body,
		Query:     event.Query,
	}

	responseChan := make(chan *runtime.HTTPResponse, 1)

	run, err := c.execService.TriggerWebhookPipeline(ctx.Request().Context(), pipeline, webhookData, responseChan)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to trigger pipeline: %v", err),
		})
	}

	select {
	case resp := <-responseChan:
		for key, value := range resp.Headers {
			ctx.Response().Header().Set(key, value)
		}

		if resp.Body != "" {
			return ctx.String(resp.Status, resp.Body)
		}

		return ctx.NoContent(resp.Status)

	case <-time.After(c.webhookTimeout):
		return ctx.JSON(http.StatusAccepted, map[string]any{
			"run_id":      run.ID,
			"pipeline_id": pipeline.ID,
			"status":      run.Status,
			"message":     "pipeline execution started",
		})
	}
}

// RegisterRoutes registers all webhook routes on the main router (no auth group).
func (c *APIWebhooksController) RegisterRoutes(router *echo.Echo) {
	router.Any("/api/webhooks/:id", c.Trigger)
}
