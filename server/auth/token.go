package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// tokenPayload is the data encoded in an API token.
type tokenPayload struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	NickName string `json:"nick_name"`
	Provider string `json:"provider"`
	UserID   string `json:"user_id"`
	Expiry   int64  `json:"exp"`
}

// GenerateToken creates a signed API token for the given user.
// The token is base64(payload) + "." + hex(hmac-sha256(payload, secret)).
func GenerateToken(user *User, secret string, ttl time.Duration) (string, error) {
	payload := tokenPayload{
		Email:    user.Email,
		Name:     user.Name,
		NickName: user.NickName,
		Provider: user.Provider,
		UserID:   user.UserID,
		Expiry:   time.Now().Add(ttl).Unix(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("could not marshal token payload: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(data)
	sig := signHMAC(encoded, secret)

	return encoded + "." + sig, nil
}

// ValidateToken verifies a signed API token and returns the user.
// Returns an error if the signature is invalid or the token has expired.
func ValidateToken(token, secret string) (*User, error) {
	parts := splitToken(token)
	if parts == nil {
		return nil, errors.New("malformed token")
	}

	expectedSig := signHMAC(parts[0], secret)
	if !hmac.Equal([]byte(parts[1]), []byte(expectedSig)) {
		return nil, errors.New("invalid token signature")
	}

	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("could not decode token payload: %w", err)
	}

	var payload tokenPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("could not parse token payload: %w", err)
	}

	if time.Now().Unix() > payload.Expiry {
		return nil, errors.New("token expired")
	}

	return &User{
		Email:    payload.Email,
		Name:     payload.Name,
		NickName: payload.NickName,
		Provider: payload.Provider,
		UserID:   payload.UserID,
	}, nil
}

// TokenValidator returns a function that validates tokens using the given secret.
// Suitable for passing to RequireAuth as the tokenValidator parameter.
func TokenValidator(secret string) func(string) (*User, error) {
	return func(token string) (*User, error) {
		return ValidateToken(token, secret)
	}
}

func signHMAC(data, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))

	return hex.EncodeToString(mac.Sum(nil))
}

func splitToken(token string) []string {
	for i, c := range token {
		if c == '.' {
			if i > 0 && i < len(token)-1 {
				return []string{token[:i], token[i+1:]}
			}

			return nil
		}
	}

	return nil
}

// generateRandomCode creates a cryptographically random hex string for CLI device flow.
func generateRandomCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}
