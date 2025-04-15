package commands

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jtarchie/ci/server"
	"github.com/jtarchie/ci/storage"
	"github.com/labstack/echo/v4"
)

type Server struct {
	Port    int    `default:"8080"              help:"Port to run the server on"`
	Storage string `help:"Path to storage file" required:""`
}

func (c *Server) Run(logger *slog.Logger) error {
	client, err := storage.NewSqlite(c.Storage, "")
	if err != nil {
		return fmt.Errorf("could not create sqlite client: %w", err)
	}
	defer client.Close()

	router, err := server.NewRouter(logger)
	if err != nil {
		return fmt.Errorf("could not create router: %w", err)
	}

	router.GET("/", func(ctx echo.Context) error {
		results, err := client.GetAll("", []string{"status"})
		if err != nil {
			return fmt.Errorf("could not get all results: %w", err)
		}

		path := server.NewPath[storage.Payload]()
		for _, result := range results {
			path.AddChild(result.Path, result.Payload)
		}

		return ctx.Render(http.StatusOK, "results.html", map[string]any{
			"Path": path,
		})
	})

	err = router.Start(fmt.Sprintf(":%d", c.Port))
	if err != nil {
		return fmt.Errorf("could not start server: %w", err)
	}

	return nil
}
