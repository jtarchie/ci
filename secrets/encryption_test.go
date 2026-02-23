package secrets_test

import (
	"testing"

	"github.com/jtarchie/ci/secrets"
	. "github.com/onsi/gomega"
)

func TestEncryptor(t *testing.T) {
	t.Parallel()

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		key := secrets.DeriveKey("test-passphrase-for-encryption")
		enc, err := secrets.NewEncryptor(key)
		assert.Expect(err).NotTo(HaveOccurred())

		plaintext := []byte("hello secret world")
		ciphertext, err := enc.Encrypt(plaintext)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ciphertext).NotTo(Equal(plaintext))

		decrypted, err := enc.Decrypt(ciphertext)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(decrypted).To(Equal(plaintext))
	})

	t.Run("empty plaintext", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		key := secrets.DeriveKey("test-key")
		enc, err := secrets.NewEncryptor(key)
		assert.Expect(err).NotTo(HaveOccurred())

		ciphertext, err := enc.Encrypt([]byte(""))
		assert.Expect(err).NotTo(HaveOccurred())

		decrypted, err := enc.Decrypt(ciphertext)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(string(decrypted)).To(Equal(""))
	})

	t.Run("wrong key fails decryption", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		key1 := secrets.DeriveKey("key-one")
		key2 := secrets.DeriveKey("key-two")

		enc1, err := secrets.NewEncryptor(key1)
		assert.Expect(err).NotTo(HaveOccurred())

		enc2, err := secrets.NewEncryptor(key2)
		assert.Expect(err).NotTo(HaveOccurred())

		ciphertext, err := enc1.Encrypt([]byte("secret data"))
		assert.Expect(err).NotTo(HaveOccurred())

		_, err = enc2.Decrypt(ciphertext)
		assert.Expect(err).To(HaveOccurred())
	})

	t.Run("invalid key length", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		_, err := secrets.NewEncryptor([]byte("too-short"))
		assert.Expect(err).To(MatchError(secrets.ErrInvalidKey))
	})

	t.Run("ciphertext too short", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		key := secrets.DeriveKey("test-key")
		enc, err := secrets.NewEncryptor(key)
		assert.Expect(err).NotTo(HaveOccurred())

		_, err = enc.Decrypt([]byte("x"))
		assert.Expect(err).To(HaveOccurred())
	})

	t.Run("same plaintext produces different ciphertext", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		key := secrets.DeriveKey("test-key")
		enc, err := secrets.NewEncryptor(key)
		assert.Expect(err).NotTo(HaveOccurred())

		plaintext := []byte("same input")
		ct1, err := enc.Encrypt(plaintext)
		assert.Expect(err).NotTo(HaveOccurred())

		ct2, err := enc.Encrypt(plaintext)
		assert.Expect(err).NotTo(HaveOccurred())

		// Due to random nonce, same plaintext should produce different ciphertext
		assert.Expect(ct1).NotTo(Equal(ct2))
	})

	t.Run("DeriveKey is deterministic", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		key1 := secrets.DeriveKey("same-passphrase")
		key2 := secrets.DeriveKey("same-passphrase")
		assert.Expect(key1).To(Equal(key2))
		assert.Expect(key1).To(HaveLen(32))
	})
}
