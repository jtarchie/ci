package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/storage"
	"github.com/labstack/echo/v4"
)

func registerWebhookRoutes(router *echo.Echo, store storage.Driver, execService *ExecutionService, webhookTimeout time.Duration, allowedFeatures []Feature) {
	// ANY /api/webhooks/:id - Trigger pipeline execution via webhook
	router.Any("/api/webhooks/:id", func(ctx echo.Context) error {
		if !IsFeatureEnabled(FeatureWebhooks, allowedFeatures) {
			return ctx.JSON(http.StatusForbidden, map[string]string{
				"error": "webhooks feature is not enabled",
			})
		}

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

		body, err := io.ReadAll(ctx.Request().Body)
		if err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "failed to read request body",
			})
		}

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

		if !execService.CanExecute() {
			return ctx.JSON(http.StatusTooManyRequests, map[string]any{
				"error":         "max concurrent executions reached",
				"in_flight":     execService.CurrentInFlight(),
				"max_in_flight": execService.MaxInFlight(),
			})
		}

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

		responseChan := make(chan *runtime.HTTPResponse, 1)

		run, err := execService.TriggerWebhookPipeline(ctx.Request().Context(), pipeline, webhookData, responseChan)
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

		case <-time.After(webhookTimeout):
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
