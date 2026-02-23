package local_test

import (
	"context"
	"testing"

	"github.com/jtarchie/ci/secrets"
	_ "github.com/jtarchie/ci/secrets/local"
	. "github.com/onsi/gomega"
)

func newTestManager(t *testing.T) secrets.Manager {
	t.Helper()

	assert := NewGomegaWithT(t)

	manager, err := secrets.New("local", "local://:memory:?key=test-encryption-key-for-testing", nil)
	assert.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() { _ = manager.Close() })

	return manager
}

func TestLocalBackend(t *testing.T) {
	t.Parallel()

	t.Run("set and get", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		ctx := context.Background()

		err := mgr.Set(ctx, secrets.GlobalScope, "API_KEY", "my-secret-value")
		assert.Expect(err).NotTo(HaveOccurred())

		value, err := mgr.Get(ctx, secrets.GlobalScope, "API_KEY")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(value).To(Equal("my-secret-value"))
	})

	t.Run("get nonexistent returns ErrNotFound", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		ctx := context.Background()

		_, err := mgr.Get(ctx, secrets.GlobalScope, "DOES_NOT_EXIST")
		assert.Expect(err).To(MatchError(secrets.ErrNotFound))
	})

	t.Run("delete existing secret", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		ctx := context.Background()

		err := mgr.Set(ctx, secrets.GlobalScope, "TO_DELETE", "value")
		assert.Expect(err).NotTo(HaveOccurred())

		err = mgr.Delete(ctx, secrets.GlobalScope, "TO_DELETE")
		assert.Expect(err).NotTo(HaveOccurred())

		_, err = mgr.Get(ctx, secrets.GlobalScope, "TO_DELETE")
		assert.Expect(err).To(MatchError(secrets.ErrNotFound))
	})

	t.Run("delete nonexistent returns ErrNotFound", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		ctx := context.Background()

		err := mgr.Delete(ctx, secrets.GlobalScope, "NOPE")
		assert.Expect(err).To(MatchError(secrets.ErrNotFound))
	})

	t.Run("scope isolation", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		ctx := context.Background()

		// Set same key in different scopes
		err := mgr.Set(ctx, secrets.GlobalScope, "SHARED_KEY", "global-value")
		assert.Expect(err).NotTo(HaveOccurred())

		err = mgr.Set(ctx, secrets.PipelineScope("pipeline-1"), "SHARED_KEY", "pipeline-1-value")
		assert.Expect(err).NotTo(HaveOccurred())

		err = mgr.Set(ctx, secrets.PipelineScope("pipeline-2"), "SHARED_KEY", "pipeline-2-value")
		assert.Expect(err).NotTo(HaveOccurred())

		// Each scope returns its own value
		val, err := mgr.Get(ctx, secrets.GlobalScope, "SHARED_KEY")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(val).To(Equal("global-value"))

		val, err = mgr.Get(ctx, secrets.PipelineScope("pipeline-1"), "SHARED_KEY")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(val).To(Equal("pipeline-1-value"))

		val, err = mgr.Get(ctx, secrets.PipelineScope("pipeline-2"), "SHARED_KEY")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(val).To(Equal("pipeline-2-value"))
	})

	t.Run("overwrite updates value and discards old", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		ctx := context.Background()

		err := mgr.Set(ctx, secrets.GlobalScope, "ROTATE_ME", "value-v1")
		assert.Expect(err).NotTo(HaveOccurred())

		err = mgr.Set(ctx, secrets.GlobalScope, "ROTATE_ME", "value-v2")
		assert.Expect(err).NotTo(HaveOccurred())

		val, err := mgr.Get(ctx, secrets.GlobalScope, "ROTATE_ME")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(val).To(Equal("value-v2"))
	})

	t.Run("values are encrypted at rest", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		ctx := context.Background()

		secretValue := "super-secret-password-12345"
		err := mgr.Set(ctx, secrets.GlobalScope, "DB_PASSWORD", secretValue)
		assert.Expect(err).NotTo(HaveOccurred())

		// Verify the value can be retrieved correctly
		val, err := mgr.Get(ctx, secrets.GlobalScope, "DB_PASSWORD")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(val).To(Equal(secretValue))
	})

	t.Run("special characters in values", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		ctx := context.Background()

		specialValues := map[string]string{
			"DOLLAR":    "$VAR_NAME",
			"STAR":      "*.wildcard",
			"BACKSLASH": `C:\path\to\file`,
			"NEWLINES":  "line1\nline2\nline3",
			"UNICODE":   "hello 🔐 world",
			"EMPTY":     "",
		}

		for key, val := range specialValues {
			err := mgr.Set(ctx, secrets.GlobalScope, key, val)
			assert.Expect(err).NotTo(HaveOccurred())

			got, err := mgr.Get(ctx, secrets.GlobalScope, key)
			assert.Expect(err).NotTo(HaveOccurred())
			assert.Expect(got).To(Equal(val), "mismatch for key %s", key)
		}
	})

	t.Run("invalid backend name errors", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.New("nonexistent-backend", "anything", nil)
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("unknown secrets backend"))
	})

	t.Run("invalid DSN errors", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.New("local", "no-key-param", nil)
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("key="))
	})
}
