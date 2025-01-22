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

type Orchestrator interface {
	RunContainer(ctx context.Context, task Task) (Container, error)
	Name() string
}

var _ Orchestrator = &Docker{}
var _ Container = &DockerContainer{}
var _ ContainerStatus = &DockerContainerStatus{}

var _ Orchestrator = &Fly{}
var _ Container = &FlyContainer{}
var _ ContainerStatus = &FlyContainerStatus{}

var _ Orchestrator = &Native{}
var _ Container = &NativeContainer{}
var _ ContainerStatus = &NativeStatus{}
