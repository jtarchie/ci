package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/jtarchie/ci/orchestra"
)

type DockerVolume struct {
	client *client.Client
	volume volume.Volume
}

// Cleanup implements orchestra.Volume.
func (d *DockerVolume) Cleanup(ctx context.Context) error {
	err := d.client.VolumeRemove(ctx, d.volume.Name, true)
	if err != nil {
		return fmt.Errorf("could not destroy volume: %w", err)
	}

	return nil
}

func (d *Docker) CreateVolume(ctx context.Context, name string, size int) (orchestra.Volume, error) {
	volume, err := d.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: fmt.Sprintf("%s-%s", d.namespace, name),
	})
	if err != nil {
		return nil, fmt.Errorf("could not create volume: %w", err)
	}

	return &DockerVolume{
		client: d.client,
		volume: volume,
	}, nil
}
