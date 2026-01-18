package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jtarchie/ci/orchestra"
	storagelib "github.com/jtarchie/ci/storage"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

// ResumableRunner wraps PipelineRunner with state persistence and resume capability.
type ResumableRunner struct {
	runner    *PipelineRunner
	storage   storagelib.Driver
	state     *PipelineState
	logger    *slog.Logger
	ctx       context.Context //nolint: containedctx
	client    orchestra.Driver
	callIndex int // Tracks how many times Run() has been called this session
}

// ResumeOptions configures resume behavior.
type ResumeOptions struct {
	// RunID is the unique identifier for this pipeline run.
	// If resuming, this should match the previous run's ID.
	RunID string
	// Resume indicates whether to attempt resuming a previous run.
	Resume bool
}

// NewResumableRunner creates a new resumable runner.
func NewResumableRunner(
	ctx context.Context,
	client orchestra.Driver,
	store storagelib.Driver,
	logger *slog.Logger,
	opts ResumeOptions,
) (*ResumableRunner, error) {
	runner := NewPipelineRunner(ctx, client, logger)
	runID := opts.RunID
	if runID == "" {
		runID = gonanoid.Must()
	}

	resumableLogger := logger.WithGroup("resumable.runner").With("runID", runID)

	r := &ResumableRunner{
		runner:  runner,
		storage: store,
		logger:  resumableLogger,
		ctx:     ctx,
		client:  client,
	}

	// Try to load existing state if resuming
	if opts.Resume {
		state, err := r.loadState(runID)
		if err != nil && !errors.Is(err, storagelib.ErrNotFound) {
			return nil, fmt.Errorf("could not load pipeline state: %w", err)
		}
		if state != nil {
			r.state = state
			resumableLogger.Info("resume.loaded_state",
				"stepCount", len(state.Steps),
				"inProgress", len(state.InProgressSteps()),
			)
		}
	}

	// Create new state if not resuming or no state found
	if r.state == nil {
		r.state = NewPipelineState(runID, opts.Resume)
	}

	return r, nil
}

const stateStoragePrefix = "_resume/state"

// loadState loads pipeline state from storage.
func (r *ResumableRunner) loadState(runID string) (*PipelineState, error) {
	payload, err := r.storage.Get(r.ctx, stateStoragePrefix+"/"+runID)
	if err != nil {
		return nil, err
	}

	// Serialize payload to JSON and back to PipelineState
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("could not marshal payload: %w", err)
	}

	var state PipelineState
	if err := json.Unmarshal(jsonBytes, &state); err != nil {
		return nil, fmt.Errorf("could not unmarshal state: %w", err)
	}

	return &state, nil
}

// saveState persists the current pipeline state.
func (r *ResumableRunner) saveState() error {
	return r.storage.Set(r.ctx, stateStoragePrefix+"/"+r.state.RunID, r.state)
}

// Run executes a task with resume support.
// If the task was previously completed, returns the cached result.
// If the task was in progress and the container is still running, reattaches.
// Otherwise, starts a new container.
func (r *ResumableRunner) Run(input RunInput) (*RunResult, error) {
	// First, try to find an existing step by looking at steps in order
	// This allows for resuming even if step names are the same
	stepID := r.findOrGenerateStepID(input)

	// Check if this step already exists in state
	existingStep := r.state.GetStep(stepID)

	if existingStep != nil {
		// Handle completed step - skip and return cached result
		if existingStep.CanSkip() {
			r.logger.Info("resume.skip_completed", "stepID", stepID, "name", input.Name)
			return existingStep.Result, nil
		}

		// Handle step that was in progress - try to reattach
		if existingStep.IsResumable() {
			r.logger.Info("resume.reattach_attempt", "stepID", stepID, "containerID", existingStep.ContainerID)
			result, err := r.reattachToContainer(existingStep)
			if err == nil {
				return result, nil
			}
			r.logger.Warn("resume.reattach_failed", "stepID", stepID, "err", err)
			// Fall through to run new container
		}
	}

	// Run the step fresh
	return r.runStep(stepID, input)
}

// findOrGenerateStepID finds an existing step ID or generates a new one.
// For resuming, we need to match steps by their position in the pipeline.
func (r *ResumableRunner) findOrGenerateStepID(input RunInput) string {
	sanitizedName := sanitizeName(input.Name)
	stepID := fmt.Sprintf("%d-%s", r.callIndex, sanitizedName)
	r.callIndex++ // Increment for next call
	return stepID
}

// sanitizeName makes a name safe for use in IDs.
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	// Limit length
	if len(name) > 32 {
		name = name[:32]
	}
	return name
}

// runStep executes a step and persists its state.
func (r *ResumableRunner) runStep(stepID string, input RunInput) (*RunResult, error) {
	now := time.Now()
	ctx := r.ctx

	// Handle timeout
	if input.Timeout != "" {
		timeout, err := time.ParseDuration(input.Timeout)
		if err != nil {
			return nil, fmt.Errorf("could not parse timeout: %w", err)
		}

		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
	}

	// Create task ID for container tracking
	taskID := gonanoid.Must()

	// Create and persist step state as running
	step := &StepState{
		StepID:    stepID,
		Name:      input.Name,
		Status:    StepStatusRunning,
		TaskID:    taskID,
		StartedAt: &now,
	}
	r.state.SetStep(step)
	if err := r.saveState(); err != nil {
		r.logger.Error("resume.save_state_failed", "stepID", stepID, "err", err)
	}

	// Build mounts
	var mounts orchestra.Mounts
	for path, volume := range input.Mounts {
		mounts = append(mounts, orchestra.Mount{
			Name: volume.Name,
			Path: path,
		})
	}

	// Build command
	command := []string{input.Command.Path}
	command = append(command, input.Command.Args...)

	// Only create stdin reader if there's actual content
	var stdinReader io.Reader
	if input.Stdin != "" {
		stdinReader = strings.NewReader(input.Stdin)
	}

	// Run the container
	container, err := r.client.RunContainer(
		ctx,
		orchestra.Task{
			Command: command,
			ContainerLimits: orchestra.ContainerLimits{
				CPU:    input.ContainerLimits.CPU,
				Memory: input.ContainerLimits.Memory,
			},
			Env:        input.Env,
			ID:         fmt.Sprintf("%s-%s", input.Name, taskID),
			Image:      input.Image,
			Mounts:     mounts,
			Privileged: input.Privileged,
			Stdin:      stdinReader,
			User:       input.Command.User,
		},
	)
	if err != nil {
		r.logger.Error("container.run.failed", "err", err)

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			step.Status = StepStatusAborted
			_ = r.saveState()
			return &RunResult{Status: RunAbort}, nil
		}

		step.Status = StepStatusFailed
		step.Error = err.Error()
		_ = r.saveState()
		return nil, fmt.Errorf("could not run container: %w", err)
	}

	// Update step with container ID for potential reattachment
	step.ContainerID = container.ID()
	if err := r.saveState(); err != nil {
		r.logger.Error("resume.save.failed", "stepID", stepID, "err", err)
	}

	// Wait for container to complete
	var containerStatus orchestra.ContainerStatus
	for {
		containerStatus, err = container.Status(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				step.Status = StepStatusAborted
				_ = r.saveState()
				return &RunResult{Status: RunAbort}, nil
			}
			step.Status = StepStatusFailed
			step.Error = err.Error()
			_ = r.saveState()
			return nil, fmt.Errorf("could not get container status: %w", err)
		}

		if containerStatus.IsDone() {
			break
		}
	}

	// Get logs
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	err = container.Logs(ctx, stdout, stderr)
	if err != nil {
		r.logger.Error("container.logs.failed", "err", err)

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			step.Status = StepStatusAborted
			_ = r.saveState()
			return &RunResult{Status: RunAbort}, nil
		}

		step.Status = StepStatusFailed
		step.Error = err.Error()
		_ = r.saveState()
		return nil, fmt.Errorf("could not get container logs: %w", err)
	}

	// Build result
	result := &RunResult{
		Status: RunComplete,
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   containerStatus.ExitCode(),
	}

	// Update step state
	completedAt := time.Now()
	step.CompletedAt = &completedAt
	step.Status = StepStatusCompleted
	step.Result = result
	if result.Code != 0 {
		step.ExitCode = &result.Code
	}

	if err := r.saveState(); err != nil {
		r.logger.Error("resume.save_state_failed", "stepID", stepID, "err", err)
	}

	// Clean up container
	if cleanupErr := container.Cleanup(ctx); cleanupErr != nil {
		r.logger.Error("container.cleanup", "err", cleanupErr)
	}

	return result, nil
}

// reattachToContainer attempts to reattach to an existing container.
func (r *ResumableRunner) reattachToContainer(step *StepState) (*RunResult, error) {
	container, err := r.client.GetContainer(r.ctx, step.ContainerID)
	if err != nil {
		if errors.Is(err, orchestra.ErrContainerNotFound) {
			return nil, fmt.Errorf("container no longer exists: %w", err)
		}
		return nil, fmt.Errorf("could not get container: %w", err)
	}

	// Wait for container to complete
	var containerStatus orchestra.ContainerStatus
	for {
		containerStatus, err = container.Status(r.ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return &RunResult{Status: RunAbort}, nil
			}
			return nil, fmt.Errorf("could not get container status: %w", err)
		}

		if containerStatus.IsDone() {
			break
		}
	}

	// Get logs
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	err = container.Logs(r.ctx, stdout, stderr)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return &RunResult{Status: RunAbort}, nil
		}
		return nil, fmt.Errorf("could not get container logs: %w", err)
	}

	// Update step state
	completedAt := time.Now()
	step.CompletedAt = &completedAt
	exitCode := containerStatus.ExitCode()
	step.ExitCode = &exitCode
	step.Status = StepStatusCompleted
	step.Result = &RunResult{
		Status: RunComplete,
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   exitCode,
	}

	if err := r.saveState(); err != nil {
		r.logger.Error("resume.save.failed", "stepID", step.StepID, "err", err)
	}

	// Clean up container
	if err := container.Cleanup(r.ctx); err != nil {
		r.logger.Error("resume.container.cleanup.failed", "stepID", step.StepID, "err", err)
	}

	return step.Result, nil
}

// CreateVolume creates a volume (passthrough to underlying runner).
func (r *ResumableRunner) CreateVolume(input VolumeInput) (*VolumeResult, error) {
	return r.runner.CreateVolume(input)
}

// CleanupVolumes cleans up all tracked volumes (passthrough to underlying runner).
func (r *ResumableRunner) CleanupVolumes() error {
	return r.runner.CleanupVolumes()
}

// State returns the current pipeline state.
func (r *ResumableRunner) State() *PipelineState {
	return r.state
}
