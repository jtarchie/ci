package orchestra

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Native struct {
	namespace string
}

func NewNative(namespace string) (Orchestrator, error) {
	return &Native{
		namespace: namespace,
	}, nil
}

func (n *Native) Name() string {
	return "native"
}

type NativeContainer struct {
	command *exec.Cmd
	stdout  *strings.Builder
	errChan chan error
}

func (n *NativeContainer) Cleanup(ctx context.Context) error {
	return nil
}

func (n *NativeContainer) Logs(ctx context.Context, stdout io.Writer, stderr io.Writer) error {
	_, err := io.WriteString(stdout, n.stdout.String())
	if err != nil {
		return fmt.Errorf("failed to copy stdout: %w", err)
	}

	return nil
}

type NativeStatus struct {
	exitCode int
	isDone   bool
}

func (n *NativeStatus) ExitCode() int {
	return n.exitCode
}

func (n *NativeStatus) IsDone() bool {
	return n.isDone
}

func (n *NativeContainer) Status(ctx context.Context) (ContainerStatus, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("failed to get status: %w", context.Canceled)
	case err := <-n.errChan:
		if err != nil {
			var exitErr *exec.ExitError

			if !errors.As(err, &exitErr) {
				return nil, fmt.Errorf("failed to get status: %w", err)
			}
		}

		defer func() { n.errChan <- err }()

		return &NativeStatus{
			exitCode: n.command.ProcessState.ExitCode(),
			isDone:   n.command.ProcessState.Exited(),
		}, nil
	default:
		return &NativeStatus{
			exitCode: -1,
			isDone:   false,
		}, nil
	}
}

func (n *Native) RunContainer(ctx context.Context, task Task) (Container, error) {
	containerName := fmt.Sprintf("%s-%s", n.namespace, task.ID)

	dir, err := os.MkdirTemp("", containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	errChan := make(chan error, 1)

	//nolint:gosec
	command := exec.CommandContext(ctx, task.Command[0], task.Command[1:]...)

	command.Dir = dir
	command.Env = []string{}

	stdout := &strings.Builder{}
	command.Stderr = stdout
	command.Stdout = stdout

	go func() {
		err := command.Run()
		if err != nil {
			errChan <- fmt.Errorf("failed to run command: %w", err)

			return
		}

		errChan <- nil
	}()

	return &NativeContainer{
		command: command,
		errChan: errChan,
		stdout:  stdout,
	}, nil
}

func init() {
	Add("native", NewNative)
}
