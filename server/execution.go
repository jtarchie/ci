package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/orchestra/cache"
	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/secrets"
	"github.com/jtarchie/ci/storage"
)

// ExecutionService manages pipeline execution with concurrency limits.
type ExecutionService struct {
	store                 storage.Driver
	logger                *slog.Logger
	maxInFlight           int
	inFlight              atomic.Int32
	mu                    sync.Mutex
	wg                    sync.WaitGroup
	DefaultDriver         string
	SecretsManager        secrets.Manager
	AllowedFeatures       []Feature
	FetchTimeout          time.Duration
	FetchMaxResponseBytes int64
}

// NewExecutionService creates a new execution service.
// The allowedDrivers list determines the default driver (first in list).
// If allowedDrivers is empty or contains "*", defaults to "docker".
func NewExecutionService(store storage.Driver, logger *slog.Logger, maxInFlight int, allowedDrivers []string) *ExecutionService {
	if maxInFlight <= 0 {
		maxInFlight = 10 // default limit
	}

	// Determine default driver: first allowed driver, or "docker" if wildcard/empty
	defaultDriver := "docker"
	if len(allowedDrivers) > 0 && allowedDrivers[0] != "*" {
		defaultDriver = allowedDrivers[0]
	}

	return &ExecutionService{
		store:         store,
		logger:        logger.WithGroup("executor.run"),
		maxInFlight:   maxInFlight,
		DefaultDriver: defaultDriver,
	}
}

// Wait blocks until all in-flight pipeline executions have completed.
// This is useful for graceful shutdown or testing.
func (s *ExecutionService) Wait() {
	s.wg.Wait()
}

// CanExecute returns true if a new pipeline can be started.
func (s *ExecutionService) CanExecute() bool {
	return int(s.inFlight.Load()) < s.maxInFlight
}

// CurrentInFlight returns the number of currently running pipelines.
func (s *ExecutionService) CurrentInFlight() int {
	return int(s.inFlight.Load())
}

// MaxInFlight returns the maximum number of concurrent pipelines allowed.
func (s *ExecutionService) MaxInFlight() int {
	return s.maxInFlight
}

// TriggerPipeline starts a new pipeline execution asynchronously.
// It creates a run record, starts a goroutine to execute the pipeline,
// and returns the run ID immediately.
func (s *ExecutionService) TriggerPipeline(ctx context.Context, pipeline *storage.Pipeline) (*storage.PipelineRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create run record with queued status
	run, err := s.store.SaveRun(ctx, pipeline.ID)
	if err != nil {
		return nil, err
	}

	// Increment in-flight counter and WaitGroup
	s.inFlight.Add(1)
	s.wg.Add(1)

	// Launch execution goroutine
	go s.executePipeline(pipeline, run, nil)

	return run, nil
}

// TriggerWebhookPipeline starts a new pipeline execution triggered by a webhook.
// It passes webhook request data and a response channel through to the pipeline runtime.
func (s *ExecutionService) TriggerWebhookPipeline(
	ctx context.Context,
	pipeline *storage.Pipeline,
	webhookData *runtime.WebhookData,
	responseChan chan *runtime.HTTPResponse,
) (*storage.PipelineRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create run record with queued status
	run, err := s.store.SaveRun(ctx, pipeline.ID)
	if err != nil {
		return nil, err
	}

	// Increment in-flight counter and WaitGroup
	s.inFlight.Add(1)
	s.wg.Add(1)

	// Launch execution goroutine with webhook data
	go s.executePipeline(pipeline, run, &webhookExecData{
		webhookData:  webhookData,
		responseChan: responseChan,
	})

	return run, nil
}

// webhookExecData holds webhook-specific execution data.
type webhookExecData struct {
	webhookData  *runtime.WebhookData
	responseChan chan *runtime.HTTPResponse
}

func (s *ExecutionService) executePipeline(pipeline *storage.Pipeline, run *storage.PipelineRun, webhook *webhookExecData) {
	defer s.inFlight.Add(-1)
	defer s.wg.Done()

	ctx := context.Background()
	logger := s.logger.With(
		"event", "pipeline.execute",
		"run_id", run.ID,
		"pipeline_id", pipeline.ID,
		"pipeline_name", pipeline.Name,
	)

	// Update status to running
	err := s.store.UpdateRunStatus(ctx, run.ID, storage.RunStatusRunning, "")
	if err != nil {
		logger.Error("run.update.failed.to_running", "error", err)
		return
	}

	logger.Info("pipeline.execute.start")

	// Determine driver DSN - use pipeline's if set, otherwise use default
	driverDSN := pipeline.DriverDSN
	if driverDSN == "" {
		driverDSN = s.DefaultDriver
	}

	// Execute the pipeline
	opts := runtime.ExecutorOptions{
		RunID:      run.ID,
		PipelineID: pipeline.ID,
	}

	// Only pass secrets manager if the secrets feature is enabled
	if IsFeatureEnabled(FeatureSecrets, s.AllowedFeatures) {
		opts.SecretsManager = s.SecretsManager
	}

	// Only pass webhook data if the webhooks feature is enabled
	if webhook != nil && IsFeatureEnabled(FeatureWebhooks, s.AllowedFeatures) {
		opts.WebhookData = webhook.webhookData
		opts.ResponseChan = webhook.responseChan
	}

	// Disable notifications if the feature is not enabled
	opts.DisableNotifications = !IsFeatureEnabled(FeatureNotifications, s.AllowedFeatures)

	// Disable fetch if the feature is not enabled
	opts.DisableFetch = !IsFeatureEnabled(FeatureFetch, s.AllowedFeatures)
	opts.FetchTimeout = s.FetchTimeout
	opts.FetchMaxResponseBytes = s.FetchMaxResponseBytes

	err = runtime.ExecutePipeline(ctx, pipeline.Content, driverDSN, s.store, logger, opts)
	if err != nil {
		logger.Error("pipeline.execute.failed", "error", err)

		updateErr := s.store.UpdateRunStatus(ctx, run.ID, storage.RunStatusFailed, err.Error())
		if updateErr != nil {
			logger.Error("run.update.failed.to_failed", "error", updateErr)
		}

		return
	}

	// Check if any jobs failed by querying job statuses
	finalStatus := s.determineRunStatus(ctx, run.ID, logger)

	err = s.store.UpdateRunStatus(ctx, run.ID, finalStatus, "")
	if err != nil {
		logger.Error("run.update.failed.to_final", "error", err)
		return
	}

	if finalStatus == storage.RunStatusSuccess {
		logger.Info("pipeline.execute.success")
	} else {
		logger.Info("pipeline.execute.completed_with_failures")
	}
}

// RunByNameSync executes a stored pipeline by name, synchronously.
// It writes SSE events (stdout, stderr lines as data events; an exit event at completion)
// to the provided http.ResponseWriter.
//
// The pipeline is looked up by exact name; ErrNotFound is returned if missing.
// Args are passed to the pipeline via pipelineContext.args.
//
// If workdirTar is non-nil, a "workdir" volume is created and seeded from the
// tar stream *before* the SSE response starts. This ensures the HTTP request
// body is fully consumed while the connection is still in request mode, which
// is required for correct behaviour through reverse proxies.
func (s *ExecutionService) RunByNameSync(
	ctx context.Context,
	name string,
	args []string,
	workdirTar io.Reader,
	w http.ResponseWriter,
) error {
	pipeline, err := s.store.GetPipelineByName(ctx, name)
	if err != nil {
		return err
	}

	run, err := s.store.SaveRun(ctx, pipeline.ID)
	if err != nil {
		return fmt.Errorf("failed to save run: %w", err)
	}

	if err = s.store.UpdateRunStatus(ctx, run.ID, storage.RunStatusRunning, ""); err != nil {
		s.logger.Error("run.update.failed.to_running", "error", err)
	}

	driverDSN := pipeline.DriverDSN
	if driverDSN == "" {
		driverDSN = s.DefaultDriver
	}

	// --- Pre-seed workdir volume (consumes HTTP body before SSE starts) ---
	var preseededVolumes map[string]orchestra.Volume
	var driver orchestra.Driver

	if workdirTar != nil {
		namespace := "ci-" + run.ID

		driverConfig, orchestrator, dErr := orchestra.GetFromDSN(driverDSN)
		if dErr != nil {
			return fmt.Errorf("could not parse driver DSN: %w", dErr)
		}

		if driverConfig.Namespace != "" {
			namespace = driverConfig.Namespace
		}

		driver, dErr = orchestrator(namespace, s.logger, driverConfig.Params)
		if dErr != nil {
			return fmt.Errorf("could not create driver: %w", dErr)
		}

		driver, dErr = cache.WrapWithCaching(driver, driverConfig.Params, s.logger)
		if dErr != nil {
			_ = driver.Close()
			return fmt.Errorf("could not init cache layer: %w", dErr)
		}

		vol, vErr := driver.CreateVolume(ctx, "workdir", 0)
		if vErr != nil {
			_ = driver.Close()
			return fmt.Errorf("could not create workdir volume: %w", vErr)
		}

		accessor, ok := driver.(cache.VolumeDataAccessor)
		if !ok {
			_ = vol.Cleanup(ctx)
			_ = driver.Close()
			return fmt.Errorf("driver %q does not support volume data access", driver.Name())
		}

		s.logger.Info("workdir.preseed.start")

		if cErr := accessor.CopyToVolume(ctx, vol.Name(), workdirTar); cErr != nil {
			_ = vol.Cleanup(ctx)
			_ = driver.Close()
			return fmt.Errorf("could not seed workdir volume: %w", cErr)
		}

		s.logger.Info("workdir.preseed.done")

		preseededVolumes = map[string]orchestra.Volume{"workdir": vol}
		// Close the driver after RunByNameSync returns (after ExecutePipeline
		// completes). ExecutePipeline reuses this driver instance via opts.Driver
		// and skips creating/closing its own.
		defer func() { _ = driver.Close() }()
	}

	// --- SSE headers — only written after the request body is consumed ---
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	opts := runtime.ExecutorOptions{
		RunID:                 run.ID,
		PipelineID:            pipeline.ID,
		Args:                  args,
		PreseededVolumes:      preseededVolumes,
		Driver:                driver,
		DisableNotifications:  !IsFeatureEnabled(FeatureNotifications, s.AllowedFeatures),
		DisableFetch:          !IsFeatureEnabled(FeatureFetch, s.AllowedFeatures),
		FetchTimeout:          s.FetchTimeout,
		FetchMaxResponseBytes: s.FetchMaxResponseBytes,
	}
	if IsFeatureEnabled(FeatureSecrets, s.AllowedFeatures) {
		opts.SecretsManager = s.SecretsManager
	}

	// Stream stdout/stderr as SSE data events while the pipeline runs.
	var writeMu sync.Mutex
	opts.OutputCallback = func(stream, data string) {
		writeMu.Lock()
		defer writeMu.Unlock()

		evt, _ := json.Marshal(map[string]string{"stream": stream, "data": data})
		fmt.Fprintf(w, "data: %s\n\n", evt) //nolint:errcheck

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	execErr := runtime.ExecutePipeline(ctx, pipeline.Content, driverDSN, s.store, s.logger, opts)

	exitCode := 0
	finalStatus := storage.RunStatusSuccess
	errMsg := ""

	if execErr != nil {
		exitCode = 1
		finalStatus = storage.RunStatusFailed
		// TODO: we never display this error message anywhere in the UI - consider surfacing it in the run details page or similar
		errMsg = execErr.Error()
	}

	if err = s.store.UpdateRunStatus(ctx, run.ID, finalStatus, errMsg); err != nil {
		s.logger.Error("run.update.failed.to_final", "error", err)
	}

	// Write SSE exit event.
	exitEvent := map[string]any{"event": "exit", "code": exitCode, "run_id": run.ID}
	if errMsg != "" {
		exitEvent["message"] = errMsg
	}

	exitData, _ := json.Marshal(exitEvent)
	fmt.Fprintf(w, "data: %s\n\n", exitData) //nolint:errcheck
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	return nil
}

// determineRunStatus checks job statuses to determine the final run status.
func (s *ExecutionService) determineRunStatus(ctx context.Context, runID string, logger *slog.Logger) storage.RunStatus {
	// Query all job statuses for this run (backwards-compat Concourse YAML pipelines).
	// Note: TypeScript pipeline task statuses under /pipeline/{runID}/tasks/ are NOT
	// checked here because individual task failures don't necessarily mean the pipeline
	// failed — the pipeline may handle errors (e.g., try/catch). Pipeline-level failure
	// is already handled by the executePipeline error return path.
	prefix := "/pipeline/" + runID + "/jobs"
	results, err := s.store.GetAll(ctx, prefix, []string{"status"})
	if err != nil {
		logger.Warn("failed to query job statuses, assuming success", "error", err)
		return storage.RunStatusSuccess
	}

	// Check if any job has a failed/error status
	for _, result := range results {
		if status, ok := result.Payload["status"].(string); ok {
			switch status {
			case "failure", "error", "abort":
				return storage.RunStatusFailed
			}
		}
	}

	return storage.RunStatusSuccess
}
