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

func TestPipelineStorage(t *testing.T) {
	t.Parallel()

	storage.Each(func(name string, init storage.InitFunc) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("SavePipeline creates a new pipeline", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				pipeline, err := client.SavePipeline(context.Background(), "test-pipeline", "console.log('hello');", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(pipeline).NotTo(BeNil())
				assert.Expect(pipeline.ID).NotTo(BeEmpty())
				assert.Expect(pipeline.Name).To(Equal("test-pipeline"))
				assert.Expect(pipeline.Content).To(Equal("console.log('hello');"))
				assert.Expect(pipeline.DriverDSN).To(Equal("docker://"))
				assert.Expect(pipeline.CreatedAt).NotTo(BeZero())
				assert.Expect(pipeline.UpdatedAt).NotTo(BeZero())
			})

			t.Run("GetPipeline retrieves existing pipeline", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				saved, err := client.SavePipeline(context.Background(), "my-pipeline", "export { pipeline };", "native://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				retrieved, err := client.GetPipeline(context.Background(), saved.ID)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(retrieved.ID).To(Equal(saved.ID))
				assert.Expect(retrieved.Name).To(Equal("my-pipeline"))
				assert.Expect(retrieved.Content).To(Equal("export { pipeline };"))
				assert.Expect(retrieved.DriverDSN).To(Equal("native://"))
			})

			t.Run("GetPipeline returns error for non-existent ID", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				_, err = client.GetPipeline(context.Background(), "non-existent-id")
				assert.Expect(err).To(Equal(storage.ErrNotFound))
			})

			t.Run("ListPipelines returns all pipelines", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				_, err = client.SavePipeline(context.Background(), "pipeline-1", "content1", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				_, err = client.SavePipeline(context.Background(), "pipeline-2", "content2", "native://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				result, err := client.ListPipelines(context.Background(), 1, 100)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(result.Items).To(HaveLen(2))
			})

			t.Run("ListPipelines returns empty slice when no pipelines", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				result, err := client.ListPipelines(context.Background(), 1, 100)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(result.Items).To(BeEmpty())
			})

			t.Run("DeletePipeline removes a pipeline", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				saved, err := client.SavePipeline(context.Background(), "to-delete", "content", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				err = client.DeletePipeline(context.Background(), saved.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				_, err = client.GetPipeline(context.Background(), saved.ID)
				assert.Expect(err).To(Equal(storage.ErrNotFound))
			})

			t.Run("DeletePipeline returns error for non-existent ID", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				err = client.DeletePipeline(context.Background(), "non-existent-id")
				assert.Expect(err).To(Equal(storage.ErrNotFound))
			})
		})
	})
}
