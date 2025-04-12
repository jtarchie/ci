package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jtarchie/ci/orchestra"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

type PipelineRunner struct {
	client orchestra.Driver
	ctx    context.Context //nolint: containedctx
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
}

func (c *PipelineRunner) CreateVolume(input VolumeInput) (*VolumeResult, error) {
	ctx := c.ctx

	logger := c.logger
	logger.Debug("volume.create", "input", input)

	volume, err := c.client.CreateVolume(ctx, input.Name, input.Size)
	if err != nil {
		logger.Error("volume.create", "err", err)

		return nil, fmt.Errorf("could not create volume: %w", err)
	}

	return &VolumeResult{
		Volume: volume,
	}, nil
}

type RunResult struct {
	Code   int    `json:"code"`
	Stderr string `json:"stderr"`
	Stdout string `json:"stdout"`

	Status RunStatus `json:"status"`
}

type RunInput struct {
	Command struct {
		Path string   `json:"path"`
		Args []string `json:"args"`
		User string   `json:"user"`
	} `json:"command"`
	Env        map[string]string       `json:"env"`
	Image      string                  `json:"image"`
	Mounts     map[string]VolumeResult `json:"mounts"`
	Name       string                  `json:"name"`
	Privileged bool                    `json:"privileged"`
	Stdin      string                  `json:"stdin"`
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

	taskID := gonanoid.Must()

	logger = c.logger.With("taskID", taskID, "name", input.Name, "privileged", input.Privileged)

	logger.Debug("container.run")

	var mounts orchestra.Mounts
	for path, volume := range input.Mounts {
		mounts = append(mounts, orchestra.Mount{
			Name: volume.Name(),
			Path: path,
		})
	}

	logger.Debug("container.run", "mounts", mounts)

	command := []string{input.Command.Path}
	command = append(command, input.Command.Args...)

	container, err := c.client.RunContainer(
		ctx,
		orchestra.Task{
			Command:    command,
			Env:        input.Env,
			ID:         fmt.Sprintf("%s-%s", input.Name, taskID),
			Image:      input.Image,
			Mounts:     mounts,
			Privileged: input.Privileged,
			Stdin:      strings.NewReader(input.Stdin),
			User:       input.Command.User,
		},
	)
	if err != nil {
		logger.Error("container.run", "err", err)

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return &RunResult{Status: RunAbort}, nil
		}

		return nil, fmt.Errorf("could not run container: %w", err)
	}

	var containerStatus orchestra.ContainerStatus

	for {
		var err error

		containerStatus, err = container.Status(ctx)
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

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return &RunResult{Status: RunAbort}, nil
		}

		return nil, fmt.Errorf("could not get container logs: %w", err)
	}

	return &RunResult{
		Status: RunComplete,
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   containerStatus.ExitCode(),
	}, nil
}
