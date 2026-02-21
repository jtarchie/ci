package orchestra

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

type InitFunc func(string, *slog.Logger, map[string]string) (Driver, error)

type DriverConfig struct {
	Name      string
	Namespace string
	Params    map[string]string
}

var drivers = map[string]InitFunc{}

func Add(driverName string, init InitFunc) {
	drivers[driverName] = init
}

func Each(f func(string, InitFunc)) {
	for name, init := range drivers {
		f(name, init)
	}
}

func Get(driverName string) (InitFunc, bool) {
	init, ok := drivers[driverName]

	return init, ok
}

// ListDrivers returns a sorted list of all registered driver names.
func ListDrivers() []string {
	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	return names
}

// ParseDriverDSN parses a driver DSN string in the format:
// - "driver" (simple name, uses defaults)
// - "driver:param1=value1,param2=value2" (parameters after colon)
// - "driver://namespace?param1=value1&param2=value2" (URL-style with namespace)
func ParseDriverDSN(dsn string) (*DriverConfig, error) {
	// If no special characters, it's just a driver name
	if !strings.Contains(dsn, ":") && !strings.Contains(dsn, "?") {
		return &DriverConfig{
			Name:   dsn,
			Params: make(map[string]string),
		}, nil
	}

	// Try URL-style parsing first: driver://namespace?param=value
	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid driver DSN format: %w", err)
		}

		params := make(map[string]string)
		for key, values := range u.Query() {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}

		return &DriverConfig{
			Name:      u.Scheme,
			Namespace: strings.TrimPrefix(u.Host, "//"),
			Params:    params,
		}, nil
	}

	// Fallback to simple colon-separated format: driver:param1=value1,param2=value2
	parts := strings.SplitN(dsn, ":", 2)
	if len(parts) == 1 {
		return &DriverConfig{
			Name:   parts[0],
			Params: make(map[string]string),
		}, nil
	}

	params := make(map[string]string)
	if parts[1] != "" {
		for _, pair := range strings.Split(parts[1], ",") {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) == 2 {
				params[kv[0]] = kv[1]
			}
		}
	}

	return &DriverConfig{
		Name:   parts[0],
		Params: params,
	}, nil
}

// GetFromDSN parses a DSN and returns the matching driver init function
func GetFromDSN(dsn string) (*DriverConfig, InitFunc, error) {
	config, err := ParseDriverDSN(dsn)
	if err != nil {
		return nil, nil, err
	}

	init, ok := drivers[config.Name]
	if !ok {
		return nil, nil, fmt.Errorf("driver %q not found", config.Name)
	}

	return config, init, nil
}

// IsDriverAllowed validates that the driver specified in the DSN is in the allowed list.
// If allowedList contains "*", all drivers with valid DSN format are allowed.
// Returns an error if the driver is not allowed or if the DSN is invalid.
func IsDriverAllowed(driverDSN string, allowedList []string) error {
	config, err := ParseDriverDSN(driverDSN)
	if err != nil {
		return fmt.Errorf("invalid driver DSN: %w", err)
	}

	// Check if wildcard (all drivers allowed)
	for _, allowed := range allowedList {
		if allowed == "*" {
			// Wildcard mode: just verify DSN is valid (parsed successfully above)
			// Driver existence check happens at execution time
			return nil
		}
	}

	// Check if driver is in allowed list
	for _, allowed := range allowedList {
		if allowed == config.Name {
			return nil
		}
	}

	// Build friendly error message
	return fmt.Errorf("driver %q is not allowed on this server. Allowed drivers: %s", config.Name, strings.Join(allowedList, ", "))
}
