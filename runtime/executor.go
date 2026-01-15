package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/orchestra/cache"
	"github.com/jtarchie/ci/storage"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

// ExecutorOptions configures pipeline execution.
type ExecutorOptions struct {
	// Resume enables resume mode for the pipeline.
	Resume bool
	// RunID is the unique identifier for this pipeline run.
	// If resuming, this should match the previous run's ID.
	RunID string
	// PipelineID is the unique identifier for this pipeline.
	// Used to scope resource versions to a specific pipeline.
	PipelineID string
}

// ExecutePipeline executes a pipeline with the given content and driver DSN.
// It handles driver initialization, execution, and cleanup.
func ExecutePipeline(
	ctx context.Context,
	content string,
	driverDSN string,
	store storage.Driver,
	logger *slog.Logger,
	opts ExecutorOptions,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	// Generate a namespace for this execution
	namespace := "ci-" + gonanoid.Must()
	if opts.RunID != "" {
		namespace = "ci-" + opts.RunID
	}

	logger = logger.WithGroup("executor").With(
		"namespace", namespace,
		"driver", driverDSN,
	)

	logger.Info("initializing driver")

	driverConfig, orchestrator, err := orchestra.GetFromDSN(driverDSN)
	if err != nil {
		return fmt.Errorf("could not parse driver DSN (%q): %w", driverDSN, err)
	}

	// Use namespace from DSN if provided
	if driverConfig.Namespace != "" {
		namespace = driverConfig.Namespace
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

	logger.Info("executing pipeline")

	js := NewJS(logger)

	executeOpts := ExecuteOptions(opts)

	err = js.ExecuteWithOptions(ctx, content, driver, store, executeOpts)
	if err != nil {
		return fmt.Errorf("could not execute pipeline: %w", err)
	}

	logger.Info("pipeline completed successfully")

	return nil
}
