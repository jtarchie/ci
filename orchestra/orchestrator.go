package orchestra

import (
	"context"
	"errors"
	"io"
)

// ErrContainerNotFound is returned when attempting to get a container that doesn't exist.
var ErrContainerNotFound = errors.New("container not found")

type ContainerStatus interface {
	IsDone() bool
	ExitCode() int
}

type Container interface {
	Cleanup(ctx context.Context) error
	// Logs retrieves container logs. When follow is false, returns all logs up to now.
	// When follow is true, streams logs in real-time until the context is cancelled.
	Logs(ctx context.Context, stdout, stderr io.Writer, follow bool) error
	Status(ctx context.Context) (ContainerStatus, error)
	// ID returns a unique identifier for this container (driver-specific).
	ID() string
}

type Volume interface {
	Cleanup(ctx context.Context) error
	Name() string
	// Path returns the absolute path to the volume directory.
	// For native volumes, this is the actual filesystem path.
	// For container-based volumes, this is the mount path inside containers.
	Path() string
}

type Driver interface {
	Close() error
	CreateVolume(ctx context.Context, name string, size int) (Volume, error)
	Name() string
	RunContainer(ctx context.Context, task Task) (Container, error)
	// GetContainer attempts to find and return an existing container by its ID.
	// Returns ErrContainerNotFound if the container does not exist.
	GetContainer(ctx context.Context, containerID string) (Container, error)
}

// Sandbox represents a long-lived container environment that accepts multiple
// sequential exec calls. Use SandboxDriver.StartSandbox to obtain one.
type Sandbox interface {
	// Exec runs a command inside the sandbox and streams its output.
	// env and workDir are applied per-call; they do not persist between calls.
	Exec(ctx context.Context, cmd []string, env map[string]string, workDir string, stdin io.Reader, stdout, stderr io.Writer) (ContainerStatus, error)
	// Cleanup stops and removes the sandbox container.
	Cleanup(ctx context.Context) error
	// ID returns the driver-specific identifier for the sandbox container.
	ID() string
}

// SandboxDriver is an optional interface that drivers may implement to support
// multi-command sandbox execution. Callers should type-assert the Driver to
// SandboxDriver before use.
type SandboxDriver interface {
	// StartSandbox creates and starts a long-lived container kept alive with
	// "tail -f /dev/null". Subsequent commands are run via Sandbox.Exec.
	StartSandbox(ctx context.Context, task Task) (Sandbox, error)
}
