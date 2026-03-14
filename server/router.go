package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/jtarchie/pocketci/secrets"
	"github.com/jtarchie/pocketci/storage"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
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
func isHtmxRequest(ctx *echo.Context) bool {
	return ctx.Request().Header.Get("HX-Request") == "true"
}

// newBasicAuthMiddleware creates a basic auth middleware using Echo's built-in BasicAuth.
// If username/password are empty strings, the middleware is disabled (returns a no-op middleware).
// newSlogMiddleware creates a request-logging middleware using slog.
func newSlogMiddleware(logger *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			start := time.Now()
			req := c.Request()

			err := next(c)

			attrs := []slog.Attr{
				slog.String("method", req.Method),
				slog.String("path", req.URL.Path),
				slog.Duration("latency", time.Since(start)),
				slog.String("remote_ip", c.RealIP()),
			}

			if resp, rErr := echo.UnwrapResponse(c.Response()); rErr == nil {
				attrs = append(attrs, slog.Int("status", resp.Status))
			}

			logger.LogAttrs(req.Context(), slog.LevelInfo, "request", attrs...)

			return err
		}
	}
}

func newBasicAuthMiddleware(username, password string) echo.MiddlewareFunc {
	if username == "" || password == "" {
		// No basic auth configured, return a no-op middleware
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return next
		}
	}

	return middleware.BasicAuth(func(c *echo.Context, u, p string) (bool, error) {
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

	// Recover orphaned runs from previous server instance
	execService.RecoverOrphanedRuns(context.Background())

	router.Use(newSlogMiddleware(logger))
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

	docsFiles, err := fs.Sub(docsFS, "docs/site")
	if err != nil {
		return nil, fmt.Errorf("could not create docs files: %w", err)
	}
	router.GET("/docs", func(ctx *echo.Context) error {
		return ctx.Redirect(http.StatusMovedPermanently, "/docs/")
	})
	router.GET("/docs/*", echo.WrapHandler(http.StripPrefix("/docs/", http.FileServer(http.FS(docsFiles)))))

	router.GET("/health", func(ctx *echo.Context) error {
		return ctx.String(http.StatusOK, "OK")
	})
	router.GET("/health/", func(ctx *echo.Context) error {
		return ctx.String(http.StatusOK, "OK")
	})

	// Create web UI group and apply basic auth middleware
	web := router.Group("")
	web.Use(newBasicAuthMiddleware(opts.BasicAuthUsername, opts.BasicAuthPassword))

	// Redirect root to pipelines list
	web.GET("/", func(ctx *echo.Context) error {
		return ctx.Redirect(http.StatusMovedPermanently, "/pipelines/")
	})

	webhookTimeout := opts.WebhookTimeout
	if webhookTimeout == 0 {
		webhookTimeout = 5 * time.Second
	}

	// Create API group with basic auth middleware (for non-webhook endpoints)
	api := router.Group("/api")
	api.Use(newBasicAuthMiddleware(opts.BasicAuthUsername, opts.BasicAuthPassword))

	registerRoutes(router, api, web, store, execService, allowedDrivers, allowedFeatures, opts.SecretsManager, webhookTimeout, logger)

	// MCP endpoint (authenticated)
	mcpHandler := newMCPHandler(store)
	web.Any("/mcp", echo.WrapHandler(mcpHandler))
	web.Any("/mcp/*", echo.WrapHandler(mcpHandler))

	return &Router{Echo: router, execService: execService, webGroup: web, allowedDrivers: allowedDrivers, allowedFeatures: allowedFeatures}, nil
}

// registerRoutes wires all controllers to their respective route groups.
func registerRoutes(
	router *echo.Echo,
	api *echo.Group,
	web *echo.Group,
	store storage.Driver,
	execService *ExecutionService,
	allowedDrivers []string,
	allowedFeatures []Feature,
	secretsMgr secrets.Manager,
	webhookTimeout time.Duration,
	logger *slog.Logger,
) {
	base := BaseController{store: store, execService: execService}

	// API controllers (JSON responses)
	(&APIPipelinesController{BaseController: base, allowedDrivers: allowedDrivers, allowedFeatures: allowedFeatures, secretsMgr: secretsMgr}).RegisterRoutes(api)
	(&APIRunsController{BaseController: base, allowedFeatures: allowedFeatures}).RegisterRoutes(api)
	(&APIDriversController{allowedDrivers: allowedDrivers}).RegisterRoutes(api)
	(&APIFeaturesController{allowedFeatures: allowedFeatures}).RegisterRoutes(api)

	// Webhooks registered on the main router (no auth group, before API group)
	(&APIWebhooksController{BaseController: base, allowedFeatures: allowedFeatures, webhookTimeout: webhookTimeout, logger: logger.WithGroup("webhook"), secretsMgr: secretsMgr}).RegisterRoutes(router)

	// Web controllers (HTML responses)
	(&WebPipelinesController{BaseController: base}).RegisterRoutes(web)
	(&WebRunsController{BaseController: base}).RegisterRoutes(web)
	(&WebMetricsController{BaseController: base}).RegisterRoutes(web)
}
