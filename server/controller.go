package server

import (
	"strings"

	"github.com/jtarchie/pocketci/storage"
)

// BaseController holds dependencies shared by all controllers.
type BaseController struct {
	store       storage.Driver
	execService *ExecutionService
}

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
