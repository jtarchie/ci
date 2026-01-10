package storage_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jtarchie/ci/storage"
	_ "github.com/jtarchie/ci/storage/sqlite"
	. "github.com/onsi/gomega"
)

func TestPipelineRunStorage(t *testing.T) {
	t.Parallel()

	storage.Each(func(name string, init storage.InitFunc) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("SaveRun creates a new run with queued status", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				pipeline, err := client.SavePipeline(context.Background(), "test-pipeline", "console.log('hello');", "docker://")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(run).NotTo(BeNil())
				assert.Expect(run.ID).NotTo(BeEmpty())
				assert.Expect(run.PipelineID).To(Equal(pipeline.ID))
				assert.Expect(run.Status).To(Equal(storage.RunStatusQueued))
				assert.Expect(run.StartedAt).To(BeNil())
				assert.Expect(run.CompletedAt).To(BeNil())
				assert.Expect(run.ErrorMessage).To(BeEmpty())
				assert.Expect(run.CreatedAt).NotTo(BeZero())
			})

			t.Run("GetRun retrieves existing run", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				pipeline, err := client.SavePipeline(context.Background(), "my-pipeline", "export { pipeline };", "native://")
				assert.Expect(err).NotTo(HaveOccurred())

				saved, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				retrieved, err := client.GetRun(context.Background(), saved.ID)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(retrieved.ID).To(Equal(saved.ID))
				assert.Expect(retrieved.PipelineID).To(Equal(pipeline.ID))
				assert.Expect(retrieved.Status).To(Equal(storage.RunStatusQueued))
			})

			t.Run("GetRun returns error for non-existent ID", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				_, err = client.GetRun(context.Background(), "non-existent-id")
				assert.Expect(err).To(Equal(storage.ErrNotFound))
			})

			t.Run("UpdateRunStatus to running sets started_at", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				pipeline, err := client.SavePipeline(context.Background(), "pipeline", "content", "docker://")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				err = client.UpdateRunStatus(context.Background(), run.ID, storage.RunStatusRunning, "")
				assert.Expect(err).NotTo(HaveOccurred())

				updated, err := client.GetRun(context.Background(), run.ID)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(updated.Status).To(Equal(storage.RunStatusRunning))
				assert.Expect(updated.StartedAt).NotTo(BeNil())
				assert.Expect(updated.CompletedAt).To(BeNil())
			})

			t.Run("UpdateRunStatus to success sets completed_at", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				pipeline, err := client.SavePipeline(context.Background(), "pipeline", "content", "docker://")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				err = client.UpdateRunStatus(context.Background(), run.ID, storage.RunStatusRunning, "")
				assert.Expect(err).NotTo(HaveOccurred())

				err = client.UpdateRunStatus(context.Background(), run.ID, storage.RunStatusSuccess, "")
				assert.Expect(err).NotTo(HaveOccurred())

				updated, err := client.GetRun(context.Background(), run.ID)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(updated.Status).To(Equal(storage.RunStatusSuccess))
				assert.Expect(updated.CompletedAt).NotTo(BeNil())
				assert.Expect(updated.ErrorMessage).To(BeEmpty())
			})

			t.Run("UpdateRunStatus to failed sets error_message", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				pipeline, err := client.SavePipeline(context.Background(), "pipeline", "content", "docker://")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				err = client.UpdateRunStatus(context.Background(), run.ID, storage.RunStatusRunning, "")
				assert.Expect(err).NotTo(HaveOccurred())

				err = client.UpdateRunStatus(context.Background(), run.ID, storage.RunStatusFailed, "something went wrong")
				assert.Expect(err).NotTo(HaveOccurred())

				updated, err := client.GetRun(context.Background(), run.ID)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(updated.Status).To(Equal(storage.RunStatusFailed))
				assert.Expect(updated.CompletedAt).NotTo(BeNil())
				assert.Expect(updated.ErrorMessage).To(Equal("something went wrong"))
			})

			t.Run("UpdateRunStatus returns error for non-existent ID", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				err = client.UpdateRunStatus(context.Background(), "non-existent-id", storage.RunStatusRunning, "")
				assert.Expect(err).To(Equal(storage.ErrNotFound))
			})
		})
	})
}
