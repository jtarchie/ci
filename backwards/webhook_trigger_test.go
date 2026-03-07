package backwards_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jtarchie/pocketci/backwards"
	"github.com/jtarchie/pocketci/orchestra"
	_ "github.com/jtarchie/pocketci/orchestra/native"
	"github.com/jtarchie/pocketci/runtime"
	"github.com/jtarchie/pocketci/storage"
	_ "github.com/jtarchie/pocketci/storage/sqlite"
	. "github.com/onsi/gomega"
)

func setupNativeDriver(t *testing.T) orchestra.Driver {
	t.Helper()
	assert := NewGomegaWithT(t)
	driverConfig, initDriver, err := orchestra.GetFromDSN("native")
	assert.Expect(err).NotTo(HaveOccurred())
	driver, err := initDriver("ci-test", slog.Default(), driverConfig.Params)
	assert.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = driver.Close() })
	return driver
}

func setupStorage(t *testing.T) storage.Driver {
	t.Helper()
	assert := NewGomegaWithT(t)
	initStorage, found := storage.GetFromDSN("sqlite://:memory:")
	assert.Expect(found).To(BeTrue())
	store, err := initStorage("sqlite://:memory:", ":memory:", slog.Default())
	assert.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func runYAMLPipeline(t *testing.T, yamlContent string, webhookData *runtime.WebhookData) error {
	t.Helper()
	assert := NewGomegaWithT(t)

	pipeline, err := backwards.NewPipelineFromContent(yamlContent)
	assert.Expect(err).NotTo(HaveOccurred())

	driver := setupNativeDriver(t)
	store := setupStorage(t)

	js := runtime.NewJS(slog.Default())
	return js.ExecuteWithOptions(context.Background(), pipeline, driver, store, runtime.ExecuteOptions{
		WebhookData: webhookData,
	})
}

const simpleEchoYAML = `
jobs:
  - name: echo-job
    plan:
      - task: echo-task
        config:
          platform: linux
          run:
            path: echo
            args: ["hello"]
`

const webhookGatedYAML = `
jobs:
  - name: gated-job
    webhook_trigger: 'provider == "github"'
    plan:
      - task: echo-task
        config:
          platform: linux
          run:
            path: echo
            args: ["hello"]
`

const webhookGatedFailingYAML = `
jobs:
  - name: gated-failing-job
    webhook_trigger: 'provider == "github"'
    plan:
      - task: failing-task
        config:
          platform: linux
          run:
            path: sh
            args: ["-c", "exit 1"]
`

func TestWebhookTriggerYAML(t *testing.T) {
	t.Parallel()

	t.Run("job runs when no webhook_trigger set (manual trigger)", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		err := runYAMLPipeline(t, simpleEchoYAML, nil)
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("job runs when no webhook_trigger set (webhook trigger)", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		err := runYAMLPipeline(t, simpleEchoYAML, &runtime.WebhookData{
			Provider:  "slack",
			EventType: "message",
			Method:    "POST",
			Headers:   map[string]string{},
			Query:     map[string]string{},
			Body:      "{}",
		})
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("job runs when webhook_trigger matches", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		err := runYAMLPipeline(t, webhookGatedYAML, &runtime.WebhookData{
			Provider:  "github",
			EventType: "push",
			Method:    "POST",
			Headers:   map[string]string{},
			Query:     map[string]string{},
			Body:      "{}",
		})
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("job is skipped (not failed) when webhook_trigger does not match", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		// This job has a failing task, but the job should be SKIPPED because the
		// webhook provider is "slack" and the trigger requires "github".
		err := runYAMLPipeline(t, webhookGatedFailingYAML, &runtime.WebhookData{
			Provider:  "slack",
			EventType: "message",
			Method:    "POST",
			Headers:   map[string]string{},
			Query:     map[string]string{},
			Body:      "{}",
		})
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("job with webhook_trigger always runs on manual trigger (nil WebhookData)", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		// Manual triggers bypass webhook_trigger expressions.
		err := runYAMLPipeline(t, webhookGatedYAML, nil)
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("transpiled JS contains webhook_trigger field", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)
		js, err := backwards.NewPipelineFromContent(webhookGatedYAML)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(js).To(ContainSubstring("webhook_trigger"))
		// The YAML value is JSON-encoded in the JS bundle, so double quotes are escaped.
		assert.Expect(js).To(ContainSubstring(`provider == \"github\"`))
	})
}
