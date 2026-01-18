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
	store       storage.Driver
	logger      *slog.Logger
	maxInFlight int
	inFlight    atomic.Int32
	mu          sync.Mutex
}

// NewExecutionService creates a new execution service.
func NewExecutionService(store storage.Driver, logger *slog.Logger, maxInFlight int) *ExecutionService {
	if maxInFlight <= 0 {
		maxInFlight = 10 // default limit
	}

	return &ExecutionService{
		store:       store,
		logger:      logger.WithGroup("executor.run"),
		maxInFlight: maxInFlight,
	}
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

	// Increment in-flight counter
	s.inFlight.Add(1)

	// Launch execution goroutine
	go s.executePipeline(pipeline, run)

	return run, nil
}

func (s *ExecutionService) executePipeline(pipeline *storage.Pipeline, run *storage.PipelineRun) {
	defer s.inFlight.Add(-1)

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
		logger.Error("run.update.failed", "error", err)
		return
	}

	logger.Info("pipeline.execute.start")

	// Determine driver DSN - use pipeline's if set, otherwise default to docker
	driverDSN := pipeline.DriverDSN
	if driverDSN == "" {
		driverDSN = "docker"
	}

	// Execute the pipeline
	opts := runtime.ExecutorOptions{
		RunID:      run.ID,
		PipelineID: pipeline.ID,
	}

	err = runtime.ExecutePipeline(ctx, pipeline.Content, driverDSN, s.store, logger, opts)
	if err != nil {
		logger.Error("pipeline.execute.failed", "error", err)

		updateErr := s.store.UpdateRunStatus(ctx, run.ID, storage.RunStatusFailed, err.Error())
		if updateErr != nil {
			logger.Error("run.update.failed", "error", updateErr)
		}

		return
	}

	// Update status to success
	err = s.store.UpdateRunStatus(ctx, run.ID, storage.RunStatusSuccess, "")
	if err != nil {
		logger.Error("run.update.failed", "error", err)
		return
	}

	logger.Info("pipeline.execute.success")
}
