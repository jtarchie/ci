package orchestra

import "os"

// GetParam retrieves a parameter from the params map, falling back to an environment variable if not found.
// Returns the parameter value, or the provided default if neither the param nor env var exist.
func GetParam(params map[string]string, key, envVar, defaultValue string) string {
	// Check DSN parameter first
	if value := params[key]; value != "" {
		return value
	}

	// Fall back to environment variable
	if envVar != "" {
		if value := os.Getenv(envVar); value != "" {
			return value
		}
	}

	// Return default value
	return defaultValue
}
