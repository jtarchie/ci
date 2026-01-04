package hetzner_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/orchestra/hetzner"
	gonanoid "github.com/matoous/go-nanoid/v2"
	. "github.com/onsi/gomega"
)

func TestHetzner(t *testing.T) {
	token := os.Getenv("HETZNER_TOKEN")
	if token == "" {
		t.Skip("HETZNER_TOKEN not set, skipping Hetzner integration tests")
	}

	// Use a test-specific label to avoid cleaning up production resources
	const testLabel = "environment=test"

	// Clean up any orphaned resources from previous failed test runs (only those with test label)
	err := hetzner.CleanupOrphanedResources(context.Background(), token, slog.Default(), testLabel)
	if err != nil {
		t.Logf("Warning: failed to cleanup orphaned resources: %v", err)
	}

	// These tests are slow (server creation takes time) so do not run in parallel with other packages
	t.Run("basic container execution", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		namespace := "test-" + gonanoid.Must()
		client, err := hetzner.NewHetzner(namespace, slog.Default(), map[string]string{
			"token":  token,
			"labels": testLabel,
		})
		assert.Expect(err).NotTo(HaveOccurred())

		// Always clean up the server, even if the test fails
		defer func() {
			closeErr := client.Close()
			assert.Expect(closeErr).NotTo(HaveOccurred())
		}()

		taskID := gonanoid.Must()

		container, err := client.RunContainer(
			context.Background(),
			orchestra.Task{
				ID:      taskID,
				Image:   "busybox",
				Command: []string{"echo", "hello from hetzner"},
			},
		)
		assert.Expect(err).NotTo(HaveOccurred())

		// Server creation + container run can take a while
		assert.Eventually(func() bool {
			status, err := container.Status(context.Background())
			if err != nil {
				return false
			}

			return status.IsDone() && status.ExitCode() == 0
		}, "10m", "5s").Should(BeTrue())

		assert.Eventually(func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			stdout, stderr := &strings.Builder{}, &strings.Builder{}
			_ = container.Logs(ctx, stdout, stderr)

			return strings.Contains(stdout.String(), "hello from hetzner")
		}, "30s", "2s").Should(BeTrue())
	})

	t.Run("with auto size", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		namespace := "test-" + gonanoid.Must()
		client, err := hetzner.NewHetzner(namespace, slog.Default(), map[string]string{
			"token":       token,
			"server_type": "auto",
			"labels":      testLabel,
		})
		assert.Expect(err).NotTo(HaveOccurred())

		// Always clean up the server, even if the test fails
		defer func() {
			closeErr := client.Close()
			assert.Expect(closeErr).NotTo(HaveOccurred())
		}()

		taskID := gonanoid.Must()

		container, err := client.RunContainer(
			context.Background(),
			orchestra.Task{
				ID:      taskID,
				Image:   "busybox",
				Command: []string{"sh", "-c", "cat /proc/meminfo | head -1"},
				ContainerLimits: orchestra.ContainerLimits{
					Memory: 2 * 1024 * 1024 * 1024, // 2GB - should trigger cx32 or larger
					CPU:    1024,
				},
			},
		)
		assert.Expect(err).NotTo(HaveOccurred())

		// Server creation + container run can take a while
		assert.Eventually(func() bool {
			status, err := container.Status(context.Background())
			if err != nil {
				return false
			}

			return status.IsDone() && status.ExitCode() == 0
		}, "10m", "5s").Should(BeTrue())

		assert.Eventually(func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			stdout, stderr := &strings.Builder{}, &strings.Builder{}
			_ = container.Logs(ctx, stdout, stderr)

			return strings.Contains(stdout.String(), "MemTotal")
		}, "30s", "2s").Should(BeTrue())
	})
}
