package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jtarchie/ci/server"
	"github.com/jtarchie/ci/storage"
	_ "github.com/jtarchie/ci/storage/sqlite"
	"github.com/labstack/echo/v4"
)

type Server struct {
	Port           int           `default:"8080"             env:"CI_PORT"                 help:"Port to run the server on"`
	Storage        string        `default:"sqlite://test.db" env:"CI_STORAGE"              help:"Path to storage file"                      required:""`
	MaxInFlight    int           `default:"10"               env:"CI_MAX_IN_FLIGHT"         help:"Maximum concurrent pipeline executions"`
	WebhookTimeout time.Duration `default:"5s"               env:"CI_WEBHOOK_TIMEOUT"       help:"Timeout waiting for pipeline webhook response"`
	BasicAuth      string        `env:"CI_BASIC_AUTH"         help:"Basic auth credentials in format 'username:password' (optional)"`
}

func (c *Server) Run(logger *slog.Logger) error {
	initStorage, found := storage.GetFromDSN(c.Storage)
	if !found {
		return fmt.Errorf("could not get storage driver: %w", errors.ErrUnsupported)
	}

	client, err := initStorage(c.Storage, "", logger)
	if err != nil {
		return fmt.Errorf("could not create sqlite client: %w", err)
	}
	defer func() { _ = client.Close() }()

	// Parse basic auth credentials if provided
	var basicAuthUsername, basicAuthPassword string
	if c.BasicAuth != "" {
		parts := strings.SplitN(c.BasicAuth, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid basic auth format: expected 'username:password', got '%s'", c.BasicAuth)
		}
		basicAuthUsername = parts[0]
		basicAuthPassword = parts[1]
		if basicAuthUsername == "" || basicAuthPassword == "" {
			return fmt.Errorf("basic auth username and password cannot be empty")
		}
	}

	router, err := server.NewRouter(logger, client, server.RouterOptions{
		MaxInFlight:       c.MaxInFlight,
		WebhookTimeout:    c.WebhookTimeout,
		BasicAuthUsername: basicAuthUsername,
		BasicAuthPassword: basicAuthPassword,
	})
	if err != nil {
		return fmt.Errorf("could not create router: %w", err)
	}

	router.ProtectedGroup().GET("/tasks/*", func(ctx echo.Context) error {
		lookupPath := ctx.Param("*")
		if lookupPath == "" || lookupPath[0] != '/' {
			lookupPath = "/" + lookupPath
		}

		results, err := client.GetAll(ctx.Request().Context(), lookupPath, []string{"status"})
		if err != nil {
			return fmt.Errorf("could not get all results: %w", err)
		}

		return ctx.Render(http.StatusOK, "results.html", map[string]any{
			"Tree": results.AsTree(),
			"Path": lookupPath,
		})
	})

	router.ProtectedGroup().GET("/graph/*", func(ctx echo.Context) error {
		lookupPath := ctx.Param("*")
		if lookupPath == "" || lookupPath[0] != '/' {
			lookupPath = "/" + lookupPath
		}

		results, err := client.GetAll(ctx.Request().Context(), lookupPath, []string{"status", "dependsOn"})
		if err != nil {
			return fmt.Errorf("could not get all results: %w", err)
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
		})
	})

	router.ProtectedGroup().GET("/asciicast/*", func(ctx echo.Context) error {
		lookupPath := ctx.Param("*")
		if lookupPath == "" || lookupPath[0] != '/' {
			lookupPath = "/" + lookupPath
		}

		results, err := client.GetAll(ctx.Request().Context(), lookupPath, []string{"stdout"})
		if err != nil {
			return fmt.Errorf("could not get all results: %w", err)
		}

		if len(results) > 1 {
			return fmt.Errorf("cannot render multiple results as asciicast: %w", errors.ErrUnsupported)
		}

		stdout, ok := results[0].Payload["stdout"].(string)
		if !ok {
			return fmt.Errorf("stdout is not a string: %w", errors.ErrUnsupported)
		}

		ctx.Response().Header().Set(echo.HeaderContentType, "application/x-asciicast")
		ctx.Response().WriteHeader(http.StatusOK)

		err = server.ToAsciiCast(stdout, ctx.Response().Writer)
		if err != nil {
			return fmt.Errorf("could not write asciicast: %w", err)
		}

		ctx.Response().Flush()

		return nil
	})

	err = router.Start(fmt.Sprintf(":%d", c.Port))
	if err != nil {
		return fmt.Errorf("could not start server: %w", err)
	}

	return nil
}
