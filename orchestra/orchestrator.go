package orchestra

import (
	"context"
	"io"
)

type ContainerStatus interface {
	IsDone() bool
	ExitCode() int
}

type Container interface {
	Cleanup(ctx context.Context) error
	Logs(ctx context.Context, stdout, stderr io.Writer) error
	Status(ctx context.Context) (ContainerStatus, error)
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
}
