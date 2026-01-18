package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jtarchie/ci/orchestra"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

type PipelineRunner struct {
	client  orchestra.Driver
	ctx     context.Context //nolint: containedctx
	logger  *slog.Logger
	volumes []orchestra.Volume
}

func NewPipelineRunner(
	ctx context.Context,
	client orchestra.Driver,
	logger *slog.Logger,
) *PipelineRunner {
	return &PipelineRunner{
		client:  client,
		ctx:     ctx,
		logger:  logger.WithGroup("pipeline.run"),
		volumes: []orchestra.Volume{},
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
			logger.Error("container.cleanup.error", "err", err)
		}
	}()

	stdout, stderr := &strings.Builder{}, &strings.Builder{}

	err = container.Logs(ctx, stdout, stderr)
	if err != nil {
		logger.Error("container.logs.error", "err", err)

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return &RunResult{Status: RunAbort}, nil
		}

		return nil, fmt.Errorf("could not get container logs: %w", err)
	}

	logger.Debug("container.logs", "stdout", stdout.String(), "stderr", stderr.String())

	return &RunResult{
		Status: RunComplete,
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   containerStatus.ExitCode(),
	}, nil
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
