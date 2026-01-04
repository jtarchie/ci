package digitalocean_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/orchestra/digitalocean"
	gonanoid "github.com/matoous/go-nanoid/v2"
	. "github.com/onsi/gomega"
)

func TestDigitalOcean(t *testing.T) {
	token := os.Getenv("DIGITALOCEAN_TOKEN")
	if token == "" {
		t.Skip("DIGITALOCEAN_TOKEN not set, skipping DigitalOcean integration tests")
	}

	// Use a test-specific tag to avoid cleaning up production resources
	const testTag = "ci-test"

	// Clean up any orphaned resources from previous failed test runs (only those with test tag)
	err := digitalocean.CleanupOrphanedResources(context.Background(), token, slog.Default(), testTag)
	if err != nil {
		t.Logf("Warning: failed to cleanup orphaned resources: %v", err)
	}

	// These tests are slow (droplet creation takes time) so do not run in parallel with other packages
	t.Run("basic container execution", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		namespace := "test-" + gonanoid.Must()
		client, err := digitalocean.NewDigitalOcean(namespace, slog.Default(), map[string]string{
			"token": token,
			"tags":  testTag,
		})
		assert.Expect(err).NotTo(HaveOccurred())

		// Always clean up the droplet, even if the test fails
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
				Command: []string{"echo", "hello from digitalocean"},
			},
		)
		assert.Expect(err).NotTo(HaveOccurred())

		// Droplet creation + container run can take a while
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

			return strings.Contains(stdout.String(), "hello from digitalocean")
		}, "30s", "2s").Should(BeTrue())
	})

	t.Run("with auto size", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		namespace := "test-" + gonanoid.Must()
		client, err := digitalocean.NewDigitalOcean(namespace, slog.Default(), map[string]string{
			"token": token,
			"size":  "auto",
			"tags":  testTag,
		})
		assert.Expect(err).NotTo(HaveOccurred())

		// Always clean up the droplet, even if the test fails
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
					Memory: 2 * 1024 * 1024 * 1024, // 2GB - should trigger s-2vcpu-2gb or larger
					CPU:    1024,
				},
			},
		)
		assert.Expect(err).NotTo(HaveOccurred())

		assert.Eventually(func() bool {
			status, err := container.Status(context.Background())
			if err != nil {
				return false
			}

			return status.IsDone() && status.ExitCode() == 0
		}, "10m", "5s").Should(BeTrue())
	})
}
