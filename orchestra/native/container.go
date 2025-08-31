package native

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jtarchie/ci/orchestra"
)

type Container struct {
	command *exec.Cmd
	stdout  *strings.Builder
	errChan chan error
}

func (n *Container) Cleanup(_ context.Context) error {
	return nil
}

func (n *Container) Logs(_ context.Context, stdout io.Writer, _ io.Writer) error {
	_, err := io.WriteString(stdout, n.stdout.String())
	if err != nil {
		return fmt.Errorf("failed to copy stdout: %w", err)
	}

	return nil
}

type Status struct {
	exitCode int
	isDone   bool
}

func (n *Status) ExitCode() int {
	return n.exitCode
}

func (n *Status) IsDone() bool {
	return n.isDone
}

func (n *Container) Status(ctx context.Context) (orchestra.ContainerStatus, error) {
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

		return &Status{
			exitCode: n.command.ProcessState.ExitCode(),
			isDone:   n.command.ProcessState.Exited(),
		}, nil
	default:
		return &Status{
			exitCode: -1,
			isDone:   false,
		}, nil
	}
}

func (n *Native) RunContainer(ctx context.Context, task orchestra.Task) (orchestra.Container, error) {
	logger := n.logger.With("taskID", task.ID)

	containerName := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s-%s", n.namespace, task.ID))))

	dir, err := os.MkdirTemp(n.path, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	for _, mount := range task.Mounts {
		volume, err := n.CreateVolume(ctx, mount.Name, 0)
		if err != nil {
			logger.Error("volume.create", "name", mount.Name, "err", err)

			return nil, fmt.Errorf("failed to create volume: %w", err)
		}

		nativeVolume, _ := volume.(*Volume)

		err = os.Symlink(nativeVolume.path, filepath.Join(dir, mount.Path))
		if err != nil {
			logger.Error("volume.create", "name", mount.Name, "err", err)

			return nil, fmt.Errorf("failed to create symlink: %w", err)
		}
	}

	errChan := make(chan error, 1)

	//nolint:gosec
	command := exec.CommandContext(ctx, task.Command[0], task.Command[1:]...)

	command.Dir = dir

	env := []string{}
	for k, v := range task.Env {
		env = append(env, k+"="+v)
	}

	command.Env = env

	stdout := &strings.Builder{}
	command.Stderr = stdout
	command.Stdout = stdout

	if task.Stdin != nil {
		command.Stdin = task.Stdin
	}

	if task.Image != "" {
		logger.Warn("orchestra.native", "warn", "image is not supported in native mode", "image", task.Image)
	}

	if task.User != "" {
		logger.Warn("orchestra.native", "warn", "user is not supported in native mode", "user", task.User)
	}

	if task.Privileged {
		logger.Warn("orchestra.native", "warn", "privileged is not supported in native mode")
	}

	go func() {
		err := command.Run()
		if err != nil {
			logger.Error("orchestra.native", "err", err)

			errChan <- fmt.Errorf("failed to run command: %w", err)

			return
		}

		errChan <- nil
	}()

	return &Container{
		command: command,
		errChan: errChan,
		stdout:  stdout,
	}, nil
}
