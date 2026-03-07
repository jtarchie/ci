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

// RegisterRoutes registers all run API routes on the given group.
func (c *APIRunsController) RegisterRoutes(api *echo.Group) {
	api.GET("/runs/:run_id/status", c.Status)
}
