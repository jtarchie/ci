package sqlite_test

import (
	"testing"

	"github.com/jtarchie/pocketci/secrets"
	_ "github.com/jtarchie/pocketci/secrets/sqlite"
	. "github.com/onsi/gomega"
)

func TestSQLiteBackend(t *testing.T) {
	t.Parallel()

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

		_, err := secrets.New("sqlite", "no-key-param", nil)
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("key="))
	})
}
