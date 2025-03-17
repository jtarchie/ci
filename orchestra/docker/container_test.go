package docker_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/orchestra/docker"
	. "github.com/onsi/gomega"
)

func TestDocker(t *testing.T) {
	t.Parallel()

	t.Run("with a user", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		client, err := docker.NewDocker("test-" + uuid.NewString())
		assert.Expect(err).NotTo(HaveOccurred())
		defer client.Close()

		taskID, err := uuid.NewV7()
		assert.Expect(err).NotTo(HaveOccurred())

		container, err := client.RunContainer(
			context.Background(),
			orchestra.Task{
				ID:      taskID.String(),
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
}
