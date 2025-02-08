package docker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/jtarchie/ci/orchestra"
)

type Docker struct {
	client    *client.Client
	namespace string
}

// Close implements orchestra.Driver.
func (d *Docker) Close() error {
	// find all containers in the namespace and remove them
	attempts := 3
	for currentAttempt := range attempts {
		_, err := d.client.ContainersPrune(context.Background(), filters.NewArgs(
			filters.Arg("label", "orchestra.namespace="+d.namespace),
		))
		if err == nil {
			break
		}

		if !errdefs.IsConflict(err) {
			return fmt.Errorf("failed to prune containers: %w", err)
		}

		if currentAttempt < attempts-1 {
			time.Sleep(time.Duration(currentAttempt+1) * time.Second) // jitter sleep
		} else {
			return fmt.Errorf("failed to prune containers after %d attempts: %w", attempts, err)
		}
	}

	// find all volumes in the namespace and remove them
	volumes, err := d.client.VolumeList(context.Background(), volume.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "orchestra.namespace="+d.namespace),
		),
	})
	if err != nil {
		return fmt.Errorf("failed to list volumes: %w", err)
	}

	for _, volume := range volumes.Volumes {
		if err := d.client.VolumeRemove(context.Background(), volume.Name, true); err != nil {
			return fmt.Errorf("failed to remove volume %s: %w", volume.Name, err)
		}
	}

	return nil
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
