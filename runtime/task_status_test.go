package runtime_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jtarchie/ci/orchestra/docker"
	"github.com/jtarchie/ci/runtime"
	sqliteStorage "github.com/jtarchie/ci/storage/sqlite"
	. "github.com/onsi/gomega"
)

func newTestStore(t *testing.T) *os.File {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "task-status-*.db")
	if err != nil {
		t.Fatal(err)
	}

	return f
}

func TestTaskStatusPersistence(t *testing.T) {
	t.Parallel()

	t.Run("successful task writes status to storage", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		dbFile := newTestStore(t)
		defer func() { _ = dbFile.Close() }()

		store, err := sqliteStorage.NewSqlite(dbFile.Name(), "ns", nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		logger := slog.Default()

		driver, err := docker.NewDocker("task-status-ns", logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		runID := "test-run-success"
		runner := runtime.NewPipelineRunner(ctx, driver, store, logger, "task-status-ns", runID)
		defer func() { _ = runner.CleanupVolumes() }()

		result, err := runner.Run(runtime.RunInput{
			Name:  "echo-task",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "echo",
				Args: []string{"hello world"},
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.Status).To(Equal(runtime.RunComplete))
		assert.Expect(result.Code).To(Equal(0))

		// Verify task status was persisted to storage via GetAll (same query the UI uses)
		results, err := store.GetAll(ctx, "/pipeline/"+runID+"/", []string{"status"})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(results).NotTo(BeEmpty())

		// Verify the individual task entry
		taskPath := "/pipeline/" + runID + "/tasks/0-echo-task"
		payload, err := store.Get(ctx, taskPath)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(payload["status"]).To(Equal("success"))
		assert.Expect(payload["stdout"]).To(ContainSubstring("hello world"))
		assert.Expect(payload["code"]).To(BeEquivalentTo(0))
	})

	t.Run("failed task writes failure status to storage", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		dbFile := newTestStore(t)
		defer func() { _ = dbFile.Close() }()

		store, err := sqliteStorage.NewSqlite(dbFile.Name(), "ns", nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		logger := slog.Default()

		driver, err := docker.NewDocker("task-fail-ns", logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		runID := "test-run-failure"
		runner := runtime.NewPipelineRunner(ctx, driver, store, logger, "task-fail-ns", runID)
		defer func() { _ = runner.CleanupVolumes() }()

		result, err := runner.Run(runtime.RunInput{
			Name:  "failing-task",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "sh",
				Args: []string{"-c", "echo 'some output'; exit 1"},
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.Status).To(Equal(runtime.RunComplete))
		assert.Expect(result.Code).To(Equal(1))

		// Verify task status was persisted as failure
		taskPath := "/pipeline/" + runID + "/tasks/0-failing-task"
		payload, err := store.Get(ctx, taskPath)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(payload["status"]).To(Equal("failure"))
		assert.Expect(payload["stdout"]).To(ContainSubstring("some output"))
		assert.Expect(payload["code"]).To(BeEquivalentTo(1))
	})

	t.Run("multiple tasks get unique storage keys", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		dbFile := newTestStore(t)
		defer func() { _ = dbFile.Close() }()

		store, err := sqliteStorage.NewSqlite(dbFile.Name(), "ns", nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		logger := slog.Default()

		driver, err := docker.NewDocker("task-multi-ns", logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		runID := "test-run-multi"
		runner := runtime.NewPipelineRunner(ctx, driver, store, logger, "task-multi-ns", runID)
		defer func() { _ = runner.CleanupVolumes() }()

		// Run first task
		result1, err := runner.Run(runtime.RunInput{
			Name:  "task-a",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "echo",
				Args: []string{"first"},
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result1.Code).To(Equal(0))

		// Run second task
		result2, err := runner.Run(runtime.RunInput{
			Name:  "task-b",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "echo",
				Args: []string{"second"},
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result2.Code).To(Equal(0))

		// Verify both tasks are in storage under the run prefix
		results, err := store.GetAll(ctx, "/pipeline/"+runID+"/", []string{"status"})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(results).To(HaveLen(2))

		// Verify each task has its own unique key
		payload1, err := store.Get(ctx, "/pipeline/"+runID+"/tasks/0-task-a")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(payload1["status"]).To(Equal("success"))
		assert.Expect(payload1["stdout"]).To(ContainSubstring("first"))

		payload2, err := store.Get(ctx, "/pipeline/"+runID+"/tasks/1-task-b")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(payload2["status"]).To(Equal("success"))
		assert.Expect(payload2["stdout"]).To(ContainSubstring("second"))
	})

	t.Run("tasks visible via GetAll for UI rendering", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		dbFile := newTestStore(t)
		defer func() { _ = dbFile.Close() }()

		store, err := sqliteStorage.NewSqlite(dbFile.Name(), "ns", nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		logger := slog.Default()

		driver, err := docker.NewDocker("task-ui-ns", logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		runID := "test-run-ui"
		runner := runtime.NewPipelineRunner(ctx, driver, store, logger, "task-ui-ns", runID)
		defer func() { _ = runner.CleanupVolumes() }()

		_, err = runner.Run(runtime.RunInput{
			Name:  "ui-task",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "echo",
				Args: []string{"visible in UI"},
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())

		// This is exactly the query the server uses in router.go for /runs/:id/tasks
		lookupPath := "/pipeline/" + runID + "/"
		results, err := store.GetAll(ctx, lookupPath, []string{"status"})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(results).NotTo(BeEmpty())

		// The tree rendering should produce a non-empty tree
		tree := results.AsTree()
		assert.Expect(tree).NotTo(BeNil())

		// Verify the result has a "status" field with correct value
		assert.Expect(results[0].Payload).To(HaveKey("status"))
		assert.Expect(results[0].Payload["status"]).To(Equal("success"))
	})

	t.Run("no storage writes when runID is empty", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		dbFile := newTestStore(t)
		defer func() { _ = dbFile.Close() }()

		store, err := sqliteStorage.NewSqlite(dbFile.Name(), "ns", nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		logger := slog.Default()

		driver, err := docker.NewDocker("task-noid-ns", logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		// No runID - should skip storage writes
		runner := runtime.NewPipelineRunner(ctx, driver, store, logger, "task-noid-ns", "")
		defer func() { _ = runner.CleanupVolumes() }()

		result, err := runner.Run(runtime.RunInput{
			Name:  "no-runid-task",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "echo",
				Args: []string{"should not be stored"},
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.Code).To(Equal(0))

		// Verify nothing was written to storage
		results, err := store.GetAll(ctx, "/pipeline/", []string{"status"})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(results).To(BeEmpty())
	})

	t.Run("no panic when storage is nil", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		ctx := context.Background()
		logger := slog.Default()

		driver, err := docker.NewDocker("task-nostorage-ns", logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		// nil storage - should not panic
		runner := runtime.NewPipelineRunner(ctx, driver, nil, logger, "task-nostorage-ns", "some-run")
		defer func() { _ = runner.CleanupVolumes() }()

		result, err := runner.Run(runtime.RunInput{
			Name:  "nil-storage-task",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "echo",
				Args: []string{"works without storage"},
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.Code).To(Equal(0))
		assert.Expect(result.Stdout).To(ContainSubstring("works without storage"))
	})

	t.Run("timed out task writes abort status", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		dbFile := newTestStore(t)
		defer func() { _ = dbFile.Close() }()

		store, err := sqliteStorage.NewSqlite(dbFile.Name(), "ns", nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		logger := slog.Default()

		// Use unique namespace to avoid container name conflicts from previous runs
		uniqueNS := "task-timeout-ns-" + time.Now().Format("150405")
		driver, err := docker.NewDocker(uniqueNS, logger, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = driver.Close() }()

		runID := "test-run-timeout-" + time.Now().Format("150405")
		runner := runtime.NewPipelineRunner(ctx, driver, store, logger, uniqueNS, runID)
		defer func() { _ = runner.CleanupVolumes() }()

		result, err := runner.Run(runtime.RunInput{
			Name:  "timeout-task",
			Image: "busybox",
			Command: struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
				User string   `json:"user"`
			}{
				Path: "sh",
				Args: []string{"-c", "echo before-timeout; sleep 30; echo after-timeout"},
			},
			Timeout: "2s",
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.Status).To(Equal(runtime.RunAbort))

		// Verify task status was persisted as abort
		taskPath := "/pipeline/" + runID + "/tasks/0-timeout-task"
		payload, err := store.Get(ctx, taskPath)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(payload["status"]).To(Equal("abort"))
	})
}
