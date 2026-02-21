package fly

import (
	"context"
	"fmt"

	fly "github.com/superfly/fly-go"

	"github.com/jtarchie/ci/orchestra"
)

type Volume struct {
	id     string
	name   string
	path   string
	driver *Fly
}

func (v *Volume) Name() string {
	return v.name
}

func (v *Volume) Path() string {
	return v.path
}

func (v *Volume) Cleanup(ctx context.Context) error {
	v.driver.logger.Debug("fly.volume.cleanup", "volume", v.id, "name", v.name)

	_, err := v.driver.client.DeleteVolume(ctx, v.driver.appName, v.id)
	if err != nil {
		return fmt.Errorf("failed to delete volume %s: %w", v.id, err)
	}

	return nil
}

func (f *Fly) CreateVolume(ctx context.Context, name string, size int) (orchestra.Volume, error) {
	volumeName := sanitizeVolumeName(fmt.Sprintf("%s_%s", f.namespace, name))

	// Check if we already have a volume with this name
	f.mu.Lock()
	existing, ok := f.volumes[name]
	f.mu.Unlock()

	if ok {
		f.logger.Info("fly.volume.reuse", "volume", existing.id, "name", volumeName)
		return existing, nil
	}

	if size <= 0 {
		size = 1 // Fly volumes must be at least 1 GB
	}

	f.logger.Debug("fly.volume.create", "name", volumeName, "size_gb", size, "region", f.region)

	vol, err := f.client.CreateVolume(ctx, f.appName, fly.CreateVolumeRequest{
		Name:   volumeName,
		Region: f.region,
		SizeGb: &size,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create fly volume %q: %w", volumeName, err)
	}

	f.trackVolume(vol.ID)

	f.logger.Info("fly.volume.created", "volume", vol.ID, "name", volumeName)

	v := &Volume{
		id:     vol.ID,
		name:   volumeName,
		path:   "/" + volumeName,
		driver: f,
	}

	f.mu.Lock()
	f.volumes[name] = v
	f.mu.Unlock()

	return v, nil
}
