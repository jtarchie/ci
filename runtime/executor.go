package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/orchestra/cache"
	"github.com/jtarchie/ci/secrets"
	"github.com/jtarchie/ci/storage"
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
	// Namespace is the namespace for this execution (internal use).
	Namespace string
	// WebhookData contains the incoming HTTP request when triggered via webhook.
	// Nil when not triggered via webhook.
	WebhookData *WebhookData
	// ResponseChan receives the HTTP response from the pipeline.
	// Nil when not triggered via webhook.
	ResponseChan chan *HTTPResponse
	// SecretsManager provides access to encrypted secrets.
	// If nil, secret resolution is disabled.
	SecretsManager secrets.Manager
	// DisableNotifications prevents the notify system from sending messages.
	DisableNotifications bool
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
	namespace := "ci-" + UniqueID()
	if opts.RunID != "" {
		namespace = "ci-" + opts.RunID
	}

	logger = logger.WithGroup("executor").With(
		"namespace", namespace,
		"driver", driverDSN,
	)

	logger.Info("driver.initialize")

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

	logger.Info("pipeline.executing")

	js := NewJS(logger)

	executeOpts := ExecuteOptions{
		Resume:               opts.Resume,
		RunID:                opts.RunID,
		PipelineID:           opts.PipelineID,
		Namespace:            namespace,
		WebhookData:          opts.WebhookData,
		ResponseChan:         opts.ResponseChan,
		SecretsManager:       opts.SecretsManager,
		DisableNotifications: opts.DisableNotifications,
	}

	err = js.ExecuteWithOptions(ctx, content, driver, store, executeOpts)
	if err != nil {
		return fmt.Errorf("could not execute pipeline: %w", err)
	}

	logger.Info("pipeline.completed.success")

	return nil
}
