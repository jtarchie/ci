package server

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/storage"
)

// ExecutionService manages pipeline execution with concurrency limits.
type ExecutionService struct {
	store         storage.Driver
	logger        *slog.Logger
	maxInFlight   int
	inFlight      atomic.Int32
	mu            sync.Mutex
	wg            sync.WaitGroup
	DefaultDriver string
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

	if webhook != nil {
		opts.WebhookData = webhook.webhookData
		opts.ResponseChan = webhook.responseChan
	}

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
