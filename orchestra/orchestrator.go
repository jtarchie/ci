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

var (
	_ Orchestrator    = &Docker{}
	_ Container       = &DockerContainer{}
	_ ContainerStatus = &DockerContainerStatus{}
)

var (
	_ Orchestrator    = &Fly{}
	_ Container       = &FlyContainer{}
	_ ContainerStatus = &FlyContainerStatus{}
)

var (
	_ Orchestrator    = &Native{}
	_ Container       = &NativeContainer{}
	_ ContainerStatus = &NativeStatus{}
)
