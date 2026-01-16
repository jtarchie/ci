package cache_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/phayes/freeport"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/orchestra/cache"
	_ "github.com/jtarchie/ci/orchestra/cache/s3"
	_ "github.com/jtarchie/ci/orchestra/docker"
	_ "github.com/jtarchie/ci/orchestra/native"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/onsi/gomega"
)

type minioServer struct {
	cmd      *exec.Cmd
	dataDir  string
	endpoint string
	bucket   string
}

func startMinIO(t *testing.T) *minioServer {
	t.Helper()

	assert := gomega.NewGomegaWithT(t)

	dataDir, err := os.MkdirTemp("", "minio-test-*")
	assert.Expect(err).NotTo(gomega.HaveOccurred())

	// Get a free port from the OS to avoid conflicts
	port, err := freeport.GetFreePort()
	assert.Expect(err).NotTo(gomega.HaveOccurred())
	endpoint := fmt.Sprintf("http://localhost:%d", port)

	cmd := exec.Command("minio", "server", dataDir, "--address", fmt.Sprintf(":%d", port), "--quiet")
	cmd.Env = append(os.Environ(),
		"MINIO_ROOT_USER=minioadmin",
		"MINIO_ROOT_PASSWORD=minioadmin",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Start()
	assert.Expect(err).NotTo(gomega.HaveOccurred())

	bucket := "testcache" + strings.ReplaceAll(strings.ToLower(gonanoid.Must()), "_", "")

	server := &minioServer{
		cmd:      cmd,
		dataDir:  dataDir,
		endpoint: endpoint,
		bucket:   bucket,
	}

	assert.Eventually(func() bool {
		bucketPath := dataDir + "/" + bucket
		if err := os.MkdirAll(bucketPath, 0755); err != nil {
			return false
		}
		return true
	}, "10s", "100ms").Should(gomega.BeTrue(), "MinIO should start")

	time.Sleep(500 * time.Millisecond)

	return server
}

func (m *minioServer) stop(t *testing.T) {
	t.Helper()

	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		_ = m.cmd.Wait()
	}

	if m.dataDir != "" {
		_ = os.RemoveAll(m.dataDir)
	}
}

func (m *minioServer) cacheURL() string {
	return fmt.Sprintf("s3://%s?endpoint=%s&region=us-east-1", m.bucket, m.endpoint)
}

func TestCacheIntegration(t *testing.T) {
	if _, err := exec.LookPath("minio"); err != nil {
		t.Skip("minio not installed, skipping integration test")
	}

	t.Setenv("AWS_ACCESS_KEY_ID", "minioadmin")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "minioadmin")

	drivers := []string{"native", "docker"}

	for _, driverName := range drivers {
		t.Run(driverName, func(t *testing.T) {
			assert := gomega.NewGomegaWithT(t)
			ctx := context.Background()
			logger := slog.Default()

			minio := startMinIO(t)
			defer minio.stop(t)

			initFunc, ok := orchestra.Get(driverName)
			assert.Expect(ok).To(gomega.BeTrue(), "driver should exist")

			t.Run("cache persists volume data across runs", func(t *testing.T) {
				volumeName := "cache-test-vol"
				mountPath := "/cachevol"
				testData := "cached-data-" + gonanoid.Must()

				namespace1 := "cache-test-1-" + gonanoid.Must()
				driver1, err := initFunc(namespace1, logger, map[string]string{})
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				cacheParams := map[string]string{
					"cache":             minio.cacheURL(),
					"cache_compression": "zstd",
					"cache_prefix":      "integration-test",
				}
				driver1, err = cache.WrapWithCaching(driver1, cacheParams, logger)
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				vol1, err := driver1.CreateVolume(ctx, volumeName, 0)
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				taskID1 := gonanoid.Must()
				container1, err := driver1.RunContainer(ctx, orchestra.Task{
					ID:      taskID1,
					Image:   "busybox",
					Command: []string{"sh", "-c", fmt.Sprintf("echo '%s' > .%s/data.txt", testData, mountPath)},
					Mounts: orchestra.Mounts{
						{Name: volumeName, Path: mountPath},
					},
				})
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				assert.Eventually(func() bool {
					status, err := container1.Status(ctx)
					if err != nil {
						return false
					}
					return status.IsDone() && status.ExitCode() == 0
				}, "30s", "100ms").Should(gomega.BeTrue(), "container should complete successfully")

				// Cleanup container before volume
				err = container1.Cleanup(ctx)
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				err = vol1.Cleanup(ctx)
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				err = driver1.Close()
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				namespace2 := "cache-test-2-" + gonanoid.Must()
				driver2, err := initFunc(namespace2, logger, map[string]string{})
				assert.Expect(err).NotTo(gomega.HaveOccurred())
				defer func() { _ = driver2.Close() }()

				driver2, err = cache.WrapWithCaching(driver2, cacheParams, logger)
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				vol2, err := driver2.CreateVolume(ctx, volumeName, 0)
				assert.Expect(err).NotTo(gomega.HaveOccurred())
				defer func() { _ = vol2.Cleanup(ctx) }()

				taskID2 := gonanoid.Must()
				container2, err := driver2.RunContainer(ctx, orchestra.Task{
					ID:      taskID2,
					Image:   "busybox",
					Command: []string{"cat", "." + mountPath + "/data.txt"},
					Mounts: orchestra.Mounts{
						{Name: volumeName, Path: mountPath},
					},
				})
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				assert.Eventually(func() bool {
					status, err := container2.Status(ctx)
					if err != nil {
						return false
					}
					return status.IsDone() && status.ExitCode() == 0
				}, "30s", "100ms").Should(gomega.BeTrue(), "container should complete successfully")

				assert.Eventually(func() bool {
					stdout := &strings.Builder{}
					stderr := &strings.Builder{}
					_ = container2.Logs(ctx, stdout, stderr)
					return strings.Contains(stdout.String(), testData)
				}, "10s", "100ms").Should(gomega.BeTrue(), "cached data should be restored")
			})

			t.Run("cache miss on first run", func(t *testing.T) {
				volumeName := "fresh-vol-" + gonanoid.Must()
				mountPath := "/freshvol"

				namespace := "cache-miss-" + gonanoid.Must()
				driver, err := initFunc(namespace, logger, map[string]string{})
				assert.Expect(err).NotTo(gomega.HaveOccurred())
				defer func() { _ = driver.Close() }()

				cacheParams := map[string]string{
					"cache":             minio.cacheURL(),
					"cache_compression": "zstd",
				}
				driver, err = cache.WrapWithCaching(driver, cacheParams, logger)
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				vol, err := driver.CreateVolume(ctx, volumeName, 0)
				assert.Expect(err).NotTo(gomega.HaveOccurred())
				defer func() { _ = vol.Cleanup(ctx) }()

				assert.Expect(vol.Name()).To(gomega.Equal(volumeName))

				taskID := gonanoid.Must()
				container, err := driver.RunContainer(ctx, orchestra.Task{
					ID:      taskID,
					Image:   "busybox",
					Command: []string{"sh", "-c", fmt.Sprintf("echo 'new data' > .%s/test.txt && cat .%s/test.txt", mountPath, mountPath)},
					Mounts: orchestra.Mounts{
						{Name: volumeName, Path: mountPath},
					},
				})
				assert.Expect(err).NotTo(gomega.HaveOccurred())

				assert.Eventually(func() bool {
					status, err := container.Status(ctx)
					if err != nil {
						return false
					}
					return status.IsDone() && status.ExitCode() == 0
				}, "30s", "100ms").Should(gomega.BeTrue())

				assert.Eventually(func() bool {
					stdout := &strings.Builder{}
					stderr := &strings.Builder{}
					_ = container.Logs(ctx, stdout, stderr)
					return strings.Contains(stdout.String(), "new data")
				}, "10s", "100ms").Should(gomega.BeTrue())
			})
		})
	}
}

func TestCacheWithoutCachingEnabled(t *testing.T) {
	t.Parallel()

	assert := gomega.NewGomegaWithT(t)
	ctx := context.Background()
	logger := slog.Default()

	initFunc, ok := orchestra.Get("native")
	assert.Expect(ok).To(gomega.BeTrue())

	namespace := "no-cache-" + gonanoid.Must()
	driver, err := initFunc(namespace, logger, map[string]string{})
	assert.Expect(err).NotTo(gomega.HaveOccurred())
	defer func() { _ = driver.Close() }()

	emptyParams := map[string]string{}
	wrappedDriver, err := cache.WrapWithCaching(driver, emptyParams, logger)
	assert.Expect(err).NotTo(gomega.HaveOccurred())

	assert.Expect(wrappedDriver.Name()).To(gomega.Equal(driver.Name()))

	vol, err := wrappedDriver.CreateVolume(ctx, "test-vol", 0)
	assert.Expect(err).NotTo(gomega.HaveOccurred())
	defer func() { _ = vol.Cleanup(ctx) }()

	assert.Expect(vol.Name()).To(gomega.Equal("test-vol"))
}
