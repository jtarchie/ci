package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/storage"
	"github.com/labstack/echo/v4"
)

// PipelineRow is a view model for the pipeline listing page that pairs a
// pipeline with its most recent run (nil if no runs exist yet).
type PipelineRow struct {
	Pipeline   storage.Pipeline
	LatestRun  *storage.PipelineRun
	DriverName string
}

// buildPipelineRows fetches the latest run for each pipeline and returns
// a slice of PipelineRow view models ready for the template.
func buildPipelineRows(ctx context.Context, store storage.Driver, pipelines []storage.Pipeline, defaultDriver string) []PipelineRow {
	rows := make([]PipelineRow, 0, len(pipelines))
	for _, p := range pipelines {
		row := PipelineRow{Pipeline: p, DriverName: driverNameFromDSN(p.DriverDSN, defaultDriver)}
		run, err := store.GetLatestRunByPipeline(ctx, p.ID)
		if err == nil {
			row.LatestRun = run
		}
		rows = append(rows, row)
	}
	return rows
}

func driverNameFromDSN(dsn, defaultDriver string) string {
	if strings.TrimSpace(dsn) == "" {
		if defaultDriver != "" {
			return defaultDriver
		}
		return "default"
	}

	config, err := orchestra.ParseDriverDSN(dsn)
	if err != nil || config.Name == "" {
		return "unknown"
	}

	return config.Name
}

func registerPipelineViewRoutes(web *echo.Group, store storage.Driver, execService *ExecutionService) {
	// GET /pipelines/ - Pipeline listing page
	web.GET("/pipelines/", func(ctx echo.Context) error {
		q := ctx.QueryParam("q")

		page := 1
		perPage := 20

		if p := ctx.QueryParam("page"); p != "" {
			_, _ = fmt.Sscanf(p, "%d", &page)
		}
		if pp := ctx.QueryParam("per_page"); pp != "" {
			_, _ = fmt.Sscanf(pp, "%d", &perPage)
		}

		result, err := store.SearchPipelines(ctx.Request().Context(), q, page, perPage)
		if err != nil {
			return fmt.Errorf("could not list pipelines: %w", err)
		}

		if result == nil || result.Items == nil {
			result = &storage.PaginationResult[storage.Pipeline]{
				Items:      []storage.Pipeline{},
				Page:       page,
				PerPage:    perPage,
				TotalItems: 0,
				TotalPages: 0,
				HasNext:    false,
			}
		}

		rows := buildPipelineRows(ctx.Request().Context(), store, result.Items, execService.DefaultDriver)

		return ctx.Render(http.StatusOK, "pipelines.html", map[string]any{
			"PipelineRows": rows,
			"Pagination":   result,
			"Query":        q,
		})
	})

	// GET /pipelines/search[/] - HTMX partial: returns only the pipeline table
	// rows filtered by the ?q= full-text search query.
	pipelinesSearchHandler := func(ctx echo.Context) error {
		q := ctx.QueryParam("q")

		page := 1
		perPage := 20

		if p := ctx.QueryParam("page"); p != "" {
			_, _ = fmt.Sscanf(p, "%d", &page)
		}
		if pp := ctx.QueryParam("per_page"); pp != "" {
			_, _ = fmt.Sscanf(pp, "%d", &perPage)
		}

		result, err := store.SearchPipelines(ctx.Request().Context(), q, page, perPage)
		if err != nil {
			return fmt.Errorf("could not search pipelines: %w", err)
		}

		if result == nil || result.Items == nil {
			result = &storage.PaginationResult[storage.Pipeline]{
				Items:      []storage.Pipeline{},
				Page:       page,
				PerPage:    perPage,
				TotalItems: 0,
				TotalPages: 0,
				HasNext:    false,
			}
		}

		rows := buildPipelineRows(ctx.Request().Context(), store, result.Items, execService.DefaultDriver)

		return ctx.Render(http.StatusOK, "_pipelines_content", map[string]any{
			"PipelineRows": rows,
			"Pagination":   result,
			"Query":        q,
		})
	}
	web.GET("/pipelines/search", pipelinesSearchHandler)
	web.GET("/pipelines/search/", pipelinesSearchHandler)

	// GET /pipelines/:id/ - Pipeline detail page
	web.GET("/pipelines/:id/", func(ctx echo.Context) error {
		id := ctx.Param("id")

		pipeline, err := store.GetPipeline(ctx.Request().Context(), id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ctx.String(http.StatusNotFound, "Pipeline not found")
			}
			return fmt.Errorf("could not get pipeline: %w", err)
		}

		page := 1
		perPage := 20

		if p := ctx.QueryParam("page"); p != "" {
			_, _ = fmt.Sscanf(p, "%d", &page)
		}
		if pp := ctx.QueryParam("per_page"); pp != "" {
			_, _ = fmt.Sscanf(pp, "%d", &perPage)
		}

		q := ctx.QueryParam("q")

		var result *storage.PaginationResult[storage.PipelineRun]
		var runsErr error
		if q != "" {
			result, runsErr = store.SearchRunsByPipeline(ctx.Request().Context(), id, q, page, perPage)
		} else {
			result, runsErr = store.ListRunsByPipeline(ctx.Request().Context(), id, page, perPage)
		}
		if runsErr != nil {
			return fmt.Errorf("could not list runs: %w", runsErr)
		}

		if result == nil || result.Items == nil {
			result = &storage.PaginationResult[storage.PipelineRun]{
				Items:      []storage.PipelineRun{},
				Page:       page,
				PerPage:    perPage,
				TotalItems: 0,
				TotalPages: 0,
				HasNext:    false,
			}
		}

		driverName := driverNameFromDSN(pipeline.DriverDSN, execService.DefaultDriver)
		return ctx.Render(http.StatusOK, "pipeline_detail.html", map[string]any{
			"Pipeline":   pipeline,
			"DriverName": driverName,
			"Runs":       result.Items,
			"Pagination": result,
			"Query":      q,
		})
	})

	// GET /pipelines/:id/runs-section[/] - HTMX partial: runs section for a pipeline
	runsSectionHandler := func(ctx echo.Context) error {
		id := ctx.Param("id")

		page := 1
		perPage := 20

		if p := ctx.QueryParam("page"); p != "" {
			_, _ = fmt.Sscanf(p, "%d", &page)
		}
		if pp := ctx.QueryParam("per_page"); pp != "" {
			_, _ = fmt.Sscanf(pp, "%d", &perPage)
		}

		result, err := store.ListRunsByPipeline(ctx.Request().Context(), id, page, perPage)
		if err != nil {
			return fmt.Errorf("could not list runs: %w", err)
		}

		if result == nil || result.Items == nil {
			result = &storage.PaginationResult[storage.PipelineRun]{
				Items:      []storage.PipelineRun{},
				Page:       page,
				PerPage:    perPage,
				TotalItems: 0,
				TotalPages: 0,
				HasNext:    false,
			}
		}

		return ctx.Render(http.StatusOK, "runs-section", map[string]any{
			"PipelineID": id,
			"Runs":       result.Items,
			"Pagination": result,
			"Query":      "",
		})
	}
	web.GET("/pipelines/:id/runs-section", runsSectionHandler)
	web.GET("/pipelines/:id/runs-section/", runsSectionHandler)

	// GET /pipelines/:id/runs-search[/] - HTMX partial: runs-section filtered by ?q=
	runsSearchHandler := func(ctx echo.Context) error {
		id := ctx.Param("id")
		q := ctx.QueryParam("q")

		page := 1
		perPage := 20

		if p := ctx.QueryParam("page"); p != "" {
			_, _ = fmt.Sscanf(p, "%d", &page)
		}
		if pp := ctx.QueryParam("per_page"); pp != "" {
			_, _ = fmt.Sscanf(pp, "%d", &perPage)
		}

		result, err := store.SearchRunsByPipeline(ctx.Request().Context(), id, q, page, perPage)
		if err != nil {
			return fmt.Errorf("could not search runs: %w", err)
		}

		if result == nil || result.Items == nil {
			result = &storage.PaginationResult[storage.PipelineRun]{
				Items:      []storage.PipelineRun{},
				Page:       page,
				PerPage:    perPage,
				TotalItems: 0,
				TotalPages: 0,
				HasNext:    false,
			}
		}

		return ctx.Render(http.StatusOK, "runs-section", map[string]any{
			"PipelineID": id,
			"Runs":       result.Items,
			"Pagination": result,
			"Query":      q,
		})
	}
	web.GET("/pipelines/:id/runs-search", runsSearchHandler)
	web.GET("/pipelines/:id/runs-search/", runsSearchHandler)

	// GET /pipelines/:id/source[/] - Pipeline source view
	sourceHandler := func(ctx echo.Context) error {
		id := ctx.Param("id")
		pipeline, err := store.GetPipeline(ctx.Request().Context(), id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ctx.String(http.StatusNotFound, "Pipeline not found")
			}
			return fmt.Errorf("could not get pipeline: %w", err)
		}
		return ctx.Render(http.StatusOK, "pipeline_source.html", map[string]any{
			"Pipeline": pipeline,
		})
	}
	web.GET("/pipelines/:id/source", sourceHandler)
	web.GET("/pipelines/:id/source/", sourceHandler)
}
