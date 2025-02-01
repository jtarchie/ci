package docker

import (
	"errors"
	"fmt"

	"github.com/docker/docker/client"
	"github.com/jtarchie/ci/orchestra"
)

type Docker struct {
	client    *client.Client
	namespace string
}

func NewDocker(namespace string) (orchestra.Driver, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Docker{
		client:    cli,
		namespace: namespace,
	}, nil
}

func (d *Docker) Name() string {
	return "docker"
}

var ErrContainerNotFound = errors.New("container not found")

func init() {
	orchestra.Add("docker", NewDocker)
}

var (
	_ orchestra.Driver          = &Docker{}
	_ orchestra.Container       = &DockerContainer{}
	_ orchestra.ContainerStatus = &DockerContainerStatus{}
	_ orchestra.Volume          = &DockerVolume{}
)
