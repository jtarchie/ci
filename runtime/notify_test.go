package runtime_test

import (
	"log/slog"
	"testing"

	"github.com/jtarchie/ci/runtime"
	. "github.com/onsi/gomega"
)

func TestNotifyRenderTemplate(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	logger := slog.Default()
	notifier := runtime.NewNotifier(logger)

	// Set up context
	notifier.SetContext(runtime.NotifyContext{
		PipelineName: "test-pipeline",
		JobName:      "test-job",
		BuildID:      "123",
		Status:       "success",
	})

	// Test basic template rendering (using struct field names, not JSON names)
	result, err := notifier.RenderTemplate("Build {{ .JobName }} completed with status {{ .Status }}")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result).To(Equal("Build test-job completed with status success"))

	// Test with Sprig functions
	result, err = notifier.RenderTemplate("Pipeline: {{ .PipelineName | upper }}")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result).To(Equal("Pipeline: TEST-PIPELINE"))
}

func TestNotifyContextUpdates(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	logger := slog.Default()
	notifier := runtime.NewNotifier(logger)

	// Set initial context
	notifier.SetContext(runtime.NotifyContext{
		PipelineName: "my-pipeline",
		JobName:      "job-1",
		Status:       "pending",
	})

	// Update job name using UpdateContext
	notifier.UpdateContext(func(ctx *runtime.NotifyContext) {
		ctx.JobName = "job-2"
	})

	result, err := notifier.RenderTemplate("Job: {{ .JobName }}")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result).To(Equal("Job: job-2"))

	// Update status
	notifier.UpdateContext(func(ctx *runtime.NotifyContext) {
		ctx.Status = "running"
	})

	result, err = notifier.RenderTemplate("Status: {{ .Status }}")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result).To(Equal("Status: running"))
}

func TestNotifyConfigLookup(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	logger := slog.Default()
	notifier := runtime.NewNotifier(logger)

	// Set configs using map
	notifier.SetConfigs(map[string]runtime.NotifyConfig{
		"slack-channel": {
			Type:     "slack",
			Token:    "xoxb-token",
			Channels: []string{"#builds"},
		},
		"teams-webhook": {
			Type:    "teams",
			Webhook: "https://teams.webhook.url",
		},
	})

	// Test config lookup by name
	config, exists := notifier.GetConfig("slack-channel")
	assert.Expect(exists).To(BeTrue())
	assert.Expect(config.Type).To(Equal("slack"))
	assert.Expect(config.Token).To(Equal("xoxb-token"))

	config, exists = notifier.GetConfig("teams-webhook")
	assert.Expect(exists).To(BeTrue())
	assert.Expect(config.Type).To(Equal("teams"))

	// Test missing config
	_, exists = notifier.GetConfig("nonexistent")
	assert.Expect(exists).To(BeFalse())
}

func TestNotifySetConfigs(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	logger := slog.Default()
	notifier := runtime.NewNotifier(logger)

	// Initially empty
	_, exists := notifier.GetConfig("test")
	assert.Expect(exists).To(BeFalse())

	// Set configs
	notifier.SetConfigs(map[string]runtime.NotifyConfig{
		"test": {
			Type: "http",
			URL:  "https://example.com/webhook",
		},
	})

	config, exists := notifier.GetConfig("test")
	assert.Expect(exists).To(BeTrue())
	assert.Expect(config.Type).To(Equal("http"))
}
