package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jtarchie/ci/orchestra"
)

type PipelineRunner struct {
	client orchestra.Driver
	ctx    context.Context
	logger *slog.Logger
}

func NewPipelineRunner(
	ctx context.Context,
	client orchestra.Driver,
	logger *slog.Logger,
) *PipelineRunner {
	return &PipelineRunner{
		client: client,
		ctx:    ctx,
		logger: logger.WithGroup("pipeline.runner"),
	}
}

type VolumeInput struct {
	Name string `json:"name"`
	Size int    `json:"size"`
}

type VolumeResult struct {
	orchestra.Volume `json:"volume,omitempty"`
	Error            string `json:"error,omitempty"`
}

func (c *PipelineRunner) CreateVolume(input VolumeInput) *VolumeResult {
	ctx := c.ctx

	logger := c.logger
	logger.Debug("volume.create", "input", input)

	volume, err := c.client.CreateVolume(ctx, input.Name, input.Size)
	if err != nil {
		return &VolumeResult{
			Error: fmt.Sprintf("could not create volume: %s", err),
		}
	}

	return &VolumeResult{
		Volume: volume,
	}
}

type RunResult struct {
	Code   int    `json:"code"`
	Stderr string `json:"stderr"`
	Stdout string `json:"stdout"`

	Message string    `json:"message"`
	Status  RunStatus `json:"status"`
}

type RunInput struct {
	Command []string                `json:"command"`
	Env     map[string]string       `json:"env"`
	Image   string                  `json:"image"`
	Mounts  map[string]VolumeResult `json:"mounts"`
	Name    string                  `json:"name"`
	Stdin   string                  `json:"stdin"`
	// has to be string because goja doesn't support string -> time.Duration
	Timeout string `json:"timeout"`
}

type RunStatus string

const (
	RunError    RunStatus = "error"
	RunAbort    RunStatus = "abort"
	RunComplete RunStatus = "complete"
)

func (c *PipelineRunner) Run(input RunInput) *RunResult {
	ctx := c.ctx

	slog.Info("pipeline.run", "input", input)

	if input.Timeout != "" {
		timeout, err := time.ParseDuration(input.Timeout)
		if err != nil {
			return &RunResult{
				Status:  RunError,
				Message: fmt.Sprintf("could not parse timeout: %s", err),
			}
		}

		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
	}

	taskID, err := uuid.NewV7()
	if err != nil {
		return &RunResult{
			Status:  RunError,
			Message: fmt.Sprintf("could not generate uuid: %s", err),
		}
	}

	logger := c.logger.With("taskID", taskID)

	logger.Debug("container.run", "input", input)

	var mounts orchestra.Mounts
	for path, volume := range input.Mounts {
		mounts = append(mounts, orchestra.Mount{
			Name: volume.Name(),
			Path: path,
		})
	}

	logger.Debug("container.run", "mounts", mounts)

	container, err := c.client.RunContainer(
		ctx,
		orchestra.Task{
			Command: input.Command,
			Env:     input.Env,
			ID:      fmt.Sprintf("%s-%s", input.Name, taskID.String()),
			Image:   input.Image,
			Mounts:  mounts,
			Stdin:   strings.NewReader(input.Stdin),
		},
	)
	if err != nil {
		status := RunError

		logger.Error("container.run", "err", err)

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = RunAbort
		}

		return &RunResult{
			Status:  status,
			Message: fmt.Sprintf("could not run container: %s", err),
		}
	}

	var containerStatus orchestra.ContainerStatus

	for {
		var err error

		containerStatus, err = container.Status(ctx)
		if err != nil {
			status := RunError

			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				status = RunAbort
			}

			return &RunResult{
				Status:  status,
				Message: fmt.Sprintf("could not get container status: %s", err),
			}
		}

		if containerStatus.IsDone() {
			break
		}
	}

	logger.Debug("container.status", "exitCode", containerStatus.ExitCode())

	defer func() {
		err := container.Cleanup(ctx)
		if err != nil {
			logger.Error("container.cleanup", "err", err)
		}
	}()

	stdout, stderr := &strings.Builder{}, &strings.Builder{}

	err = container.Logs(ctx, stdout, stderr)
	if err != nil {
		logger.Error("container.logs", "err", err)

		status := RunError
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = RunAbort
		}

		return &RunResult{
			Code:    containerStatus.ExitCode(),
			Status:  status,
			Message: fmt.Sprintf("could not get container logs: %s", err),
		}
	}

	return &RunResult{
		Status: RunComplete,
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   containerStatus.ExitCode(),
	}
}
