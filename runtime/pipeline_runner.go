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
	client orchestra.Orchestrator
}

func NewPipelineRunner(
	client orchestra.Orchestrator,
) *PipelineRunner {
	return &PipelineRunner{
		log:    slog.Default().WithGroup("pipeline.runner"),
		client: client,
	}
}

type Result struct {
	Code   int    `js:"code"   json:"code"`
	Error  string `js:"error"  json:"error"`
	Stderr string `js:"stderr" json:"stderr"`
	Stdout string `js:"stdout" json:"stdout"`
}

type RunInput struct {
	Command []string `js:"command" json:"command"`
	Image   string   `js:"image"   json:"image"`
	Name    string   `js:"name"    json:"name"`
}

func (c *PipelineRunner) Run(input RunInput) *Result {
	ctx := context.Background()

	taskID, err := uuid.NewV7()
	if err != nil {
		return &Result{
			Code:  1,
			Error: fmt.Sprintf("could not generate uuid: %s", err),
		}
	}

	logger := c.log.With("id", taskID, "orchestrator", c.client.Name())

	logger.Info("container.run", "input", input)

	container, err := c.client.RunContainer(
		ctx,
		orchestra.Task{
			ID:      fmt.Sprintf("%s-%s", input.Name, taskID.String()),
			Image:   input.Image,
			Command: input.Command,
		},
	)
	if err != nil {
		return &Result{
			Code:  1,
			Error: fmt.Sprintf("could not run container: %s", err),
		}
	}

	var status orchestra.ContainerStatus

	for {
		var err error

		status, err = container.Status(ctx)
		if err != nil {
			return &Result{
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
			slog.Error("container.cleanup", "err", err)
		}
	}()

	stdout, stderr := &strings.Builder{}, &strings.Builder{}

	err = container.Logs(ctx, stdout, stderr)
	if err != nil {
		logger.Error("container.logs", "err", err)

		return &Result{
			Code:  status.ExitCode(),
			Error: fmt.Sprintf("could not get container logs: %s", err),
		}
	}

	return &Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   status.ExitCode(),
	}
}
