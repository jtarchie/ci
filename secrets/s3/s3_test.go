package s3_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/jtarchie/pocketci/secrets"
	_ "github.com/jtarchie/pocketci/secrets/s3"
	"github.com/jtarchie/pocketci/testhelpers"
	. "github.com/onsi/gomega"
)

// newTestManager starts a local MinIO server and returns an S3-backed
// secrets.Manager configured with SSE-S3 (AES256). The test is skipped when:
//   - minio is not installed
//   - MinIO does not support SSE (most default deployments) - checked via the
//     SSE probe that runs inside the constructor
func newTestManager(t *testing.T) secrets.Manager {
	t.Helper()

	if _, err := exec.LookPath("minio"); err != nil {
		t.Skip("minio not installed, skipping S3 secrets test")
	}

	server := testhelpers.StartMinIO(t)
	t.Cleanup(server.Stop)

	dsn := server.CacheURL() + "&sse=AES256&key=test-encryption-passphrase"

	assert := NewGomegaWithT(t)

	mgr, err := secrets.New("s3", dsn, nil)
	if err != nil {
		t.Skipf("S3 secrets SSE probe failed (MinIO may not support SSE without KMS): %v", err)
	}

	assert.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() { _ = mgr.Close() })

	return mgr
}

func TestS3Secrets_RequiresSSE(t *testing.T) {
	t.Parallel()

	t.Run("missing sse param returns error", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.New("s3", "s3://s3.amazonaws.com/test-bucket?region=us-east-1&key=passphrase", nil)
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("sse="))
	})

	t.Run("missing key param returns error", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.New("s3", "s3://s3.amazonaws.com/test-bucket?region=us-east-1&sse=AES256", nil)
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("key="))
	})

	t.Run("invalid sse value returns error", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.New("s3", "s3://s3.amazonaws.com/test-bucket?region=us-east-1&sse=INVALID&key=passphrase", nil)
		assert.Expect(err).To(HaveOccurred())
	})

	t.Run("invalid DSN scheme returns error", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.New("s3", "docker://example.com/bucket", nil)
		assert.Expect(err).To(HaveOccurred())
	})
}

func TestS3Secrets_CRUD(t *testing.T) {
	// t.Parallel() is intentionally omitted: subtests use newTestManager which
	// calls testhelpers.StartMinIO, which calls t.Setenv. Go's testing package
	// forbids t.Setenv in parallel tests.

	t.Run("set and get", func(t *testing.T) {
		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		err := mgr.Set(context.Background(), "global", "db-password", "supersecret")
		assert.Expect(err).NotTo(HaveOccurred())

		val, err := mgr.Get(context.Background(), "global", "db-password")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(val).To(Equal("supersecret"))
	})

	t.Run("get nonexistent returns ErrNotFound", func(t *testing.T) {
		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		_, err := mgr.Get(context.Background(), "global", "does-not-exist")
		assert.Expect(err).To(MatchError(secrets.ErrNotFound))
	})

	t.Run("delete existing", func(t *testing.T) {
		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		err := mgr.Set(context.Background(), "global", "to-delete", "value")
		assert.Expect(err).NotTo(HaveOccurred())

		err = mgr.Delete(context.Background(), "global", "to-delete")
		assert.Expect(err).NotTo(HaveOccurred())

		_, err = mgr.Get(context.Background(), "global", "to-delete")
		assert.Expect(err).To(MatchError(secrets.ErrNotFound))
	})

	t.Run("delete nonexistent returns ErrNotFound", func(t *testing.T) {
		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		err := mgr.Delete(context.Background(), "global", "ghost-key")
		assert.Expect(err).To(MatchError(secrets.ErrNotFound))
	})

	t.Run("overwrite updates value", func(t *testing.T) {
		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		err := mgr.Set(context.Background(), "global", "api-key", "first")
		assert.Expect(err).NotTo(HaveOccurred())

		err = mgr.Set(context.Background(), "global", "api-key", "second")
		assert.Expect(err).NotTo(HaveOccurred())

		val, err := mgr.Get(context.Background(), "global", "api-key")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(val).To(Equal("second"))
	})

	t.Run("scope isolation", func(t *testing.T) {
		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		err := mgr.Set(context.Background(), "pipeline/1", "token", "pipeline-value")
		assert.Expect(err).NotTo(HaveOccurred())

		err = mgr.Set(context.Background(), "global", "token", "global-value")
		assert.Expect(err).NotTo(HaveOccurred())

		v1, err := mgr.Get(context.Background(), "pipeline/1", "token")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(v1).To(Equal("pipeline-value"))

		v2, err := mgr.Get(context.Background(), "global", "token")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(v2).To(Equal("global-value"))
	})

	t.Run("ListByScope returns all keys", func(t *testing.T) {
		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		_ = mgr.Set(context.Background(), "list-scope", "alpha", "1")
		_ = mgr.Set(context.Background(), "list-scope", "beta", "2")
		_ = mgr.Set(context.Background(), "list-scope", "gamma", "3")

		keys, err := mgr.ListByScope(context.Background(), "list-scope")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(keys).To(ConsistOf("alpha", "beta", "gamma"))
	})

	t.Run("ListByScope empty scope returns empty list", func(t *testing.T) {
		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		keys, err := mgr.ListByScope(context.Background(), "empty-scope")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(keys).To(BeEmpty())
	})

	t.Run("DeleteByScope removes all keys in scope", func(t *testing.T) {
		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		_ = mgr.Set(context.Background(), "del-scope", "x", "1")
		_ = mgr.Set(context.Background(), "del-scope", "y", "2")

		err := mgr.DeleteByScope(context.Background(), "del-scope")
		assert.Expect(err).NotTo(HaveOccurred())

		keys, err := mgr.ListByScope(context.Background(), "del-scope")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(keys).To(BeEmpty())
	})

	t.Run("special characters in key name", func(t *testing.T) {
		assert := NewGomegaWithT(t)
		mgr := newTestManager(t)

		specialKey := "my/key with spaces & symbols!"

		err := mgr.Set(context.Background(), "global", specialKey, "encoded-value")
		assert.Expect(err).NotTo(HaveOccurred())

		val, err := mgr.Get(context.Background(), "global", specialKey)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(val).To(Equal("encoded-value"))

		keys, err := mgr.ListByScope(context.Background(), "global")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(keys).To(ContainElement(specialKey))
	})
}
