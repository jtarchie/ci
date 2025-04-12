package commands

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/jtarchie/ci/backwards"
	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/storage"
)

type Runner struct {
	Storage      string        `default:":memory:"                                            help:"Path to storage file"`
	Pipeline     string        `arg:""                                                        help:"Path to pipeline javascript file" type:"existingfile"`
	Orchestrator string        `default:"native"                                              help:"orchestrator runtime to use"`
	Timeout      time.Duration `help:"timeout for the pipeline, will cause abort if exceeded"`
}

func youtubeIDStyle(input string) string {
	hash := sha256.Sum256([]byte(input))

	encoded := base64.RawURLEncoding.EncodeToString(hash[:])

	const maxLength = 11

	if len(encoded) > maxLength {
		return encoded[:maxLength] // YouTube IDs are 11 chars
	}

	return encoded
}

func (c *Runner) Run(loggers ...*slog.Logger) error {
	pipelinePath, err := filepath.Abs(c.Pipeline)
	if err != nil {
		return fmt.Errorf("could not get absolute path to pipeline: %w", err)
	}

	runtimeID := youtubeIDStyle(pipelinePath)

	if len(loggers) == 0 {
		loggers = []*slog.Logger{slog.Default()}
	}

	logger := loggers[0].WithGroup("runner").With(
		"id", runtimeID,
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

	extension := filepath.Ext(pipelinePath)
	if extension == ".yml" || extension == ".yaml" {
		var err error

		pipeline, err = backwards.NewPipeline(pipelinePath)
		if err != nil {
			return fmt.Errorf("could not create pipeline from YAML: %w", err)
		}
	} else {
		result := api.Build(api.BuildOptions{
			EntryPoints:      []string{pipelinePath},
			Bundle:           true,
			Sourcemap:        api.SourceMapInline,
			Platform:         api.PlatformNeutral,
			PreserveSymlinks: true,
			AbsWorkingDir:    filepath.Dir(pipelinePath),
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

	driver, err := orchestrator("ci-"+runtimeID, logger)
	if err != nil {
		return fmt.Errorf("could not create orchestrator client: %w", err)
	}
	defer driver.Close()

	storage, err := storage.NewSqlite(c.Storage, runtimeID)
	if err != nil {
		return fmt.Errorf("could not create sqlite client: %w", err)
	}
	defer storage.Close()

	js := runtime.NewJS(logger)

	err = js.Execute(ctx, pipeline, driver, storage)
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
