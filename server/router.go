package server

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/jtarchie/ci/secrets"
	"github.com/jtarchie/ci/storage"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	slogecho "github.com/samber/slog-echo"
)

// RouterOptions configures the router.
type RouterOptions struct {
	MaxInFlight           int
	WebhookTimeout        time.Duration
	BasicAuthUsername     string
	BasicAuthPassword     string
	AllowedDrivers        string
	AllowedFeatures       string
	SecretsManager        secrets.Manager
	FetchTimeout          time.Duration
	FetchMaxResponseBytes int64
}

// Router wraps echo.Echo and provides access to the execution service.
type Router struct {
	*echo.Echo
	execService     *ExecutionService
	webGroup        *echo.Group
	allowedDrivers  []string
	allowedFeatures []Feature
}

// WaitForExecutions blocks until all in-flight pipeline executions have completed.
// This is useful for graceful shutdown or testing.
func (r *Router) WaitForExecutions() {
	r.execService.Wait()
}

// ExecutionService returns the execution service for testing purposes.
func (r *Router) ExecutionService() *ExecutionService {
	return r.execService
}

// ProtectedGroup returns the web group that has basic auth middleware applied.
// Use this to add routes that should require authentication.
func (r *Router) ProtectedGroup() *echo.Group {
	return r.webGroup
}

// isHtmxRequest checks if the request is from htmx.
func isHtmxRequest(ctx echo.Context) bool {
	return ctx.Request().Header.Get("HX-Request") == "true"
}

// newBasicAuthMiddleware creates a basic auth middleware using Echo's built-in BasicAuth.
// If username/password are empty strings, the middleware is disabled (returns a no-op middleware).
func newBasicAuthMiddleware(username, password string) echo.MiddlewareFunc {
	if username == "" || password == "" {
		// No basic auth configured, return a no-op middleware
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return next
		}
	}

	return middleware.BasicAuth(func(u, p string, ctx echo.Context) (bool, error) {
		return u == username && p == password, nil
	})
}

func NewRouter(logger *slog.Logger, store storage.Driver, opts RouterOptions) (*Router, error) {
	router := echo.New()

	// Parse allowed drivers
	allowedDrivers := parseAllowedDrivers(opts.AllowedDrivers)

	// Parse allowed features
	allowedFeatures, err := ParseAllowedFeatures(opts.AllowedFeatures)
	if err != nil {
		return nil, fmt.Errorf("could not parse allowed features: %w", err)
	}

	// Create execution service with allowed drivers and features
	execService := NewExecutionService(store, logger, opts.MaxInFlight, allowedDrivers)
	execService.SecretsManager = opts.SecretsManager
	execService.AllowedFeatures = allowedFeatures
	execService.FetchTimeout = opts.FetchTimeout
	execService.FetchMaxResponseBytes = opts.FetchMaxResponseBytes
	router.Pre(middleware.AddTrailingSlashWithConfig(middleware.TrailingSlashConfig{
		Skipper: func(c echo.Context) bool {
			// Skip trailing slash middleware for static files, API routes, runs, and health
			path := c.Request().URL.Path
			if len(path) >= 7 && path[:7] == "/static" {
				return true
			}
			if len(path) >= 4 && path[:4] == "/api" {
				return true
			}
			if len(path) >= 5 && path[:5] == "/runs" {
				return true
			}
			if path == "/health" || path == "/health/" {
				return true
			}
			return false
		},
	}))
	router.Use(slogecho.New(logger))
	router.Use(middleware.Recover())

	renderer, err := newTemplates()
	if err != nil {
		return nil, fmt.Errorf("could not create templates: %w", err)
	}

	router.Renderer = renderer

	// Serve static files from embedded filesystem
	staticFiles, err := fs.Sub(staticFS, "static/dist")
	if err != nil {
		return nil, fmt.Errorf("could not create static files: %w", err)
	}
	router.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles)))))

	router.GET("/health", func(ctx echo.Context) error {
		return ctx.String(http.StatusOK, "OK")
	})
	router.GET("/health/", func(ctx echo.Context) error {
		return ctx.String(http.StatusOK, "OK")
	})

	// Create web UI group and apply basic auth middleware
	web := router.Group("")
	web.Use(newBasicAuthMiddleware(opts.BasicAuthUsername, opts.BasicAuthPassword))

	// Redirect root to pipelines list
	web.GET("/", func(ctx echo.Context) error {
		return ctx.Redirect(http.StatusMovedPermanently, "/pipelines/")
	})

	registerPipelineViewRoutes(web, store, execService)

	// Pipeline API endpoints
	// Register webhooks first (without auth) on the main router
	webhookTimeout := opts.WebhookTimeout
	if webhookTimeout == 0 {
		webhookTimeout = 5 * time.Second
	}
	registerWebhookRoutes(router, store, execService, webhookTimeout, allowedFeatures)

	// Create API group with basic auth middleware (for non-webhook endpoints)
	api := router.Group("/api")
	api.Use(newBasicAuthMiddleware(opts.BasicAuthUsername, opts.BasicAuthPassword))
	registerPipelineRoutes(api, store, execService, webhookTimeout, allowedDrivers, allowedFeatures)
	registerDriverRoutes(api, allowedDrivers)
	registerFeatureRoutes(api, allowedFeatures)

	registerRunViewRoutes(web, store)

	return &Router{Echo: router, execService: execService, webGroup: web, allowedDrivers: allowedDrivers, allowedFeatures: allowedFeatures}, nil
}
