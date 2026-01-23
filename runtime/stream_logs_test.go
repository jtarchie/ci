package runtime_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jtarchie/ci/orchestra/docker"
	"github.com/jtarchie/ci/runtime"
	storage "github.com/jtarchie/ci/storage/sqlite"
	. "github.com/onsi/gomega"
)

func TestStreamLogsToStorage(t *testing.T) {
	t.Parallel()

	t.Run("streams logs to storage while container runs", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		// Create storage
		store, err := storage.NewSqlite("sqlite://:memory:", "stream-test", nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		logger := slog.Default()

		// Create docker driver
		driver, err := docker.NewDocker("stream-test-ns", logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		runID := "stream-test-run"
		storageKey := "/pipeline/test/jobs/streaming-job"

		// Create pipeline runner with storage
		runner := runtime.NewPipelineRunner(ctx, driver, store, logger, "stream-test-ns", runID)
		defer func() { _ = runner.CleanupVolumes() }()

		// Run a task that produces output with small delays
		// This simulates real-world streaming behavior
		result, err := runner.Run(runtime.RunInput{
			Name:  "streaming-task",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "sh",
				Args: []string{"-c", "echo line1; sleep 0.1; echo line2; sleep 0.1; echo line3"},
			},
			StorageKey: storageKey,
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.Status).To(Equal(runtime.RunComplete))
		assert.Expect(result.Code).To(Equal(0))

		// Verify final output contains all lines
		assert.Expect(result.Stdout).To(ContainSubstring("line1"))
		assert.Expect(result.Stdout).To(ContainSubstring("line2"))
		assert.Expect(result.Stdout).To(ContainSubstring("line3"))

		// Verify storage was updated
		payload, err := store.Get(ctx, storageKey)
		assert.Expect(err).NotTo(HaveOccurred())

		assert.Expect(payload).NotTo(BeNil())
		assert.Expect(payload["stdout"]).To(ContainSubstring("line1"))
		assert.Expect(payload["stdout"]).To(ContainSubstring("line2"))
		assert.Expect(payload["stdout"]).To(ContainSubstring("line3"))
	})

	t.Run("storage key is optional", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		// Create storage
		store, err := storage.NewSqlite("sqlite://:memory:", "stream-test", nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		logger := slog.Default()

		// Create docker driver
		driver, err := docker.NewDocker("stream-test-ns-optional", logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		runID := "stream-test-optional"

		// Create pipeline runner with storage
		runner := runtime.NewPipelineRunner(ctx, driver, store, logger, "stream-test-ns-optional", runID)
		defer func() { _ = runner.CleanupVolumes() }()

		// Run a task WITHOUT storage key - should still work
		result, err := runner.Run(runtime.RunInput{
			Name:  "no-streaming-task",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "echo",
				Args: []string{"hello without streaming"},
			},
			// No StorageKey provided
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.Status).To(Equal(runtime.RunComplete))
		assert.Expect(result.Stdout).To(ContainSubstring("hello without streaming"))

		// Verify no storage key was written (storage.Get should fail or return empty)
		_, err = store.Get(ctx, "/pipeline/test/jobs/no-streaming-task")
		assert.Expect(err).To(HaveOccurred()) // Should not exist
	})

	t.Run("handles container errors during streaming", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		// Create storage
		store, err := storage.NewSqlite("sqlite://:memory:", "stream-error-test", nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		logger := slog.Default()

		// Create docker driver
		driver, err := docker.NewDocker("stream-error-ns", logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		runID := "stream-error-run"
		storageKey := "/pipeline/test/jobs/error-job"

		// Create pipeline runner with storage
		runner := runtime.NewPipelineRunner(ctx, driver, store, logger, "stream-error-ns", runID)
		defer func() { _ = runner.CleanupVolumes() }()

		// Run a task that outputs then fails
		result, err := runner.Run(runtime.RunInput{
			Name:  "error-task",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "sh",
				Args: []string{"-c", "echo before-error; exit 1"},
			},
			StorageKey: storageKey,
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.Status).To(Equal(runtime.RunComplete))
		assert.Expect(result.Code).To(Equal(1))
		assert.Expect(result.Stdout).To(ContainSubstring("before-error"))

		// Verify storage captured output even with error
		payload, err := store.Get(ctx, storageKey)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(payload["stdout"]).To(ContainSubstring("before-error"))
	})

	t.Run("context cancellation stops streaming", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		// Create storage
		store, err := storage.NewSqlite("sqlite://:memory:", "stream-cancel-test", nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		logger := slog.Default()

		// Create docker driver with unique namespace
		uniqueNS := "stream-cancel-ns-" + time.Now().Format("150405")
		driver, err := docker.NewDocker(uniqueNS, logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		// Create a context that will be cancelled - give enough time for image pull
		// but not enough for the full 10 second sleep
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		runID := "stream-cancel-run-" + time.Now().Format("150405")
		storageKey := "/pipeline/test/jobs/cancel-job"

		// Create pipeline runner with storage
		runner := runtime.NewPipelineRunner(ctx, driver, store, logger, uniqueNS, runID)
		defer func() { _ = runner.CleanupVolumes() }()

		// Run a task that takes longer than the timeout
		result, err := runner.Run(runtime.RunInput{
			Name:  "cancel-task",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "sh",
				Args: []string{"-c", "echo before-cancel; sleep 10; echo after-cancel"},
			},
			StorageKey: storageKey,
		})

		// The task should be aborted due to context cancellation
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.Status).To(Equal(runtime.RunAbort))
		// Note: Partial output may or may not be captured depending on timing
		// The important assertion is that the run was aborted properly
	})
}
