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
	Logs(ctx context.Context, stdout, stderr io.Writer) error
	Status(ctx context.Context) (ContainerStatus, error)
	// ID returns a unique identifier for this container (driver-specific).
	ID() string
}

type Volume interface {
	Cleanup(ctx context.Context) error
	Name() string
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
