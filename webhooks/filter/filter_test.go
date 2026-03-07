package filter_test

import (
	"testing"

	"github.com/jtarchie/pocketci/webhooks/filter"
	. "github.com/onsi/gomega"
)

func baseEnv() filter.WebhookEnv {
	return filter.WebhookEnv{
		Provider:  "github",
		EventType: "push",
		Method:    "POST",
		Headers:   map[string]string{"content-type": "application/json"},
		Query:     map[string]string{},
		Payload:   map[string]any{"ref": "refs/heads/main"},
	}
}

func TestEvaluate(t *testing.T) {
	t.Parallel()

	t.Run("true when expression matches provider", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		ok, err := filter.Evaluate(`provider == "github"`, baseEnv())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeTrue())
	})

	t.Run("false when expression does not match", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		ok, err := filter.Evaluate(`provider == "slack"`, baseEnv())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeFalse())
	})

	t.Run("matches on eventType", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		ok, err := filter.Evaluate(`eventType == "push"`, baseEnv())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeTrue())
		ok, err = filter.Evaluate(`eventType == "pull_request"`, baseEnv())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeFalse())
	})

	t.Run("matches on method", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		ok, err := filter.Evaluate(`method == "POST"`, baseEnv())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeTrue())
	})

	t.Run("matches on header value", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		ok, err := filter.Evaluate(`headers["content-type"] == "application/json"`, baseEnv())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeTrue())
	})

	t.Run("matches on query param", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		env := baseEnv()
		env.Query = map[string]string{"branch": "main"}
		ok, err := filter.Evaluate(`query["branch"] == "main"`, env)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeTrue())
	})

	t.Run("matches on nested payload field", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		ok, err := filter.Evaluate(`payload["ref"] == "refs/heads/main"`, baseEnv())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeTrue())
	})

	t.Run("compound and expression", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		ok, err := filter.Evaluate(`provider == "github" && eventType == "push"`, baseEnv())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeTrue())
		ok, err = filter.Evaluate(`provider == "github" && eventType == "pull_request"`, baseEnv())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeFalse())
	})

	t.Run("error on compile failure", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		_, err := filter.Evaluate("this is not valid expr !!!", baseEnv())
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("webhook_trigger compile error"))
	})

	t.Run("error when expression is not boolean", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		_, err := filter.Evaluate("provider", baseEnv())
		assert.Expect(err).To(HaveOccurred())
	})

	t.Run("nil payload is supported", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		env := baseEnv()
		env.Payload = nil
		ok, err := filter.Evaluate("payload == nil", env)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ok).To(BeTrue())
	})
}
