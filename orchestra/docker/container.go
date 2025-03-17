package docker

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/jtarchie/ci/orchestra"
)

type Container struct {
	id     string
	client *client.Client
	task   orchestra.Task
}

type ContainerStatus struct {
	state *container.State
}

func (d *Container) Status(ctx context.Context) (orchestra.ContainerStatus, error) {
	// doc: https://docs.docker.com/reference/api/engine/version/v1.43/#tag/Container/operation/ContainerInspect
	inspection, err := d.client.ContainerInspect(ctx, d.id)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	return &ContainerStatus{
		state: inspection.State,
	}, nil
}

func (d *Container) Logs(ctx context.Context, stdout, stderr io.Writer) error {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	}

	logs, err := d.client.ContainerLogs(ctx, d.id, options)
	if err != nil {
		return fmt.Errorf("failed to get container logs: %w", err)
	}

	_, err = stdcopy.StdCopy(stdout, stderr, logs)
	if err != nil {
		return fmt.Errorf("failed to copy logs: %w", err)
	}

	return nil
}

func (d *Container) Cleanup(ctx context.Context) error {
	err := d.client.ContainerRemove(ctx, d.id, container.RemoveOptions{
		Force:         true,
		RemoveLinks:   false,
		RemoveVolumes: false,
	})
	if err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}

func (s *ContainerStatus) IsDone() bool {
	return s.state.Status == "exited"
}

func (s *ContainerStatus) ExitCode() int {
	return s.state.ExitCode
}

func (d *Docker) RunContainer(ctx context.Context, task orchestra.Task) (orchestra.Container, error) {
	reader, err := d.client.ImagePull(ctx, task.Image, image.PullOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to initiate pull image: %w", err)
	}

	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to pull image: %w", err)
	}

	containerName := fmt.Sprintf("%s-%s", d.namespace, task.ID)

	mounts := []mount.Mount{}

	//nolint:varnamelen
	for _, m := range task.Mounts {
		volume, err := d.CreateVolume(ctx, m.Name, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to create volume: %w", err)
		}

		dockerVolume, _ := volume.(*Volume)

		mounts = append(mounts, mount.Mount{
			Type:   "volume",
			Source: dockerVolume.volume.Name,
			Target: filepath.Join("/tmp", containerName, m.Path),
		})
	}

	env := []string{}
	for k, v := range task.Env {
		env = append(env, k+"="+v)
	}

	enabledStdin := task.Stdin != nil

	response, err := d.client.ContainerCreate(
		ctx,
		&container.Config{
			Image: task.Image,
			Cmd:   task.Command,
			Labels: map[string]string{
				"orchestra.namespace": d.namespace,
			},
			Env:        env,
			WorkingDir: filepath.Join("/tmp", containerName),
			OpenStdin:  enabledStdin,
			StdinOnce:  enabledStdin,
			User:       task.User,
		},
		&container.HostConfig{
			Mounts: mounts,
		}, nil, nil,
		containerName,
	)
	if err != nil && errdefs.IsConflict(err) {
		filter := filters.NewArgs()
		filter.Add("name", containerName)

		containers, err := d.client.ContainerList(ctx, container.ListOptions{Filters: filter, All: true})
		if err != nil {
			return nil, fmt.Errorf("failed to list containers: %w", err)
		}

		if len(containers) == 0 {
			return nil, fmt.Errorf("failed to find container by name %s: %w", containerName, ErrContainerNotFound)
		}

		return &Container{
			id:     containers[0].ID,
			client: d.client,
			task:   task,
		}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	if enabledStdin {
		// Attach to the container's STDIN
		attachOptions := container.AttachOptions{
			Stream: true,
			Stdin:  true,
		}

		conn, err := d.client.ContainerAttach(ctx, response.ID, attachOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to attach to container: %w", err)
		}
		defer conn.Close()

		// Send the STDIN string to the container
		_, err = io.Copy(conn.Conn, task.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to write to container stdin: %w", err)
		}
	}

	err = d.client.ContainerStart(ctx, response.ID, container.StartOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	return &Container{
		id:     response.ID,
		client: d.client,
		task:   task,
	}, nil
}
