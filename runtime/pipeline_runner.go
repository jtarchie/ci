package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jtarchie/ci/orchestra"
)

type PipelineRunner struct {
	client orchestra.Driver
	ctx    context.Context
	logger *slog.Logger
}

func NewPipelineRunner(
	client orchestra.Driver,
	ctx context.Context,
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
	logger := c.logger
	logger.Debug("volume.create", "input", input)

	volume, err := c.client.CreateVolume(c.ctx, input.Name, input.Size)
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

	Message string `json:"error"`
	Status  string `json:"status"`
}

type RunInput struct {
	Command []string                `json:"command"`
	Env     map[string]string       `json:"env"`
	Image   string                  `json:"image"`
	Mounts  map[string]VolumeResult `json:"mounts"`
	Name    string                  `json:"name"`
	Stdin   string                  `json:"stdin"`
}

func (c *PipelineRunner) Run(input RunInput) *RunResult {
	taskID, err := uuid.NewV7()
	if err != nil {
		return &RunResult{
			Status:  "error",
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
		c.ctx,
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
		logger.Error("container.run", "err", err)

		return &RunResult{
			Status:  "error",
			Message: fmt.Sprintf("could not run container: %s", err),
		}
	}

	var status orchestra.ContainerStatus

	for {
		var err error

		status, err = container.Status(c.ctx)
		if err != nil {
			return &RunResult{
				Status:  "error",
				Message: fmt.Sprintf("could not get container status: %s", err),
			}
		}

		if status.IsDone() {
			break
		}
	}

	logger.Debug("container.status", "exitCode", status.ExitCode())

	defer func() {
		err := container.Cleanup(c.ctx)
		if err != nil {
			logger.Error("container.cleanup", "err", err)
		}
	}()

	stdout, stderr := &strings.Builder{}, &strings.Builder{}

	err = container.Logs(c.ctx, stdout, stderr)
	if err != nil {
		logger.Error("container.logs", "err", err)

		return &RunResult{
			Code:    status.ExitCode(),
			Status:  "error",
			Message: fmt.Sprintf("could not get container logs: %s", err),
		}
	}

	return &RunResult{
		Status: "complete",
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   status.ExitCode(),
	}
}
