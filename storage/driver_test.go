package storage_test

import (
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

				err = client.Set("/foo", map[string]string{
					"field":   "123",
					"another": "456",
				})
				assert.Expect(err).NotTo(HaveOccurred())

				results, err := client.GetAll("/foo", []string{"field"})
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
		})
	})
}
