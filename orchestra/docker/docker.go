package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/cli/cli/connhelper"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/jtarchie/ci/orchestra"
)

type Docker struct {
	client    *client.Client
	logger    *slog.Logger
	namespace string
}

// Close implements orchestra.Driver.
func (d *Docker) Close() error {
	// find all containers in the namespace and remove them
	attempts := 5
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
			time.Sleep(time.Duration(1<<currentAttempt) * time.Second) // exponential backoff
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

func NewDocker(namespace string, logger *slog.Logger) (orchestra.Driver, error) {
	var clientOpts []client.Opt

	dockerHost := os.Getenv("DOCKER_HOST")
	if strings.HasPrefix(dockerHost, "ssh://") {
		// https://gist.github.com/agbaraka/654a218f8ea13b3da8a47d47595f5d05
		helper, err := connhelper.GetConnectionHelper(dockerHost)
		if err != nil {
			return nil, fmt.Errorf("failed to get connection helper: %w", err)
		}

		httpClient := &http.Client{
			Transport: &http.Transport{
				DialContext: helper.Dialer,
			},
		}

		clientOpts = append(clientOpts,
			client.WithHTTPClient(httpClient),
			client.WithHost(helper.Host),
			client.WithDialContext(helper.Dialer),
			client.WithAPIVersionNegotiation(),
		)
	} else {
		clientOpts = append(clientOpts, client.FromEnv, client.WithAPIVersionNegotiation())
	}

	cli, err := client.NewClientWithOpts(clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Docker{
		client:    cli,
		logger:    logger,
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
	_ orchestra.Container       = &Container{}
	_ orchestra.ContainerStatus = &ContainerStatus{}
	_ orchestra.Volume          = &Volume{}
)
