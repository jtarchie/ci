package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/storage"
)

type PipelineRunner struct {
	client    orchestra.Driver
	storage   storage.Driver
	ctx       context.Context //nolint: containedctx
	logger    *slog.Logger
	volumes   []orchestra.Volume
	namespace string
	runID     string
	mu        sync.Mutex // Protects callIndex
	callIndex int        // Tracks how many times Run() has been called
}

func NewPipelineRunner(
	ctx context.Context,
	client orchestra.Driver,
	storageClient storage.Driver,
	logger *slog.Logger,
	namespace string,
	runID string,
) *PipelineRunner {
	return &PipelineRunner{
		client:    client,
		storage:   storageClient,
		ctx:       ctx,
		logger:    logger.WithGroup("pipeline.run"),
		volumes:   []orchestra.Volume{},
		namespace: namespace,
		runID:     runID,
	}
}

type VolumeInput struct {
	Name string `json:"name"`
	Size int    `json:"size"`
}

type VolumeResult struct {
	volume orchestra.Volume
	Name   string `json:"name"`
	Path   string `json:"path"`
}

func (c *PipelineRunner) CreateVolume(input VolumeInput) (*VolumeResult, error) {
	ctx := c.ctx

	logger := c.logger
	logger.Debug("volume.create.pipeline.request", "input", input)

	volume, err := c.client.CreateVolume(ctx, input.Name, input.Size)
	if err != nil {
		logger.Error("volume.create.pipeline.error", "err", err)

		return nil, fmt.Errorf("could not create volume: %w", err)
	}

	// Track volume for cleanup
	c.volumes = append(c.volumes, volume)

	return &VolumeResult{
		volume: volume,
		Name:   volume.Name(),
		Path:   volume.Path(),
	}, nil
}

type RunResult struct {
	Code   int    `json:"code"`
	Stderr string `json:"stderr"`
	Stdout string `json:"stdout"`

	Status RunStatus `json:"status"`
}

// OutputCallback is called with streaming output chunks.
// stream is either "stdout" or "stderr", data is the output chunk.
type OutputCallback func(stream string, data string)

type RunInput struct {
	Command struct {
		Path string   `json:"path"`
		Args []string `json:"args"`
		User string   `json:"user"`
	} `json:"command"`
	ContainerLimits struct {
		CPU    int64 `json:"cpu"`
		Memory int64 `json:"memory"`
	} `json:"container_limits"`
	Env        map[string]string       `json:"env"`
	Image      string                  `json:"image"`
	Mounts     map[string]VolumeResult `json:"mounts"`
	Name       string                  `json:"name"`
	Privileged bool                    `json:"privileged"`
	Stdin      string                  `json:"stdin"`
	// OnOutput is called with streaming output chunks as the container runs.
	// If provided, the callback receives (stream, data) where stream is "stdout" or "stderr".
	OnOutput OutputCallback `json:"-"` // Not serialized from JS, set programmatically
	// has to be string because goja doesn't support string -> time.Duration
	Timeout string `json:"timeout"`
}

type RunStatus string

const (
	RunAbort    RunStatus = "abort"
	RunComplete RunStatus = "complete"
)

func (c *PipelineRunner) Run(input RunInput) (*RunResult, error) {
	ctx := c.ctx
	logger := c.logger

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

	// Create deterministic task ID for consistent container naming
	// Use callIndex to ensure uniqueness even when task names are reused
	c.mu.Lock()
	stepID := fmt.Sprintf("%d-%s", c.callIndex, input.Name)
	c.callIndex++
	c.mu.Unlock()
	taskID := DeterministicTaskID(c.namespace, c.runID, stepID, input.Name)

	logger = c.logger.With("task.id", taskID, "task.name", input.Name, "task.privileged", input.Privileged)

	logger.Debug("container.run.start")

	var mounts orchestra.Mounts
	for path, volume := range input.Mounts {
		mounts = append(mounts, orchestra.Mount{
			Name: volume.Name,
			Path: path,
		})
	}

	logger.Debug("container.run.mounts", "mounts", mounts)

	command := []string{input.Command.Path}
	command = append(command, input.Command.Args...)

	// Only create stdin reader if there's actual content
	var stdinReader io.Reader
	if input.Stdin != "" {
		stdinReader = strings.NewReader(input.Stdin)
	}

	container, err := c.client.RunContainer(
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
		logger.Error("container.run.create_error", "err", err)

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return &RunResult{Status: RunAbort}, nil
		}

		return nil, fmt.Errorf("could not run container: %w", err)
	}

	var containerStatus orchestra.ContainerStatus

	// Create a streaming writer that calls the callback
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	var streamWg sync.WaitGroup

	// Start streaming logs if callback is provided
	if input.OnOutput != nil {
		streamWg.Add(1)

		go func() {
			defer streamWg.Done()
			c.streamLogsWithCallback(streamCtx, container, input.OnOutput, stdout, stderr)
		}()
	}

	// Wait for container to complete
	for {
		var err error

		containerStatus, err = container.Status(ctx)
		if err != nil {
			cancelStream()

			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return &RunResult{Status: RunAbort}, nil
			}

			return nil, fmt.Errorf("could not get container status: %w", err)
		}

		if containerStatus.IsDone() {
			break
		}

		// Small sleep to avoid busy loop
		time.Sleep(100 * time.Millisecond)
	}

	// Cancel streaming and wait for it to finish
	cancelStream()
	streamWg.Wait()

	logger.Debug("container.status", "exitCode", containerStatus.ExitCode())

	defer func() {
		err := container.Cleanup(ctx)
		if err != nil {
			logger.Error("container.cleanup.error", "err", err)
		}
	}()

	// Get final logs (if we weren't streaming, or to ensure we have complete output)
	if input.OnOutput == nil {
		err = container.Logs(ctx, stdout, stderr, false)
		if err != nil {
			logger.Error("container.logs.error", "err", err)

			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return &RunResult{Status: RunAbort}, nil
			}

			return nil, fmt.Errorf("could not get container logs: %w", err)
		}
	}

	logger.Debug("container.logs", "stdout", stdout.String(), "stderr", stderr.String())

	return &RunResult{
		Status: RunComplete,
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   containerStatus.ExitCode(),
	}, nil
}

// streamLogsWithCallback streams container logs and invokes the callback with each chunk.
func (c *PipelineRunner) streamLogsWithCallback(
	ctx context.Context,
	container orchestra.Container,
	callback OutputCallback,
	stdout, stderr *strings.Builder,
) {
	logger := c.logger

	// Use a pipe to capture streaming output
	pr, pw := io.Pipe()

	var streamWg sync.WaitGroup

	streamWg.Add(1)

	go func() {
		defer streamWg.Done()
		defer func() { _ = pw.Close() }()

		err := container.Logs(ctx, pw, io.Discard, true)
		if err != nil && ctx.Err() == nil {
			logger.Debug("container.streamLogs.error", "err", err)
		}
	}()

	// Read from pipe in chunks and invoke callback
	buf := make([]byte, 4096)

	for {
		n, err := pr.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			stdout.WriteString(chunk)

			// Invoke callback with the chunk
			// Only invoke if stream context is still active
			if ctx.Err() == nil {
				callback("stdout", chunk)
			}
		}

		if err != nil {
			if err != io.EOF && ctx.Err() == nil {
				logger.Debug("container.streamLogs.read.error", "err", err)
			}

			break
		}
	}

	streamWg.Wait()
}

// CleanupVolumes cleans up all tracked volumes.
// This triggers cache persistence for CachingVolume wrappers.
func (c *PipelineRunner) CleanupVolumes() error {
	logger := c.logger
	ctx := c.ctx

	var errs []error

	for _, volume := range c.volumes {
		logger.Debug("volume.cleanup", "name", volume.Name())

		err := volume.Cleanup(ctx)
		if err != nil {
			logger.Error("volume.cleanup.error", "name", volume.Name(), "err", err)
			errs = append(errs, err)
		}
	}

	// Clear the slice
	c.volumes = nil

	if len(errs) > 0 {
		return fmt.Errorf("failed to cleanup %d volumes: %w", len(errs), errors.Join(errs...))
	}

	return nil
}
