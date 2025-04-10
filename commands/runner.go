package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/google/uuid"
	"github.com/jtarchie/ci/backwards"
	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/runtime"
)

type Runner struct {
	Pipeline     string        `arg:""                                                        help:"Path to pipeline javascript file" type:"existingfile"`
	Orchestrator string        `default:"native"                                              help:"orchestrator runtime to use"`
	Timeout      time.Duration `help:"timeout for the pipeline, will cause abort if exceeded"`
}

func (c *Runner) Run() error {
	logger := slog.Default().WithGroup("runner").With(
		"id", uuid.New().String(),
		"pipeline", c.Pipeline,
		"orchestrator", c.Orchestrator,
	)

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if c.Timeout > 0 {
		// Create a context with timeout
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	// Set up signal handling
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// Handle signals in a separate goroutine
	go func() {
		sig := <-sigs
		logger.Debug("execution.canceled", "signal", sig)
		cancel() // Cancel the context when signal is received
	}()

	var pipeline string

	extension := filepath.Ext(c.Pipeline)
	if extension == ".yml" || extension == ".yaml" {
		var err error

		pipeline, err = backwards.NewPipeline(c.Pipeline)
		if err != nil {
			return fmt.Errorf("could not create pipeline from YAML: %w", err)
		}
	} else {
		result := api.Build(api.BuildOptions{
			EntryPoints:      []string{c.Pipeline},
			Bundle:           true,
			Sourcemap:        api.SourceMapInline,
			Platform:         api.PlatformNeutral,
			PreserveSymlinks: true,
			AbsWorkingDir:    filepath.Dir(c.Pipeline),
		})
		if len(result.Errors) > 0 {
			return fmt.Errorf("%w: %s", ErrCouldNotBundle, result.Errors[0].Text)
		}

		pipeline = string(result.OutputFiles[0].Contents)
	}

	orchestrator, found := orchestra.Get(c.Orchestrator)
	if !found {
		return fmt.Errorf("could not get orchestrator (%q): %w", c.Orchestrator, ErrOrchestratorNotFound)
	}

	driver, err := orchestrator("ci-"+uuid.New().String(), logger)
	if err != nil {
		return fmt.Errorf("could not create docker client: %w", err)
	}
	defer driver.Close()

	js := runtime.NewJS(logger)

	err = js.Execute(ctx, pipeline, driver)
	if err != nil {
		// Check if the error was due to context cancellation
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("execution cancelled: %w", err)
		}

		return fmt.Errorf("could not execute pipeline: %w", err)
	}

	return nil
}

var (
	ErrCouldNotBundle       = errors.New("could not bundle pipeline")
	ErrOrchestratorNotFound = errors.New("orchestrator not found")
)
