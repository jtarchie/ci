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

func TestDrivers(t *testing.T) {
	t.Parallel()

	storage.Each(func(name string, init storage.InitFunc) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("Add Path", func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())

				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())

				defer func() { _ = client.Close() }()

				err = client.Set(context.Background(), "/foo", map[string]string{
					"field":   "123",
					"another": "456",
				})
				assert.Expect(err).NotTo(HaveOccurred())

				results, err := client.GetAll(context.Background(), "/foo", []string{"field"})
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(results).To(HaveLen(1))
				assert.Expect(results[0].Path).To(Equal("/namespace/foo"))
				assert.Expect(results[0].Payload).To(Equal(storage.Payload{
					"field": "123",
				}))

				tree := results.AsTree()
				assert.Expect(tree).To(Equal(&storage.Tree[storage.Payload]{
					Name:     "namespace/foo",
					Children: nil,
					Value: storage.Payload{
						"field": "123",
					},
					FullPath: "/namespace/foo",
				}))
			})

			t.Run("resource versions", func(t *testing.T) {
				t.Parallel()

				t.Run("SaveResourceVersion creates new version", func(t *testing.T) {
					t.Parallel()

					assert := NewGomegaWithT(t)

					buildFile, err := os.CreateTemp(t.TempDir(), "")
					assert.Expect(err).NotTo(HaveOccurred())

					defer func() { _ = buildFile.Close() }()

					client, err := init(buildFile.Name(), "namespace", slog.Default())
					assert.Expect(err).NotTo(HaveOccurred())

					defer func() { _ = client.Close() }()

					version := map[string]string{"version": "v1"}
					rv, err := client.SaveResourceVersion(context.Background(), "test-resource", version, "test-job")
					assert.Expect(err).NotTo(HaveOccurred())
					assert.Expect(rv.ResourceName).To(Equal("test-resource"))
					assert.Expect(rv.Version).To(Equal(version))
					assert.Expect(rv.JobName).To(Equal("test-job"))
				})

				t.Run("GetLatestResourceVersion returns most recent by ID", func(t *testing.T) {
					t.Parallel()

					assert := NewGomegaWithT(t)

					buildFile, err := os.CreateTemp(t.TempDir(), "")
					assert.Expect(err).NotTo(HaveOccurred())

					defer func() { _ = buildFile.Close() }()

					client, err := init(buildFile.Name(), "namespace", slog.Default())
					assert.Expect(err).NotTo(HaveOccurred())

					defer func() { _ = client.Close() }()

					// Save multiple versions quickly (same timestamp)
					v1 := map[string]string{"version": "1"}
					_, err = client.SaveResourceVersion(context.Background(), "counter", v1, "job")
					assert.Expect(err).NotTo(HaveOccurred())

					v2 := map[string]string{"version": "2"}
					_, err = client.SaveResourceVersion(context.Background(), "counter", v2, "job")
					assert.Expect(err).NotTo(HaveOccurred())

					v3 := map[string]string{"version": "3"}
					_, err = client.SaveResourceVersion(context.Background(), "counter", v3, "job")
					assert.Expect(err).NotTo(HaveOccurred())

					// GetLatestResourceVersion should return v3 even if timestamps are identical
					latest, err := client.GetLatestResourceVersion(context.Background(), "counter")
					assert.Expect(err).NotTo(HaveOccurred())
					assert.Expect(latest.Version).To(Equal(v3))
				})

				t.Run("ListResourceVersions returns all versions", func(t *testing.T) {
					t.Parallel()

					assert := NewGomegaWithT(t)

					buildFile, err := os.CreateTemp(t.TempDir(), "")
					assert.Expect(err).NotTo(HaveOccurred())

					defer func() { _ = buildFile.Close() }()

					client, err := init(buildFile.Name(), "namespace", slog.Default())
					assert.Expect(err).NotTo(HaveOccurred())

					defer func() { _ = client.Close() }()

					v1 := map[string]string{"version": "1"}
					_, err = client.SaveResourceVersion(context.Background(), "resource", v1, "job")
					assert.Expect(err).NotTo(HaveOccurred())

					v2 := map[string]string{"version": "2"}
					_, err = client.SaveResourceVersion(context.Background(), "resource", v2, "job")
					assert.Expect(err).NotTo(HaveOccurred())

					versions, err := client.ListResourceVersions(context.Background(), "resource", 10)
					assert.Expect(err).NotTo(HaveOccurred())
					assert.Expect(versions).To(HaveLen(2))
				})

				t.Run("GetLatestResourceVersion returns ErrNotFound for unknown resource", func(t *testing.T) {
					t.Parallel()

					assert := NewGomegaWithT(t)

					buildFile, err := os.CreateTemp(t.TempDir(), "")
					assert.Expect(err).NotTo(HaveOccurred())

					defer func() { _ = buildFile.Close() }()

					client, err := init(buildFile.Name(), "namespace", slog.Default())
					assert.Expect(err).NotTo(HaveOccurred())

					defer func() { _ = client.Close() }()

					_, err = client.GetLatestResourceVersion(context.Background(), "non-existent")
					assert.Expect(err).To(Equal(storage.ErrNotFound))
				})

				t.Run("SaveResourceVersion with duplicate version updates job_name", func(t *testing.T) {
					t.Parallel()

					assert := NewGomegaWithT(t)

					buildFile, err := os.CreateTemp(t.TempDir(), "")
					assert.Expect(err).NotTo(HaveOccurred())

					defer func() { _ = buildFile.Close() }()

					client, err := init(buildFile.Name(), "namespace", slog.Default())
					assert.Expect(err).NotTo(HaveOccurred())

					defer func() { _ = client.Close() }()

					version := map[string]string{"version": "v1"}
					rv1, err := client.SaveResourceVersion(context.Background(), "resource", version, "job-1")
					assert.Expect(err).NotTo(HaveOccurred())

					// Save same version with different job name
					rv2, err := client.SaveResourceVersion(context.Background(), "resource", version, "job-2")
					assert.Expect(err).NotTo(HaveOccurred())

					// Should have same ID but updated job name
					assert.Expect(rv2.ID).To(Equal(rv1.ID))
					assert.Expect(rv2.JobName).To(Equal("job-2"))

					// Verify only one version exists
					versions, err := client.ListResourceVersions(context.Background(), "resource", 10)
					assert.Expect(err).NotTo(HaveOccurred())
					assert.Expect(versions).To(HaveLen(1))
				})
			})
		})
	})
}
