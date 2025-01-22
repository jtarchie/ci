package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jtarchie/ci/orchestra"
)

type Sandbox struct {
	log    *slog.Logger
	client orchestra.Orchestrator
}

func NewSandbox(
	client orchestra.Orchestrator,
) *Sandbox {
	return &Sandbox{
		log:    slog.Default().WithGroup("sandbox"),
		client: client,
	}
}

type Result struct {
	Code   int    `json:"code" js:"code"`
	Error  string `json:"error" js:"error"`
	Stderr string `json:"stderr" js:"stderr"`
	Stdout string `json:"stdout" js:"stdout"`
}

type RunInput struct {
	Command []string `json:"command" js:"command"`
	Image   string   `json:"image" js:"image"`
	Name    string   `json:"name" js:"name"`
}

func (c *Sandbox) Run(input RunInput) *Result {
	ctx := context.Background()

	id, err := uuid.NewV7()
	if err != nil {
		return &Result{
			Code:  1,
			Error: fmt.Sprintf("could not generate uuid: %s", err),
		}
	}

	logger := c.log.With("id", id, "orchestrator", c.client.Name())

	logger.Info("container.run", "input", input)

	container, err := c.client.RunContainer(
		ctx,
		orchestra.Task{
			ID:      fmt.Sprintf("%s-%s", input.Name, id.String()),
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
