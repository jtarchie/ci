package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/jtarchie/ci/orchestra"
)

type Volume struct {
	client     *client.Client
	volume     volume.Volume
	volumeName string
}

// Cleanup implements orchestra.Volume.
func (d *Volume) Cleanup(ctx context.Context) error {
	err := d.client.VolumeRemove(ctx, d.volume.Name, true)
	if err != nil {
		return fmt.Errorf("could not destroy volume: %w", err)
	}

	return nil
}

func (d *Docker) CreateVolume(ctx context.Context, name string, _ int) (orchestra.Volume, error) {
	volume, err := d.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: fmt.Sprintf("%s-%s", d.namespace, name),
		Labels: map[string]string{
			"orchestra.namespace": d.namespace,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("could not create volume: %w", err)
	}

	return &Volume{
		client:     d.client,
		volume:     volume,
		volumeName: name,
	}, nil
}

func (d *Volume) Name() string {
	return d.volumeName
}

// Path implements orchestra.Volume.
// For Docker volumes, this returns the mount path inside containers.
func (d *Volume) Path() string {
	return "/" + d.volumeName
}
