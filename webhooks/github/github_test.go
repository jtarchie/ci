package github_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jtarchie/ci/webhooks"
	_ "github.com/jtarchie/ci/webhooks/github"
)

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)

	return fmt.Sprintf("sha256=%x", mac.Sum(nil))
}

func TestGitHub_Match(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	req.Header.Set("X-GitHub-Event", "push")

	// No providers other than GitHub registered.
	event, err := webhooks.Detect(req, []byte("{}"), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Provider != "github" {
		t.Errorf("expected provider 'github', got %q", event.Provider)
	}

	if event.EventType != "push" {
		t.Errorf("expected eventType 'push', got %q", event.EventType)
	}
}

func TestGitHub_ValidSignature(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	secret := "mysecret"

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", sign(body, secret))

	event, err := webhooks.Detect(req, body, secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Provider != "github" {
		t.Errorf("expected provider 'github', got %q", event.Provider)
	}

	if event.EventType != "pull_request" {
		t.Errorf("expected eventType 'pull_request', got %q", event.EventType)
	}
}

func TestGitHub_InvalidSignature(t *testing.T) {
	body := []byte(`{"action":"opened"}`)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=badhex")

	_, err := webhooks.Detect(req, body, "mysecret")
	if err != webhooks.ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestGitHub_MissingSignatureWithSecret(t *testing.T) {
	body := []byte(`{}`)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	req.Header.Set("X-GitHub-Event", "push")
	// No X-Hub-Signature-256

	_, err := webhooks.Detect(req, body, "mysecret")
	if err != webhooks.ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}
