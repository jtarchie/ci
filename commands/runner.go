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
	"github.com/jtarchie/ci/orchestra/cache"
	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/storage"
)

type Runner struct {
	Storage  string        `default:"sqlite://test.db"                                    help:"Path to storage file"                                                                                                                                      required:""`
	Pipeline string        `arg:""                                                        help:"Path to pipeline javascript file"                                                                                                                          type:"existingfile"`
	Driver   string        `default:"native"                                              help:"Orchestrator driver DSN (e.g., 'k8s:namespace=my-ns', 'k8s://my-ns', 'docker', 'native')"`
	Timeout  time.Duration `help:"timeout for the pipeline, will cause abort if exceeded"`
	Resume   bool          `help:"Resume from last checkpoint if pipeline was interrupted"`
	RunID    string        `help:"Unique run ID for resume support (auto-generated if not provided)"`
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

func (c *Runner) Run(logger *slog.Logger) error {
	initStorage, found := storage.GetFromDSN(c.Storage)
	if !found {
		return fmt.Errorf("could not get storage driver: %w", errors.ErrUnsupported)
	}

	pipelinePath, err := filepath.Abs(c.Pipeline)
	if err != nil {
		return fmt.Errorf("could not get absolute path to pipeline: %w", err)
	}

	runtimeID := youtubeIDStyle(pipelinePath)

	if logger == nil {
		logger = slog.Default()
	}

	logger = logger.WithGroup("runner.run").With(
		"id", runtimeID,
		"pipeline", c.Pipeline,
		"orchestrator", c.Driver,
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

	driverConfig, orchestrator, err := orchestra.GetFromDSN(c.Driver)
	if err != nil {
		return fmt.Errorf("could not parse driver DSN (%q): %w", c.Driver, err)
	}

	// Use namespace from DSN if provided, otherwise use generated ID
	namespace := driverConfig.Namespace
	if namespace == "" {
		namespace = "ci-" + runtimeID
	}

	driver, err := orchestrator(namespace, logger, driverConfig.Params)
	if err != nil {
		return fmt.Errorf("could not create orchestrator client: %w", err)
	}
	defer func() { _ = driver.Close() }()

	// Wrap driver with caching if cache parameters are present
	driver, err = cache.WrapWithCaching(driver, driverConfig.Params, logger)
	if err != nil {
		return fmt.Errorf("could not initialize cache layer: %w", err)
	}

	storage, err := initStorage(c.Storage, runtimeID, logger)
	if err != nil {
		return fmt.Errorf("could not create sqlite client: %w", err)
	}
	defer func() { _ = storage.Close() }()

	js := runtime.NewJS(logger)

	opts := runtime.ExecuteOptions{
		Resume:     c.Resume,
		RunID:      c.RunID,
		PipelineID: runtimeID,
	}

	// If resuming but no RunID provided, use the runtime ID for consistency
	if c.Resume && opts.RunID == "" {
		opts.RunID = runtimeID
	}

	err = js.ExecuteWithOptions(ctx, pipeline, driver, storage, opts)
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
