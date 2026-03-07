package backwards_test

import (
	"os"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/jtarchie/pocketci/backwards"
	. "github.com/onsi/gomega"
)

func TestAgentStepConfig(t *testing.T) {
	t.Parallel()

	t.Run("parses agent step YAML fields correctly", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		contents, err := os.ReadFile("testdata/agent-with-llm.yml")
		assert.Expect(err).NotTo(HaveOccurred())

		var config backwards.Config

		err = yaml.Unmarshal(contents, &config)
		assert.Expect(err).NotTo(HaveOccurred())

		assert.Expect(config.Jobs).To(HaveLen(1))

		step := config.Jobs[0].Plan[0]

		assert.Expect(step.Agent).To(Equal("review-code-agent"))
		assert.Expect(step.Model).To(ContainSubstring("gemini"))

		// LLM config
		assert.Expect(step.AgentLLM).NotTo(BeNil())
		assert.Expect(*step.AgentLLM.Temperature).To(BeNumerically("~", 0.2, 0.001))
		assert.Expect(step.AgentLLM.MaxTokens).To(Equal(int32(8192)))

		// Thinking config
		assert.Expect(step.AgentThinking).NotTo(BeNil())
		assert.Expect(step.AgentThinking.Budget).To(Equal(int32(10000)))
		assert.Expect(step.AgentThinking.Level).To(Equal("medium"))

		// Safety config
		assert.Expect(step.AgentSafety).NotTo(BeEmpty())
		assert.Expect(step.AgentSafety["harassment"]).To(Equal("block_none"))
		assert.Expect(step.AgentSafety["dangerous_content"]).To(Equal("block_none"))

		// Context guard config
		assert.Expect(step.AgentContextGuard).NotTo(BeNil())
		assert.Expect(step.AgentContextGuard.Strategy).To(Equal("threshold"))
		assert.Expect(step.AgentContextGuard.MaxTokens).To(Equal(100000))
	})

	t.Run("NewPipeline transpiles agent step with LLM config to JS", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		js, err := backwards.NewPipeline("testdata/agent-with-llm.yml")
		assert.Expect(err).NotTo(HaveOccurred())

		// The generated JS should include config fields so job_runner.ts can read them.
		assert.Expect(js).To(ContainSubstring("review-code-agent"))
		assert.Expect(js).To(ContainSubstring(`"temperature"`))
		assert.Expect(js).To(ContainSubstring(`"max_tokens"`))
		assert.Expect(js).To(ContainSubstring(`"thinking"`))
		assert.Expect(js).To(ContainSubstring(`"context_guard"`))
		assert.Expect(js).To(ContainSubstring(`"safety"`))
	})

	t.Run("agent step without LLM config is still valid", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		minimalYAML := `
jobs:
  - name: review
    plan:
      - agent: my-agent
        prompt: Do something
        model: openrouter/google/gemini-3.1-flash-lite-preview
        config:
          platform: linux
          image_resource:
            type: registry-image
            source: { repository: alpine }
          run:
            path: echo
`
		var config backwards.Config

		err := yaml.Unmarshal([]byte(minimalYAML), &config)
		assert.Expect(err).NotTo(HaveOccurred())

		step := config.Jobs[0].Plan[0]
		assert.Expect(step.Agent).To(Equal("my-agent"))
		assert.Expect(step.AgentLLM).To(BeNil())
		assert.Expect(step.AgentThinking).To(BeNil())
		assert.Expect(step.AgentSafety).To(BeEmpty())
		assert.Expect(step.AgentContextGuard).To(BeNil())
	})
}
