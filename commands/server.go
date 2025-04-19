package commands

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jtarchie/ci/server"
	"github.com/jtarchie/ci/storage"
	_ "github.com/jtarchie/ci/storage/sqlite"
	"github.com/labstack/echo/v4"
)

type Server struct {
	Port    int    `default:"8080"             help:"Port to run the server on"`
	Storage string `default:"sqlite://test.db" help:"Path to storage file"      required:""`
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
	defer client.Close()

	router, err := server.NewRouter(logger)
	if err != nil {
		return fmt.Errorf("could not create router: %w", err)
	}

	router.GET("/tasks/*", func(ctx echo.Context) error {
		lookupPath := ctx.Param("*")
		if lookupPath == "" || lookupPath[0] != '/' {
			lookupPath = "/" + lookupPath
		}

		results, err := client.GetAll(lookupPath, []string{"status"})
		if err != nil {
			return fmt.Errorf("could not get all results: %w", err)
		}

		return ctx.Render(http.StatusOK, "results.html", map[string]any{
			"Tree": results.AsTree(),
		})
	})

	router.GET("/asciicast/*", func(ctx echo.Context) error {
		lookupPath := ctx.Param("*")
		if lookupPath == "" || lookupPath[0] != '/' {
			lookupPath = "/" + lookupPath
		}

		results, err := client.GetAll(lookupPath, []string{"stdout"})
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
