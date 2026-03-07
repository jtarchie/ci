package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	adktool "google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"

	genaianthropic "github.com/achetronic/adk-utils-go/genai/anthropic"
	genaiopenai "github.com/achetronic/adk-utils-go/genai/openai"
	"github.com/achetronic/adk-utils-go/plugin/contextguard"

	"github.com/jtarchie/pocketci/secrets"
)

// AgentLLMConfig controls LLM generation parameters.
type AgentLLMConfig struct {
	Temperature *float32 `json:"temperature,omitempty"`
	MaxTokens   int32    `json:"max_tokens,omitempty"`
}

// AgentThinkingConfig enables extended thinking for supported models.
// Budget sets the maximum thinking tokens (>= 1024).
// Level is Gemini-specific: LOW | MEDIUM | HIGH | MINIMAL.
type AgentThinkingConfig struct {
	Budget int32  `json:"budget"`
	Level  string `json:"level,omitempty"`
}

// AgentContextGuardConfig enables context window management.
type AgentContextGuardConfig struct {
	Strategy  string `json:"strategy"`
	MaxTurns  int    `json:"max_turns,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

// AgentConfig is the configuration passed from JavaScript to runtime.agent().
type AgentConfig struct {
	Name             string                   `json:"name"`
	Prompt           string                   `json:"prompt"`
	Model            string                   `json:"model"`
	Image            string                   `json:"image"`
	Mounts           map[string]VolumeResult  `json:"mounts"`
	OutputVolumePath string                   `json:"outputVolumePath"`
	LLM              *AgentLLMConfig          `json:"llm,omitempty"`
	Thinking         *AgentThinkingConfig     `json:"thinking,omitempty"`
	Safety           map[string]string        `json:"safety,omitempty"`
	ContextGuard     *AgentContextGuardConfig `json:"context_guard,omitempty"`
	// OnOutput is called with streaming chunks. Not serialised from JS.
	OnOutput OutputCallback `json:"-"`
}

// AgentResult is returned to JavaScript after the agent completes.
type AgentResult struct {
	Text      string           `json:"text"`
	Status    string           `json:"status"` // "success" or "failure"
	ToolCalls []ToolCallRecord `json:"toolCalls"`
	Usage     AgentUsage       `json:"usage"`
}

// ToolCallRecord captures a single tool invocation and its result.
type ToolCallRecord struct {
	Name     string         `json:"name"`
	Args     map[string]any `json:"args,omitempty"`
	Result   map[string]any `json:"result,omitempty"`
	ExitCode int            `json:"exitCode,omitempty"`
}

// AgentUsage tracks cumulative token counts and request stats.
type AgentUsage struct {
	PromptTokens     int32 `json:"promptTokens"`
	CompletionTokens int32 `json:"completionTokens"`
	TotalTokens      int32 `json:"totalTokens"`
	LLMRequests      int   `json:"llmRequests"`
	ToolCallCount    int   `json:"toolCallCount"`
}

// runCommandInput is the tool schema for run_command.
type runCommandInput struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// runCommandOutput is the tool result schema for run_command.
type runCommandOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// defaultBaseURLs maps providers (that use the OpenAI-compatible API) to their base URLs.
var defaultBaseURLs = map[string]string{
	"openrouter": "https://openrouter.ai/api/v1",
	"ollama":     "http://localhost:11434/v1",
	"openai":     "https://api.openai.com/v1",
}

// splitModel splits "provider/model-name" into ("provider", "model-name").
// For example: "openrouter/google/gemini-3" → ("openrouter", "google/gemini-3").
func splitModel(model string) (provider, modelName string) {
	idx := strings.Index(model, "/")
	if idx < 0 {
		return model, model
	}

	return model[:idx], model[idx+1:]
}

// resolveSecret looks up a secret key in pipeline → global scope order.
// Falls back to the corresponding environment variable (PROVIDER_API_KEY) if not found.
func resolveSecret(ctx context.Context, sm secrets.Manager, pipelineID, key string) string {
	if sm != nil {
		if pipelineID != "" {
			val, err := sm.Get(ctx, secrets.PipelineScope(pipelineID), key)
			if err == nil {
				return val
			}
		}

		val, err := sm.Get(ctx, secrets.GlobalScope, key)
		if err == nil {
			return val
		}
	}

	return ""
}

// resolveModel builds an adk-compatible LLM model from provider + name + key.
// llmCfg sets temperature and output token limit for all providers.
// thinkingCfg provides Anthropic-specific extended thinking budget.
func resolveModel(provider, modelName, apiKey string, llmCfg *AgentLLMConfig, thinkingCfg *AgentThinkingConfig) (model.LLM, error) {
	switch provider {
	case "anthropic":
		cfg := genaianthropic.Config{
			APIKey:    apiKey,
			ModelName: modelName,
		}

		if llmCfg != nil && llmCfg.MaxTokens > 0 {
			cfg.MaxOutputTokens = int(llmCfg.MaxTokens)
		}

		if thinkingCfg != nil && thinkingCfg.Budget > 0 {
			cfg.ThinkingBudgetTokens = int(thinkingCfg.Budget)
			// Anthropic requires MaxOutputTokens > ThinkingBudgetTokens.
			// Default to 8192 if not explicitly set.
			if cfg.MaxOutputTokens == 0 {
				cfg.MaxOutputTokens = 8192
			}
		}

		return genaianthropic.New(cfg), nil
	default:
		// openrouter, openai, ollama, etc. all speak OpenAI-compatible API.
		baseURL := defaultBaseURLs[provider]
		if baseURL == "" {
			return nil, fmt.Errorf("unknown provider %q: set a base URL or use anthropic/openai/openrouter/ollama", provider)
		}

		return genaiopenai.New(genaiopenai.Config{
			APIKey:    apiKey,
			BaseURL:   baseURL,
			ModelName: modelName,
		}), nil
	}
}

// simpleRegistry is a fallback ModelRegistry for contextguard that returns
// conservative defaults when the model is not in a curated database.
type simpleRegistry struct{}

func (simpleRegistry) ContextWindow(_ string) int    { return 128000 }
func (simpleRegistry) DefaultMaxTokens(_ string) int { return 4096 }

// harmCategoryFromString maps a YAML harm category key to a genai.HarmCategory.
func harmCategoryFromString(s string) genai.HarmCategory {
	return genai.HarmCategory("HARM_CATEGORY_" + strings.ToUpper(s))
}

// harmThresholdFromString maps a YAML threshold value to a genai.HarmBlockThreshold.
func harmThresholdFromString(s string) genai.HarmBlockThreshold {
	upper := strings.ToUpper(s)
	// "off" → "OFF"; everything else needs the BLOCK_ prefix already present
	// in the canonical names (e.g. "block_none" → "BLOCK_NONE").
	switch upper {
	case "OFF":
		return genai.HarmBlockThreshold("OFF")
	default:
		return genai.HarmBlockThreshold(upper)
	}
}

// buildGenerateContentConfig constructs a genai.GenerateContentConfig from the
// agent config fields. Returns nil when no tuning is requested.
func buildGenerateContentConfig(provider string, llmCfg *AgentLLMConfig, thinkingCfg *AgentThinkingConfig, safety map[string]string) *genai.GenerateContentConfig {
	var gcc genai.GenerateContentConfig
	has := false

	if llmCfg != nil {
		if llmCfg.Temperature != nil {
			gcc.Temperature = llmCfg.Temperature
			has = true
		}

		if llmCfg.MaxTokens > 0 {
			gcc.MaxOutputTokens = llmCfg.MaxTokens
			has = true
		}
	}

	// For non-Anthropic providers, wire thinking via GenerateContentConfig.
	if thinkingCfg != nil && provider != "anthropic" {
		budget := thinkingCfg.Budget
		tc := &genai.ThinkingConfig{ThinkingBudget: &budget}

		if thinkingCfg.Level != "" {
			tc.ThinkingLevel = genai.ThinkingLevel(strings.ToUpper(thinkingCfg.Level))
		}

		gcc.ThinkingConfig = tc
		has = true
	}

	if len(safety) > 0 {
		for category, threshold := range safety {
			gcc.SafetySettings = append(gcc.SafetySettings, &genai.SafetySetting{
				Category:  harmCategoryFromString(category),
				Threshold: harmThresholdFromString(threshold),
			})
		}

		has = true
	}

	if !has {
		return nil
	}

	return &gcc
}

// RunAgent executes an LLM agent with a run_command tool backed by a sandbox container.
// It writes a result.json to outputVolumePath when the agent finishes.
func RunAgent(
	ctx context.Context,
	sandboxRunner Runner,
	sm secrets.Manager,
	pipelineID string,
	config AgentConfig,
) (*AgentResult, error) {
	provider, modelName := splitModel(config.Model)

	// Resolve API key: secrets (pipeline → global) then env var fallback.
	apiKey := resolveSecret(ctx, sm, pipelineID, "agent/"+provider)
	if apiKey == "" {
		envKey := strings.ToUpper(strings.ReplaceAll(provider, "-", "_")) + "_API_KEY"
		apiKey = os.Getenv(envKey)
	}

	// Start the sandbox container.
	sandbox, err := sandboxRunner.StartSandbox(SandboxInput{
		Image:  config.Image,
		Name:   config.Name,
		Mounts: config.Mounts,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: failed to start sandbox: %w", err)
	}

	defer func() { _ = sandbox.Close() }()

	// Build the run_command tool.
	// Determine a common workdir from the mounts so the agent can reference
	// files by relative path (e.g. "my-repo/main.go").
	agentWorkDir := ""
	if len(config.Mounts) > 0 {
		// Use the parent of the first mount path as workdir.
		// For Fly shared volumes: /workspace/my-repo → workdir /workspace
		// For Docker: /tmp/container/my-repo → workdir /tmp/container
		for _, vol := range config.Mounts {
			if vol.Path != "" {
				idx := strings.LastIndex(vol.Path, "/")
				if idx > 0 {
					agentWorkDir = vol.Path[:idx]
				}

				break
			}
		}
	}

	runCmd, err := functiontool.New[runCommandInput, runCommandOutput](
		functiontool.Config{
			Name:        "run_command",
			Description: "Run a shell command in the sandbox container. Returns stdout, stderr, and exit code.",
		},
		func(_ adktool.Context, input runCommandInput) (runCommandOutput, error) {
			var execInput ExecInput
			execInput.Command.Path = input.Command
			execInput.Command.Args = input.Args
			execInput.WorkDir = agentWorkDir
			execInput.OnOutput = config.OnOutput

			result, execErr := sandbox.Exec(execInput)
			if execErr != nil {
				return runCommandOutput{}, execErr
			}

			return runCommandOutput{
				Stdout:   result.Stdout,
				Stderr:   result.Stderr,
				ExitCode: result.Code,
			}, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("agent: failed to create run_command tool: %w", err)
	}

	// Resolve the LLM model.
	llmModel, err := resolveModel(provider, modelName, apiKey, config.LLM, config.Thinking)
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}

	// Build the instruction with mount path context so the LLM knows
	// where files are located.
	instruction := config.Prompt
	if len(config.Mounts) > 0 {
		instruction += "\n\nAvailable directories:\n"
		for name, vol := range config.Mounts {
			instruction += fmt.Sprintf("- %s (at %s)\n", name, vol.Path)
		}

		if agentWorkDir != "" {
			instruction += fmt.Sprintf("Working directory: %s\n", agentWorkDir)
		}
	}

	// Create the ADK agent.
	genCfg := buildGenerateContentConfig(provider, config.LLM, config.Thinking, config.Safety)

	myAgent, err := llmagent.New(llmagent.Config{
		Name:                  config.Name,
		Model:                 llmModel,
		Description:           "CI pipeline agent",
		Instruction:           instruction,
		Tools:                 []adktool.Tool{runCmd},
		GenerateContentConfig: genCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: failed to create agent: %w", err)
	}

	// Set up an in-memory session.
	sessionService := session.InMemoryService()

	sessResp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: "ci-agent",
		UserID:  "pipeline",
	})
	if err != nil {
		return nil, fmt.Errorf("agent: failed to create session: %w", err)
	}

	runnr, err := runner.New(runner.Config{
		AppName:        "ci-agent",
		Agent:          myAgent,
		SessionService: sessionService,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: failed to create runner: %w", err)
	}

	// Wire context guard plugin when requested.
	if config.ContextGuard != nil {
		cg := config.ContextGuard
		guard := contextguard.New(simpleRegistry{})

		var opts []contextguard.AgentOption

		switch cg.Strategy {
		case "sliding_window":
			if cg.MaxTurns > 0 {
				opts = append(opts, contextguard.WithSlidingWindow(cg.MaxTurns))
			} else {
				opts = append(opts, contextguard.WithSlidingWindow(30))
			}
		default: // threshold
			if cg.MaxTokens > 0 {
				opts = append(opts, contextguard.WithMaxTokens(cg.MaxTokens))
			}
		}

		guard.Add(config.Name, llmModel, opts...)

		pluginCfg := guard.PluginConfig()
		runnr, err = runner.New(runner.Config{
			AppName:        "ci-agent",
			Agent:          myAgent,
			SessionService: sessionService,
			PluginConfig:   pluginCfg,
		})
		if err != nil {
			return nil, fmt.Errorf("agent: failed to create runner with context guard: %w", err)
		}
	}

	// Run the agent, collecting the final text response, tool call history, and usage.
	userMsg := genai.NewContentFromText(config.Prompt, genai.RoleUser)

	var textBuilder strings.Builder
	var toolCalls []ToolCallRecord
	var usage AgentUsage

	// pendingCalls tracks in-flight function calls by ID so we can pair them
	// with the matching FunctionResponse later.
	pendingCalls := make(map[string]*ToolCallRecord)

	var runErr error

	for event, err := range runnr.Run(ctx, "pipeline", sessResp.Session.ID(), userMsg, agent.RunConfig{}) {
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				runErr = err
			}

			break
		}

		// Accumulate token usage from every LLM response.
		if event.UsageMetadata != nil {
			usage.PromptTokens += event.UsageMetadata.PromptTokenCount
			usage.CompletionTokens += event.UsageMetadata.CandidatesTokenCount
			usage.TotalTokens += event.UsageMetadata.TotalTokenCount
			usage.LLMRequests++
		}

		if event.Content == nil {
			continue
		}

		for _, part := range event.Content.Parts {
			// Track function calls (tool invocations by the model).
			if part.FunctionCall != nil {
				fc := part.FunctionCall
				record := &ToolCallRecord{
					Name: fc.Name,
					Args: fc.Args,
				}

				// Store by ID so we can attach the response later.
				if fc.ID != "" {
					pendingCalls[fc.ID] = record
				}

				toolCalls = append(toolCalls, *record)
				usage.ToolCallCount++
			}

			// Track function responses (tool results).
			if part.FunctionResponse != nil {
				fr := part.FunctionResponse
				if pending, ok := pendingCalls[fr.ID]; ok {
					pending.Result = fr.Response
					// Update the toolCalls slice entry.
					for i := len(toolCalls) - 1; i >= 0; i-- {
						if toolCalls[i].Name == pending.Name && toolCalls[i].Result == nil {
							toolCalls[i].Result = fr.Response
							break
						}
					}

					delete(pendingCalls, fr.ID)
				}
			}

			if part.Text == "" {
				continue
			}

			textBuilder.WriteString(part.Text)

			if config.OnOutput != nil {
				config.OnOutput("stdout", part.Text)
			}
		}
	}

	if runErr != nil {
		return nil, fmt.Errorf("agent: run failed: %w", runErr)
	}

	finalText := textBuilder.String()
	status := "success"

	// Write result.json to the output path inside the sandbox if configured.
	if config.OutputVolumePath != "" {
		resultData := map[string]string{"status": status, "text": finalText}
		data, _ := json.Marshal(resultData)

		// Create the directory and write via sandbox exec (the path is inside the container).
		writeCmd := fmt.Sprintf("mkdir -p %s && cat > %s/result.json",
			config.OutputVolumePath, config.OutputVolumePath)

		var execInput ExecInput
		execInput.Command.Path = "sh"
		execInput.Command.Args = []string{"-c", writeCmd}
		execInput.Stdin = string(data)

		_, _ = sandbox.Exec(execInput)
	}

	return &AgentResult{
		Text:      finalText,
		Status:    status,
		ToolCalls: toolCalls,
		Usage:     usage,
	}, nil
}
