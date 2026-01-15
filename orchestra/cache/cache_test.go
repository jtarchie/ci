package cache_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/jtarchie/ci/orchestra/cache"
	"github.com/onsi/gomega"
)

func TestCompressor(t *testing.T) {
	t.Parallel()

	assert := gomega.NewGomegaWithT(t)

	t.Run("zstd compressor", func(t *testing.T) {
		t.Parallel()

		compressor := cache.NewZstdCompressor(0)

		original := []byte("hello world, this is some test data that should compress")

		var compressed bytes.Buffer

		writer, err := compressor.Compress(&compressed)
		assert.Expect(err).NotTo(gomega.HaveOccurred())

		_, err = writer.Write(original)
		assert.Expect(err).NotTo(gomega.HaveOccurred())
		assert.Expect(writer.Close()).To(gomega.Succeed())

		reader, err := compressor.Decompress(&compressed)
		assert.Expect(err).NotTo(gomega.HaveOccurred())

		decompressed, err := io.ReadAll(reader)
		assert.Expect(err).NotTo(gomega.HaveOccurred())
		assert.Expect(reader.Close()).To(gomega.Succeed())

		assert.Expect(decompressed).To(gomega.Equal(original))
		assert.Expect(compressor.Extension()).To(gomega.Equal(".zst"))
	})

	t.Run("no compressor", func(t *testing.T) {
		t.Parallel()

		compressor := cache.NewCompressor("none")

		original := []byte("hello world")

		var buf bytes.Buffer

		writer, err := compressor.Compress(&buf)
		assert.Expect(err).NotTo(gomega.HaveOccurred())

		_, err = writer.Write(original)
		assert.Expect(err).NotTo(gomega.HaveOccurred())
		assert.Expect(writer.Close()).To(gomega.Succeed())

		assert.Expect(buf.Bytes()).To(gomega.Equal(original))
		assert.Expect(compressor.Extension()).To(gomega.Equal(""))
	})
}

type mockCacheStore struct {
	data map[string][]byte
}

func newMockCacheStore() *mockCacheStore {
	return &mockCacheStore{data: make(map[string][]byte)}
}

func (m *mockCacheStore) Restore(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := m.data[key]
	if !ok {
		return nil, nil
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockCacheStore) Persist(_ context.Context, key string, reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	m.data[key] = data

	return nil
}

func (m *mockCacheStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]

	return ok, nil
}

func (m *mockCacheStore) Delete(_ context.Context, key string) error {
	delete(m.data, key)

	return nil
}

func TestMockCacheStore(t *testing.T) {
	t.Parallel()

	assert := gomega.NewGomegaWithT(t)
	ctx := context.Background()

	store := newMockCacheStore()

	reader, err := store.Restore(ctx, "missing")
	assert.Expect(err).NotTo(gomega.HaveOccurred())
	assert.Expect(reader).To(gomega.BeNil())

	err = store.Persist(ctx, "test-key", bytes.NewReader([]byte("test data")))
	assert.Expect(err).NotTo(gomega.HaveOccurred())

	exists, err := store.Exists(ctx, "test-key")
	assert.Expect(err).NotTo(gomega.HaveOccurred())
	assert.Expect(exists).To(gomega.BeTrue())

	reader, err = store.Restore(ctx, "test-key")
	assert.Expect(err).NotTo(gomega.HaveOccurred())
	assert.Expect(reader).NotTo(gomega.BeNil())

	data, err := io.ReadAll(reader)
	assert.Expect(err).NotTo(gomega.HaveOccurred())
	assert.Expect(string(data)).To(gomega.Equal("test data"))

	err = store.Delete(ctx, "test-key")
	assert.Expect(err).NotTo(gomega.HaveOccurred())

	exists, err = store.Exists(ctx, "test-key")
	assert.Expect(err).NotTo(gomega.HaveOccurred())
	assert.Expect(exists).To(gomega.BeFalse())
}
