package orchestra_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jtarchie/ci/orchestra"
	_ "github.com/jtarchie/ci/orchestra/docker"
	_ "github.com/jtarchie/ci/orchestra/k8s"
	_ "github.com/jtarchie/ci/orchestra/native"
	gonanoid "github.com/matoous/go-nanoid/v2"
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

				client, err := init("test-"+gonanoid.Must(), slog.Default(), map[string]string{})
				assert.Expect(err).NotTo(HaveOccurred())

				defer func() { _ = client.Close() }()

				taskID := gonanoid.Must()

				container, err := client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID,
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

				client, err := init("test-"+gonanoid.Must(), slog.Default(), map[string]string{})
				assert.Expect(err).NotTo(HaveOccurred())

				defer func() { _ = client.Close() }()

				taskID := gonanoid.Must()

				container, err := client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID,
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

				client, err := init("test-"+gonanoid.Must(), slog.Default(), map[string]string{})
				assert.Expect(err).NotTo(HaveOccurred())

				defer func() { _ = client.Close() }()

				taskID := gonanoid.Must()

				container, err := client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID,
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
						ID:      taskID,
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

				client, err := init("test-"+gonanoid.Must(), slog.Default(), map[string]string{})
				assert.Expect(err).NotTo(HaveOccurred())

				defer func() { _ = client.Close() }()

				taskID := gonanoid.Must()

				container, err := client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID,
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
						ID:      taskID + "-2",
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

				assert := NewGomegaWithT(t)

				assert.Expect(os.Setenv("IGNORE", "ME")).NotTo(HaveOccurred()) //nolint: usetesting

				client, err := init("test-"+gonanoid.Must(), slog.Default(), map[string]string{})
				assert.Expect(err).NotTo(HaveOccurred())

				defer func() { _ = client.Close() }()

				taskID := gonanoid.Must()

				container, err := client.RunContainer(
					context.Background(),
					orchestra.Task{
						ID:      taskID,
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

func TestParseDriverDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dsn            string
		expectedName   string
		expectedNS     string
		expectedParams map[string]string
		expectError    bool
	}{
		{
			name:           "simple driver name",
			dsn:            "docker",
			expectedName:   "docker",
			expectedNS:     "",
			expectedParams: map[string]string{},
		},
		{
			name:           "driver with parameters",
			dsn:            "k8s:namespace=my-ns,timeout=30",
			expectedName:   "k8s",
			expectedNS:     "",
			expectedParams: map[string]string{"namespace": "my-ns", "timeout": "30"},
		},
		{
			name:           "URL-style with namespace",
			dsn:            "k8s://my-namespace",
			expectedName:   "k8s",
			expectedNS:     "my-namespace",
			expectedParams: map[string]string{},
		},
		{
			name:           "URL-style with namespace and params",
			dsn:            "k8s://production?timeout=60&region=us-west",
			expectedName:   "k8s",
			expectedNS:     "production",
			expectedParams: map[string]string{"timeout": "60", "region": "us-west"},
		},
		{
			name:           "native driver",
			dsn:            "native",
			expectedName:   "native",
			expectedNS:     "",
			expectedParams: map[string]string{},
		},
		{
			name:           "driver with empty params",
			dsn:            "docker:",
			expectedName:   "docker",
			expectedNS:     "",
			expectedParams: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert := NewGomegaWithT(t)

			config, err := orchestra.ParseDriverDSN(tt.dsn)

			if tt.expectError {
				assert.Expect(err).To(HaveOccurred())
				return
			}

			assert.Expect(err).NotTo(HaveOccurred())
			assert.Expect(config.Name).To(Equal(tt.expectedName))
			assert.Expect(config.Namespace).To(Equal(tt.expectedNS))
			assert.Expect(config.Params).To(Equal(tt.expectedParams))
		})
	}
}

func TestGetFromDSN(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	t.Run("existing driver", func(t *testing.T) {
		config, init, err := orchestra.GetFromDSN("native")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(config.Name).To(Equal("native"))
		assert.Expect(init).NotTo(BeNil())
	})

	t.Run("non-existing driver", func(t *testing.T) {
		_, _, err := orchestra.GetFromDSN("nonexistent")
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("not found"))
	})

	t.Run("driver with params", func(t *testing.T) {
		config, init, err := orchestra.GetFromDSN("k8s:namespace=test")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(config.Name).To(Equal("k8s"))
		assert.Expect(config.Params).To(HaveKey("namespace"))
		assert.Expect(init).NotTo(BeNil())
	})
}
