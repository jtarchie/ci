package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jtarchie/ci/storage"
	"github.com/labstack/echo/v5"
)

// WebRunsController handles HTML view endpoints for pipeline runs.
type WebRunsController struct {
	BaseController
}

// Show handles GET /runs/:id/tasks - Task tree view for a run.
func (c *WebRunsController) Show(ctx *echo.Context) error {
	runID := ctx.Param("id")
	lookupPath := "/pipeline/" + runID + "/"

	results, err := c.store.GetAll(ctx.Request().Context(), lookupPath, []string{"status", "elapsed", "started_at"})
	if err != nil {
		return fmt.Errorf("could not get all results: %w", err)
	}

	run, runErr := c.store.GetRun(ctx.Request().Context(), runID)
	isActive := runErr == nil && (run.Status == storage.RunStatusQueued || run.Status == storage.RunStatusRunning)

	var pipeline *storage.Pipeline
	title := "Tasks"
	if runErr == nil && run.PipelineID != "" {
		pipeline, _ = c.store.GetPipeline(ctx.Request().Context(), run.PipelineID)
		if pipeline != nil {
			title = "Tasks \u2014 " + pipeline.Name
		}
	}

	return ctx.Render(http.StatusOK, "results.html", map[string]any{
		"Tree":     results.AsTree(),
		"Path":     lookupPath,
		"RunID":    runID,
		"IsActive": isActive,
		"Run":      run,
		"Pipeline": pipeline,
		"Title":    title,
	})
}

// Graph handles GET /runs/:id/graph - Task graph view for a run.
func (c *WebRunsController) Graph(ctx *echo.Context) error {
	runID := ctx.Param("id")
	lookupPath := "/pipeline/" + runID + "/"

	results, err := c.store.GetAll(ctx.Request().Context(), lookupPath, []string{"status", "dependsOn"})
	if err != nil {
		return fmt.Errorf("could not get all results: %w", err)
	}

	run, runErr := c.store.GetRun(ctx.Request().Context(), runID)
	isActive := runErr == nil && (run.Status == storage.RunStatusQueued || run.Status == storage.RunStatusRunning)

	var pipeline *storage.Pipeline
	title := "Task Graph"
	if runErr == nil && run.PipelineID != "" {
		pipeline, _ = c.store.GetPipeline(ctx.Request().Context(), run.PipelineID)
		if pipeline != nil {
			title = "Task Graph \u2014 " + pipeline.Name
		}
	}

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
		"Pipeline": pipeline,
		"Title":    title,
	})
}

// TasksPartial handles GET /runs/:id/tasks-partial[/] - HTMX partial: tasks container for polling.
func (c *WebRunsController) TasksPartial(ctx *echo.Context) error {
	runID := ctx.Param("id")
	lookupPath := "/pipeline/" + runID + "/"
	q := ctx.QueryParam("q")

	var results storage.Results
	var err error

	if q != "" {
		// Full-text search: return only tasks whose output matches the query.
		// Disable live-polling while a search filter is active.
		results, err = c.store.Search(ctx.Request().Context(), "pipeline/"+runID, q)
		if err != nil {
			return fmt.Errorf("could not search tasks: %w", err)
		}

		return ctx.Render(http.StatusOK, "tasks-partial", map[string]any{
			"Tree":     results.AsTree(),
			"Path":     lookupPath,
			"RunID":    runID,
			"IsActive": false,
			"Run":      nil,
		})
	}

	results, err = c.store.GetAll(ctx.Request().Context(), lookupPath, []string{"status", "elapsed", "started_at"})
	if err != nil {
		return fmt.Errorf("could not get all results: %w", err)
	}

	run, runErr := c.store.GetRun(ctx.Request().Context(), runID)
	isActive := runErr == nil && (run.Status == storage.RunStatusQueued || run.Status == storage.RunStatusRunning)

	return ctx.Render(http.StatusOK, "tasks-partial", map[string]any{
		"Tree":     results.AsTree(),
		"Path":     lookupPath,
		"RunID":    runID,
		"IsActive": isActive,
		"Run":      run,
	})
}

// GraphData handles GET /runs/:id/graph-data[/] - HTMX partial: graph data JSON for polling.
func (c *WebRunsController) GraphData(ctx *echo.Context) error {
	runID := ctx.Param("id")
	lookupPath := "/pipeline/" + runID + "/"

	results, err := c.store.GetAll(ctx.Request().Context(), lookupPath, []string{"status", "dependsOn"})
	if err != nil {
		return fmt.Errorf("could not get all results: %w", err)
	}

	run, runErr := c.store.GetRun(ctx.Request().Context(), runID)
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
}

// RegisterRoutes registers all run web view routes on the given group.
func (c *WebRunsController) RegisterRoutes(web *echo.Group) {
	web.GET("/runs/:id/tasks", c.Show)
	web.GET("/runs/:id/graph", c.Graph)
	web.GET("/runs/:id/tasks-partial", c.TasksPartial)
	web.GET("/runs/:id/tasks-partial/", c.TasksPartial)
	web.GET("/runs/:id/graph-data", c.GraphData)
	web.GET("/runs/:id/graph-data/", c.GraphData)
}
