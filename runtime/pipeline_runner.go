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
	log    *slog.Logger
	client orchestra.Driver
}

func NewPipelineRunner(
	client orchestra.Driver,
) *PipelineRunner {
	return &PipelineRunner{
		log:    slog.Default().WithGroup("pipeline.runner").With("orchestrator", client.Name()),
		client: client,
	}
}

type VolumeInput struct {
	Name string `json:"name"`
	Size int    `json:"size"`
}

type VolumeResult struct {
	orchestra.Volume
	Error string `json:"error"`
}

func (c *PipelineRunner) CreateVolume(input VolumeInput) *VolumeResult {
	ctx := context.Background()

	logger := c.log
	logger.Info("volume.create", "input", input)

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
	Error  string `json:"error"`
	Stderr string `json:"stderr"`
	Stdout string `json:"stdout"`
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
	ctx := context.Background()

	taskID, err := uuid.NewV7()
	if err != nil {
		return &RunResult{
			Code:  1,
			Error: fmt.Sprintf("could not generate uuid: %s", err),
		}
	}

	logger := c.log.With("id", taskID)

	logger.Info("container.run", "input", input)

	var mounts orchestra.Mounts
	for path, volume := range input.Mounts {
		mounts = append(mounts, orchestra.Mount{
			Name: volume.Name(),
			Path: path,
		})
	}

	logger.Info("container.run", "mounts", mounts)

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
		logger.Error("container.run", "err", err)

		return &RunResult{
			Code:  1,
			Error: fmt.Sprintf("could not run container: %s", err),
		}
	}

	var status orchestra.ContainerStatus

	for {
		var err error

		status, err = container.Status(ctx)
		if err != nil {
			return &RunResult{
				Code:  1,
				Error: fmt.Sprintf("could not get container status: %s", err),
			}
		}

		if status.IsDone() {
			break
		}
	}

	logger.Info("container.status", "exitCode", status.ExitCode())

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

		return &RunResult{
			Code:  status.ExitCode(),
			Error: fmt.Sprintf("could not get container logs: %s", err),
		}
	}

	return &RunResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   status.ExitCode(),
	}
}
