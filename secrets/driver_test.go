package secrets_test

import (
	"context"
	"log/slog"
	"os/exec"
	"testing"

	"github.com/jtarchie/pocketci/secrets"
	_ "github.com/jtarchie/pocketci/secrets/s3"
	_ "github.com/jtarchie/pocketci/secrets/sqlite"
	"github.com/jtarchie/pocketci/testhelpers"
	. "github.com/onsi/gomega"
)

func newSecretsManager(t *testing.T, name string, init secrets.InitFunc) secrets.Manager {
	t.Helper()

	var dsn string
	switch name {
	case "s3":
		if _, err := exec.LookPath("minio"); err != nil {
			t.Skip("minio not installed, skipping S3 secrets test")
		}

		server := testhelpers.StartMinIO(t)
		t.Cleanup(server.Stop)

		dsn = server.CacheURL() + "&encrypt=sse-s3&key=test-encryption-passphrase"
	case "sqlite":
		dsn = "sqlite://:memory:?key=test-encryption-key-for-testing"
	default:
		t.Skipf("unknown secrets driver: %s", name)
	}

	mgr, err := init(dsn, slog.Default())
	if err != nil {
		if name == "s3" {
			t.Skipf("S3 secrets SSE probe failed (MinIO may not support SSE without KMS): %v", err)
		}
		t.Fatalf("failed to initialize secrets backend: %v", err)
	}

	t.Cleanup(func() { _ = mgr.Close() })

	return mgr
}

func TestSecretDrivers(t *testing.T) {
	secrets.Each(func(name string, init secrets.InitFunc) {
		t.Run(name, func(t *testing.T) {
			t.Run("set and get", func(t *testing.T) {
				assert := NewGomegaWithT(t)
				mgr := newSecretsManager(t, name, init)
				ctx := context.Background()

				err := mgr.Set(ctx, secrets.GlobalScope, "API_KEY", "my-secret-value")
				assert.Expect(err).NotTo(HaveOccurred())

				value, err := mgr.Get(ctx, secrets.GlobalScope, "API_KEY")
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(value).To(Equal("my-secret-value"))
			})

			t.Run("get nonexistent returns ErrNotFound", func(t *testing.T) {
				assert := NewGomegaWithT(t)
				mgr := newSecretsManager(t, name, init)
				ctx := context.Background()

				_, err := mgr.Get(ctx, secrets.GlobalScope, "DOES_NOT_EXIST")
				assert.Expect(err).To(MatchError(secrets.ErrNotFound))
			})

			t.Run("delete existing secret", func(t *testing.T) {
				assert := NewGomegaWithT(t)
				mgr := newSecretsManager(t, name, init)
				ctx := context.Background()

				err := mgr.Set(ctx, secrets.GlobalScope, "TO_DELETE", "value")
				assert.Expect(err).NotTo(HaveOccurred())

				err = mgr.Delete(ctx, secrets.GlobalScope, "TO_DELETE")
				assert.Expect(err).NotTo(HaveOccurred())

				_, err = mgr.Get(ctx, secrets.GlobalScope, "TO_DELETE")
				assert.Expect(err).To(MatchError(secrets.ErrNotFound))
			})

			t.Run("delete nonexistent returns ErrNotFound", func(t *testing.T) {
				assert := NewGomegaWithT(t)
				mgr := newSecretsManager(t, name, init)
				ctx := context.Background()

				err := mgr.Delete(ctx, secrets.GlobalScope, "NOPE")
				assert.Expect(err).To(MatchError(secrets.ErrNotFound))
			})

			t.Run("scope isolation", func(t *testing.T) {
				assert := NewGomegaWithT(t)
				mgr := newSecretsManager(t, name, init)
				ctx := context.Background()

				err := mgr.Set(ctx, secrets.GlobalScope, "SHARED_KEY", "global-value")
				assert.Expect(err).NotTo(HaveOccurred())

				err = mgr.Set(ctx, secrets.PipelineScope("pipeline-1"), "SHARED_KEY", "pipeline-1-value")
				assert.Expect(err).NotTo(HaveOccurred())

				err = mgr.Set(ctx, secrets.PipelineScope("pipeline-2"), "SHARED_KEY", "pipeline-2-value")
				assert.Expect(err).NotTo(HaveOccurred())

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

			t.Run("overwrite updates value", func(t *testing.T) {
				assert := NewGomegaWithT(t)
				mgr := newSecretsManager(t, name, init)
				ctx := context.Background()

				err := mgr.Set(ctx, secrets.GlobalScope, "ROTATE_ME", "value-v1")
				assert.Expect(err).NotTo(HaveOccurred())

				err = mgr.Set(ctx, secrets.GlobalScope, "ROTATE_ME", "value-v2")
				assert.Expect(err).NotTo(HaveOccurred())

				val, err := mgr.Get(ctx, secrets.GlobalScope, "ROTATE_ME")
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(val).To(Equal("value-v2"))
			})

			t.Run("special characters in values", func(t *testing.T) {
				assert := NewGomegaWithT(t)
				mgr := newSecretsManager(t, name, init)
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

			t.Run("ListByScope returns all keys in a scope", func(t *testing.T) {
				assert := NewGomegaWithT(t)
				mgr := newSecretsManager(t, name, init)
				ctx := context.Background()

				scope := secrets.PipelineScope("list-test")

				err := mgr.Set(ctx, scope, "B_KEY", "val-b")
				assert.Expect(err).NotTo(HaveOccurred())

				err = mgr.Set(ctx, scope, "A_KEY", "val-a")
				assert.Expect(err).NotTo(HaveOccurred())

				err = mgr.Set(ctx, secrets.GlobalScope, "GLOBAL_KEY", "val-g")
				assert.Expect(err).NotTo(HaveOccurred())

				keys, err := mgr.ListByScope(ctx, scope)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(keys).To(Equal([]string{"A_KEY", "B_KEY"}))
			})

			t.Run("ListByScope returns nil for empty scope", func(t *testing.T) {
				assert := NewGomegaWithT(t)
				mgr := newSecretsManager(t, name, init)
				ctx := context.Background()

				keys, err := mgr.ListByScope(ctx, secrets.PipelineScope("nonexistent"))
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(keys).To(BeNil())
			})

			t.Run("DeleteByScope removes all secrets in scope", func(t *testing.T) {
				assert := NewGomegaWithT(t)
				mgr := newSecretsManager(t, name, init)
				ctx := context.Background()

				scope := secrets.PipelineScope("del-scope-test")

				err := mgr.Set(ctx, scope, "KEY1", "val1")
				assert.Expect(err).NotTo(HaveOccurred())

				err = mgr.Set(ctx, scope, "KEY2", "val2")
				assert.Expect(err).NotTo(HaveOccurred())

				err = mgr.Set(ctx, secrets.GlobalScope, "GLOBAL", "val-g")
				assert.Expect(err).NotTo(HaveOccurred())

				err = mgr.DeleteByScope(ctx, scope)
				assert.Expect(err).NotTo(HaveOccurred())

				keys, err := mgr.ListByScope(ctx, scope)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(keys).To(BeNil())

				val, err := mgr.Get(ctx, secrets.GlobalScope, "GLOBAL")
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(val).To(Equal("val-g"))
			})
		})
	})
}
