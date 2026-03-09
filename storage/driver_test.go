package storage_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jtarchie/pocketci/storage"
	_ "github.com/jtarchie/pocketci/storage/sqlite"
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

			t.Run("Wildcard returns all fields", func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())

				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())

				defer func() { _ = client.Close() }()

				err = client.Set(context.Background(), "/bar", map[string]any{
					"field":   "123",
					"another": "456",
					"third":   "789",
				})
				assert.Expect(err).NotTo(HaveOccurred())

				results, err := client.GetAll(context.Background(), "/bar", []string{"*"})
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(results).To(HaveLen(1))
				assert.Expect(results[0].Path).To(Equal("/namespace/bar"))
				assert.Expect(results[0].Payload).To(Equal(storage.Payload{
					"field":   "123",
					"another": "456",
					"third":   "789",
				}))
			})

		})
	})
}
