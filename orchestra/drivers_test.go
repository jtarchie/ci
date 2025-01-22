package orchestra_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jtarchie/ci/orchestra"
	. "github.com/onsi/gomega"
)

func TestDrivers(t *testing.T) {
	t.Parallel()

	orchestra.Each(func(name string, init orchestra.InitFunc) {
		t.Run(fmt.Sprintf("%s exit code failed", name), func(t *testing.T) {
			t.Parallel()

			assert := NewGomegaWithT(t)

			client, err := init("test")
			assert.Expect(err).NotTo(HaveOccurred())

			id, err := uuid.NewV7()
			assert.Expect(err).NotTo(HaveOccurred())

			container, err := client.RunContainer(
				context.Background(),
				orchestra.Task{
					ID:      id.String(),
					Image:   "alpine",
					Command: []string{"sh", "-c", "exit 1"},
				},
			)
			assert.Expect(err).NotTo(HaveOccurred())
			defer func(container orchestra.Container) { _ = container.Cleanup(context.Background()) }(container)

			assert.Eventually(func() bool {
				status, err := container.Status(context.Background())
				assert.Expect(err).NotTo(HaveOccurred())

				return status.IsDone() && status.ExitCode() == 1
			}, "10s").Should(BeTrue())

			assert.Consistently(func() bool {
				status, err := container.Status(context.Background())
				assert.Expect(err).NotTo(HaveOccurred())

				return status.IsDone() && status.ExitCode() == 1
			}).Should(BeTrue())
		})

		t.Run(fmt.Sprintf("%s happy path", name), func(t *testing.T) {
			assert := NewGomegaWithT(t)

			client, err := init("test")
			assert.Expect(err).NotTo(HaveOccurred())

			id, err := uuid.NewV7()
			assert.Expect(err).NotTo(HaveOccurred())

			container, err := client.RunContainer(
				context.Background(),
				orchestra.Task{
					ID:      id.String(),
					Image:   "alpine",
					Command: []string{"echo", "hello"},
				},
			)
			assert.Expect(err).NotTo(HaveOccurred())
			defer func(container orchestra.Container) { _ = container.Cleanup(context.Background()) }(container)

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
				// assert.Expect(err).NotTo(HaveOccurred())

				return strings.Contains(stdout.String(), "hello")
			}, "90s").Should(BeTrue())

			// running a container should be deterministic and idempotent
			container, err = client.RunContainer(
				context.Background(),
				orchestra.Task{
					ID:      id.String(),
					Image:   "alpine",
					Command: []string{"echo", "hello"},
				},
			)
			assert.Expect(err).NotTo(HaveOccurred())

			assert.Eventually(func() bool {
				status, err := container.Status(context.Background())
				assert.Expect(err).NotTo(HaveOccurred())

				return status.IsDone() && status.ExitCode() == 0
			}).Should(BeTrue())

			assert.Eventually(func() bool {
				stdout, stderr := &strings.Builder{}, &strings.Builder{}
				err := container.Logs(context.Background(), stdout, stderr)
				assert.Expect(err).NotTo(HaveOccurred())
				return strings.Contains(stdout.String(), "hello")
			}).Should(BeTrue())

			err = container.Cleanup(context.Background())
			assert.Expect(err).NotTo(HaveOccurred())
		})
	})
}
