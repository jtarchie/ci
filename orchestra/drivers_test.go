package orchestra_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jtarchie/ci/orchestra"
	_ "github.com/jtarchie/ci/orchestra/docker"
	_ "github.com/jtarchie/ci/orchestra/native"
	. "github.com/onsi/gomega"
)

func TestDrivers(t *testing.T) {
	t.Parallel()

	orchestra.Each(func(name string, init orchestra.InitFunc) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("with stdin", func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)

				client, err := init("test-" + uuid.NewString())
				assert.Expect(err).NotTo(HaveOccurred())
				defer client.Close()

				taskID, err := uuid.NewV7()
				assert.Expect(err).NotTo(HaveOccurred())

				container, err := client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID.String(),
						Image:   "busybox",
						Command: []string{"sh", "-c", "cat < /dev/stdin"},
						Stdin:   strings.NewReader("hello"),
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

					return strings.Contains(stdout.String(), "hello")
				}, "10s").Should(BeTrue())

				err = client.Close()
				assert.Expect(err).NotTo(HaveOccurred())
			})

			t.Run("exit code failed", func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)

				client, err := init("test-" + uuid.NewString())
				assert.Expect(err).NotTo(HaveOccurred())
				defer client.Close()

				taskID, err := uuid.NewV7()
				assert.Expect(err).NotTo(HaveOccurred())

				container, err := client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID.String(),
						Image:   "busybox",
						Command: []string{"sh", "-c", "exit 1"},
					},
				)
				assert.Expect(err).NotTo(HaveOccurred())

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

				err = client.Close()
				assert.Expect(err).NotTo(HaveOccurred())
			})

			t.Run("happy path", func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)

				client, err := init("test-" + uuid.NewString())
				assert.Expect(err).NotTo(HaveOccurred())
				defer client.Close()

				taskID, err := uuid.NewV7()
				assert.Expect(err).NotTo(HaveOccurred())

				container, err := client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID.String(),
						Image:   "busybox",
						Command: []string{"echo", "hello"},
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
					// assert.Expect(err).NotTo(HaveOccurred())

					return strings.Contains(stdout.String(), "hello")
				}, "10s").Should(BeTrue())

				// running a container should be deterministic and idempotent
				container, err = client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID.String(),
						Image:   "busybox",
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

				err = client.Close()
				assert.Expect(err).NotTo(HaveOccurred())
			})

			t.Run("volume", func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)

				client, err := init("test-" + uuid.NewString())
				assert.Expect(err).NotTo(HaveOccurred())
				defer client.Close()

				taskID, err := uuid.NewV7()
				assert.Expect(err).NotTo(HaveOccurred())

				container, err := client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID.String(),
						Image:   "busybox",
						Command: []string{"sh", "-c", "echo world > ./test/hello"},
						Mounts: orchestra.Mounts{
							{Name: "test", Path: "/test"},
						},
					},
				)
				assert.Expect(err).NotTo(HaveOccurred())

				assert.Eventually(func() bool {
					status, err := container.Status(context.Background())
					assert.Expect(err).NotTo(HaveOccurred())

					return status.IsDone() && status.ExitCode() == 0
				}, "10s").Should(BeTrue())

				container, err = client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID.String() + "-2",
						Image:   "busybox",
						Command: []string{"cat", "./test/hello"},
						Mounts: orchestra.Mounts{
							{Name: "test", Path: "/test"},
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

					return strings.Contains(stdout.String(), "world")
				}, "10s").Should(BeTrue())

				err = client.Close()
				assert.Expect(err).NotTo(HaveOccurred())
			})

			t.Run("environment variables", func(t *testing.T) {
				t.Parallel()

				os.Setenv("IGNORE", "ME") //nolint: usetesting

				assert := NewGomegaWithT(t)

				client, err := init("test-" + uuid.NewString())
				assert.Expect(err).NotTo(HaveOccurred())
				defer client.Close()

				taskID, err := uuid.NewV7()
				assert.Expect(err).NotTo(HaveOccurred())

				container, err := client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID.String(),
						Image:   "busybox",
						Command: []string{"env"},
						Env:     map[string]string{"HELLO": "WORLD"},
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

					return strings.Contains(stdout.String(), "HELLO=WORLD\n") && !strings.Contains(stdout.String(), "IGNORE")
				}, "10s").Should(BeTrue())
			})
		})
	})
}
