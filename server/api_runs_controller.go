package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jtarchie/pocketci/storage"
	"github.com/labstack/echo/v5"
)

// APIRunsController handles JSON API endpoints for pipeline runs.
type APIRunsController struct {
	BaseController
}

// Status handles GET /api/runs/:run_id/status - Get run status.
func (c *APIRunsController) Status(ctx *echo.Context) error {
	runID := ctx.Param("run_id")

	run, err := c.store.GetRun(ctx.Request().Context(), runID)
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
}

// Stop handles POST /api/runs/:run_id/stop - Stop a running pipeline run.
func (c *APIRunsController) Stop(ctx *echo.Context) error {
	runID := ctx.Param("run_id")

	run, err := c.store.GetRun(ctx.Request().Context(), runID)
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

	if run.Status != storage.RunStatusRunning && run.Status != storage.RunStatusQueued {
		return ctx.JSON(http.StatusConflict, map[string]string{
			"error": "run is not currently in flight",
		})
	}

	if err := c.execService.StopRun(runID); err != nil {
		if errors.Is(err, ErrRunNotInFlight) {
			// The run appears running/queued in the DB but has no active goroutine
			// (e.g. the server crashed and restarted). Force it to failed directly.
			reqCtx := ctx.Request().Context()
			_ = c.store.UpdateStatusForPrefix(reqCtx, "/pipeline/"+runID+"/", []string{"pending", "running"}, "aborted")

			if updateErr := c.store.UpdateRunStatus(reqCtx, runID, storage.RunStatusFailed, "Run stopped by user"); updateErr != nil {
				return ctx.JSON(http.StatusInternalServerError, map[string]string{
					"error": fmt.Sprintf("failed to mark orphaned run as failed: %v", updateErr),
				})
			}

			return ctx.JSON(http.StatusOK, map[string]string{
				"run_id": runID,
				"status": "stopped",
			})
		}

		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to stop run: %v", err),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]string{
		"run_id": runID,
		"status": "stopping",
	})
}

// RegisterRoutes registers all run API routes on the given group.
func (c *APIRunsController) RegisterRoutes(api *echo.Group) {
	api.GET("/runs/:run_id/status", c.Status)
	api.POST("/runs/:run_id/stop", c.Stop)
}
