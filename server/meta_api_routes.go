package server

import (
	"net/http"
	"strings"

	"github.com/jtarchie/ci/orchestra"
	"github.com/labstack/echo/v4"
)

// parseAllowedDrivers parses a comma-separated list of driver names.
// Returns ["*"] if input is empty or "*".
// Trims whitespace from each driver name.
func parseAllowedDrivers(input string) []string {
	if input == "" || input == "*" {
		return []string{"*"}
	}

	parts := strings.Split(input, ",")
	drivers := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			drivers = append(drivers, trimmed)
		}
	}

	if len(drivers) == 0 {
		return []string{"*"}
	}

	return drivers
}

// registerDriverRoutes adds API endpoints for listing allowed drivers.
func registerDriverRoutes(api *echo.Group, allowedDrivers []string) {
	// GET /api/drivers - List allowed drivers
	api.GET("/drivers", func(ctx echo.Context) error {
		var drivers []string

		if len(allowedDrivers) == 1 && allowedDrivers[0] == "*" {
			drivers = orchestra.ListDrivers()
		} else {
			drivers = allowedDrivers
		}

		return ctx.JSON(http.StatusOK, map[string]any{
			"drivers": drivers,
		})
	})
}

// registerFeatureRoutes adds API endpoints for listing allowed features.
func registerFeatureRoutes(api *echo.Group, allowedFeatures []Feature) {
	// GET /api/features - List allowed features
	api.GET("/features", func(ctx echo.Context) error {
		features := make([]string, len(allowedFeatures))
		for i, f := range allowedFeatures {
			features[i] = string(f)
		}

		return ctx.JSON(http.StatusOK, map[string]any{
			"features": features,
		})
	})
}
