package docker_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/orchestra/docker"
	gonanoid "github.com/matoous/go-nanoid/v2"
	. "github.com/onsi/gomega"
)

func TestDocker(t *testing.T) {
	t.Parallel()

	t.Run("with a user", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		client, err := docker.NewDocker("test-"+gonanoid.Must(), slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())

		defer func() { _ = client.Close() }()

		taskID := gonanoid.Must()

		container, err := client.RunContainer(
			context.Background(),
			orchestra.Task{
				ID:      taskID,
				Image:   "busybox",
				Command: []string{"whoami"},
				User:    "nobody",
			},
		)
		assert.Expect(err).NotTo(HaveOccurred())

		assert.Eventually(func() bool {
			status, err := container.Status(context.Background())
			assert.Expect(err).NotTo(HaveOccurred())

			return status.IsDone() && status.ExitCode() == 0
		}, "10s").Should(BeTrue())

		assert.Eventually(func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			stdout, stderr := &strings.Builder{}, &strings.Builder{}
			_ = container.Logs(ctx, stdout, stderr)

			return strings.Contains(stdout.String(), "nobody")
		}, "1s").Should(BeTrue())

		err = client.Close()
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("with privileged", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		client, err := docker.NewDocker("test-"+gonanoid.Must(), slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())

		defer func() { _ = client.Close() }()

		taskID := gonanoid.Must()

		container, err := client.RunContainer(
			context.Background(),
			orchestra.Task{
				ID:         taskID,
				Image:      "busybox",
				Command:    []string{"ls", "-l", "/dev/kmsg"},
				Privileged: true,
			},
		)
		assert.Expect(err).NotTo(HaveOccurred())

		assert.Eventually(func() bool {
			status, err := container.Status(context.Background())
			assert.Expect(err).NotTo(HaveOccurred())

			return status.IsDone() && status.ExitCode() == 0
		}, "10s").Should(BeTrue())

		err = client.Close()
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("with container limits", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		client, err := docker.NewDocker("test-"+gonanoid.Must(), slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())

		defer func() { _ = client.Close() }()

		t.Run("cpu and memory limits", func(t *testing.T) {
			taskID := gonanoid.Must()

			// Use sh to check cgroup values from inside the container
			// For cgroup v2 (modern Docker), check /sys/fs/cgroup/cpu.max and memory.max
			// For cgroup v1 (older Docker), check /sys/fs/cgroup/cpu/cpu.shares and memory/memory.limit_in_bytes
			// Note: cgroup v2 converts CPU shares to weight differently (shares/1024 * 100 + 1)
			container, err := client.RunContainer(
				context.Background(),
				orchestra.Task{
					ID:    taskID,
					Image: "busybox",
					Command: []string{
						"sh", "-c",
						"cat /sys/fs/cgroup/cpu/cpu.shares 2>/dev/null || cat /sys/fs/cgroup/cpu.weight 2>/dev/null; " +
							"cat /sys/fs/cgroup/memory/memory.limit_in_bytes 2>/dev/null || cat /sys/fs/cgroup/memory.max 2>/dev/null",
					},
					ContainerLimits: orchestra.ContainerLimits{
						CPU:    512,       // 512 CPU shares
						Memory: 134217728, // 128MB in bytes
					},
				},
			)
			assert.Expect(err).NotTo(HaveOccurred())

			assert.Eventually(func() bool {
				status, err := container.Status(context.Background())
				assert.Expect(err).NotTo(HaveOccurred())

				return status.IsDone() && status.ExitCode() == 0
			}, "10s").Should(BeTrue())

			assert.Eventually(func() bool {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()

				stdout, stderr := &strings.Builder{}, &strings.Builder{}
				_ = container.Logs(ctx, stdout, stderr)

				output := stdout.String()

				// Check if either cgroup v1 or v2 shows the limits
				// For CPU: cgroup v1 shows 512 shares, cgroup v2 shows weight (formula: (shares-2)*9999/262142 + 1, so 512->20)
				// For Memory: both should show 134217728 bytes
				hasCPULimit := strings.Contains(output, "512") || strings.Contains(output, "20")
				hasMemoryLimit := strings.Contains(output, "134217728")

				return hasCPULimit && hasMemoryLimit
			}, "1s").Should(BeTrue())
		})

		t.Run("memory only limit", func(t *testing.T) {
			taskID := gonanoid.Must()

			// Test memory limit without CPU limit
			container, err := client.RunContainer(
				context.Background(),
				orchestra.Task{
					ID:    taskID,
					Image: "busybox",
					Command: []string{
						"sh", "-c",
						"cat /sys/fs/cgroup/memory/memory.limit_in_bytes 2>/dev/null || cat /sys/fs/cgroup/memory.max 2>/dev/null",
					},
					ContainerLimits: orchestra.ContainerLimits{
						Memory: 67108864, // 64MB in bytes
					},
				},
			)
			assert.Expect(err).NotTo(HaveOccurred())

			assert.Eventually(func() bool {
				status, err := container.Status(context.Background())
				assert.Expect(err).NotTo(HaveOccurred())

				return status.IsDone() && status.ExitCode() == 0
			}, "10s").Should(BeTrue())

			assert.Eventually(func() bool {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()

				stdout, stderr := &strings.Builder{}, &strings.Builder{}
				_ = container.Logs(ctx, stdout, stderr)

				output := stdout.String()

				// Should see 64MB limit
				return strings.Contains(output, "67108864")
			}, "1s").Should(BeTrue())
		})

		err = client.Close()
		assert.Expect(err).NotTo(HaveOccurred())
	})
}
