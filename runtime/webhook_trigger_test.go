package runtime_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jtarchie/ci/runtime"
	. "github.com/onsi/gomega"
)

func runWithWebhook(t *testing.T, src string, data *runtime.WebhookData) error {
	t.Helper()
	js := runtime.NewJS(slog.Default())
	return js.ExecuteWithOptions(context.Background(), src, nil, nil, runtime.ExecuteOptions{
		WebhookData: data,
	})
}

func TestWebhookTriggerGlobal(t *testing.T) {
	t.Parallel()

	t.Run("returns true for manual trigger (nil WebhookData)", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		err := runWithWebhook(t, `
			async function pipeline() {
				assert.equal(webhookTrigger('provider == "github"'), true);
			}
			export { pipeline };
		`, nil)
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("true when expression matches webhook", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		err := runWithWebhook(t, `
			async function pipeline() {
				assert.equal(webhookTrigger('provider == "github" && eventType == "push"'), true);
			}
			export { pipeline };
		`, &runtime.WebhookData{
			Provider:  "github",
			EventType: "push",
			Method:    "POST",
			Headers:   map[string]string{},
			Query:     map[string]string{},
			Body:      "{}",
		})
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("false when expression does not match webhook", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		err := runWithWebhook(t, `
			async function pipeline() {
				assert.equal(webhookTrigger('eventType == "pull_request"'), false);
			}
			export { pipeline };
		`, &runtime.WebhookData{
			Provider:  "github",
			EventType: "push",
			Method:    "POST",
			Headers:   map[string]string{},
			Query:     map[string]string{},
			Body:      "{}",
		})
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("can filter on nested JSON payload field", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		err := runWithWebhook(t, `
			async function pipeline() {
				assert.equal(webhookTrigger('payload["ref"] == "refs/heads/main"'), true);
			}
			export { pipeline };
		`, &runtime.WebhookData{
			Provider:  "github",
			EventType: "push",
			Method:    "POST",
			Headers:   map[string]string{},
			Query:     map[string]string{},
			Body:      `{"ref":"refs/heads/main"}`,
		})
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("false (not panic) on invalid expression", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		err := runWithWebhook(t, `
			async function pipeline() {
				assert.equal(webhookTrigger('not valid expr !!!'), false);
			}
			export { pipeline };
		`, &runtime.WebhookData{
			Provider:  "github",
			EventType: "push",
			Method:    "POST",
			Headers:   map[string]string{},
			Query:     map[string]string{},
			Body:      "{}",
		})
		assert.Expect(err).NotTo(HaveOccurred())
	})
}
