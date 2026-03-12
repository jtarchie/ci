package s3_test

import (
	"testing"

	"github.com/jtarchie/pocketci/secrets"
	_ "github.com/jtarchie/pocketci/secrets/s3"
	. "github.com/onsi/gomega"
)

func TestS3Secrets_RequiresKey(t *testing.T) {
	t.Parallel()

	t.Run("missing key param returns error", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.New("s3", "s3://s3.amazonaws.com/test-bucket?region=us-east-1", nil)
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("key="))
	})

	t.Run("invalid encrypt value returns error", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.New("s3", "s3://s3.amazonaws.com/test-bucket?region=us-east-1&encrypt=INVALID&key=passphrase", nil)
		assert.Expect(err).To(HaveOccurred())
	})

	t.Run("invalid DSN scheme returns error", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.New("s3", "docker://example.com/bucket", nil)
		assert.Expect(err).To(HaveOccurred())
	})

	t.Run("no encrypt param is allowed (app-layer AES only)", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.New("s3", "s3://s3.amazonaws.com/test-bucket?region=us-east-1&key=passphrase", nil)
		if err != nil {
			assert.Expect(err.Error()).NotTo(ContainSubstring("sse="))
			assert.Expect(err.Error()).NotTo(ContainSubstring("requires sse"))
		}
	})
}
