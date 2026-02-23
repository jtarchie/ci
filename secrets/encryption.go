package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// ErrInvalidKey is returned when the encryption key has an invalid length.
var ErrInvalidKey = errors.New("encryption key must be 32 bytes (AES-256)")

// Encryptor provides AES-256-GCM encryption and decryption.
type Encryptor struct {
	aead cipher.AEAD
}

// NewEncryptor creates an Encryptor from a 32-byte key.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("could not create cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("could not create GCM: %w", err)
	}

	return &Encryptor{aead: aead}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// The nonce is prepended to the ciphertext.
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())

	_, err := io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return nil, fmt.Errorf("could not generate nonce: %w", err)
	}

	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext that was encrypted with Encrypt.
// Expects nonce prepended to ciphertext.
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := e.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short: %w", errors.ErrUnsupported)
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	plaintext, err := e.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("could not decrypt: %w", err)
	}

	return plaintext, nil
}

// DeriveKey derives a 32-byte key from a passphrase using SHA-256.
// The passphrase should be high-entropy (e.g., a random string or generated key).
func DeriveKey(passphrase string) []byte {
	hash := sha256.Sum256([]byte(passphrase))

	return hash[:]
}
