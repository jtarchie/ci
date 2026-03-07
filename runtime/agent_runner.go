package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	adktool "google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"

	genaianthropic "github.com/achetronic/adk-utils-go/genai/anthropic"
	genaiopenai "github.com/achetronic/adk-utils-go/genai/openai"
	"github.com/achetronic/adk-utils-go/plugin/contextguard"

	"github.com/jtarchie/pocketci/secrets"
	"github.com/jtarchie/pocketci/storage"
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
	Context          *AgentContext            `json:"context,omitempty"`
	// OnOutput is called with streaming chunks. Not serialised from JS.
	OnOutput OutputCallback `json:"-"`
	// Internal fields populated by Runtime.Agent() — not exposed to JS.
	storage     storage.Driver
	namespace   string
	runID       string
	pipelineID  string
	triggeredBy string
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

// AgentContextTask specifies a prior task whose output is pre-fetched into the
// agent's session history before the first turn.
type AgentContextTask struct {
	Name  string `json:"name"`
	Field string `json:"field,omitempty"` // "stdout" | "stderr" | "both" (default)
}

// AgentContext configures pre-fetched task outputs injected as synthetic tool
// call events before the agent's first turn, saving orientation tool calls.
type AgentContext struct {
	Tasks    []AgentContextTask `json:"tasks,omitempty"`
	MaxBytes int                `json:"max_bytes,omitempty"`
}

// taskSummary is the list_tasks tool output element.
type taskSummary struct {
	Name      string `json:"name"`
	Index     int    `json:"index"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at,omitempty"`
	Elapsed   string `json:"elapsed,omitempty"`
}

// listTasksOutput is the list_tasks tool result.
type listTasksOutput struct {
	Tasks []taskSummary `json:"tasks"`
}

// getTaskResultInput is the get_task_result tool input schema.
type getTaskResultInput struct {
	Name     string `json:"name"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

// getTaskResultOutput is the get_task_result tool result schema.
type getTaskResultOutput struct {
	Name      string `json:"name"`
	Index     int    `json:"index"`
	Status    string `json:"status"`
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	StartedAt string `json:"started_at,omitempty"`
	Elapsed   string `json:"elapsed,omitempty"`
	Truncated bool   `json:"truncated"`
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
func resolveModel(provider, modelName, apiKey string, llmCfg *AgentLLMConfig, thinkingCfg *AgentThinkingConfig) (adkmodel.LLM, error) {
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

// parseTaskStepID splits a stepID of the form "{index}-{name}" into its parts.
func parseTaskStepID(stepID string) (int, string) {
	idx := strings.IndexByte(stepID, '-')
	if idx < 0 {
		return -1, stepID
	}

	n, err := strconv.Atoi(stepID[:idx])
	if err != nil {
		return -1, stepID
	}

	return n, stepID[idx+1:]
}

// loadTaskSummaries fetches all task summaries for the given run from storage.
func loadTaskSummaries(ctx context.Context, st storage.Driver, runID string) ([]taskSummary, error) {
	prefix := "/pipeline/" + runID + "/tasks/"

	results, err := st.GetAll(ctx, prefix, []string{"status", "started_at", "elapsed"})
	if err != nil {
		return nil, fmt.Errorf("load tasks: %w", err)
	}

	tasks := make([]taskSummary, 0, len(results))

	for _, r := range results {
		stepID := path.Base(r.Path)
		idx, name := parseTaskStepID(stepID)
		t := taskSummary{Name: name, Index: idx}

		if s, ok := r.Payload["status"].(string); ok {
			t.Status = s
		}

		if s, ok := r.Payload["started_at"].(string); ok {
			t.StartedAt = s
		}

		if s, ok := r.Payload["elapsed"].(string); ok {
			t.Elapsed = s
		}

		tasks = append(tasks, t)
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Index < tasks[j].Index
	})

	return tasks, nil
}

// levenshtein computes the edit distance between two strings (case-insensitive).
func levenshtein(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)

	if len(a) == 0 {
		return len(b)
	}

	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)

	for j := range prev {
		prev[j] = j
	}

	for i, ca := range a {
		curr[0] = i + 1

		for j, cb := range b {
			cost := 1
			if ca == cb {
				cost = 0
			}

			curr[j+1] = min(curr[j]+1, min(prev[j+1]+1, prev[j]+cost))
		}

		prev, curr = curr, prev
	}

	return prev[len(b)]
}

// fuzzyFindTask returns the task whose name best matches the given query.
// Substring match is tried first; Levenshtein distance is used as a fallback.
func fuzzyFindTask(tasks []taskSummary, name string) (taskSummary, bool) {
	if len(tasks) == 0 {
		return taskSummary{}, false
	}

	lower := strings.ToLower(name)

	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.Name), lower) {
			return t, true
		}
	}

	// Levenshtein fallback.
	best := tasks[0]
	bestDist := levenshtein(tasks[0].Name, name)

	for _, t := range tasks[1:] {
		if d := levenshtein(t.Name, name); d < bestDist {
			bestDist = d
			best = t
		}
	}

	return best, true
}

// truncateStr shortens s to at most maxBytes bytes. Returns the (possibly
// truncated) string and a flag indicating whether truncation occurred.
func truncateStr(s string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s, false
	}

	return s[:maxBytes], true
}

// injectSyntheticToolCall appends a matched FunctionCall + FunctionResponse
// event pair into the session history before the agent's first turn. This lets
// the agent read the result as if it had called the tool itself, saving a turn.
func injectSyntheticToolCall(
	ctx context.Context,
	svc session.Service,
	sess session.Session,
	agentName, toolName string,
	args map[string]any,
	result map[string]any,
) error {
	callID := uuid.NewString()
	invocationID := uuid.NewString()

	// Model "calls" the tool.
	callEvent := session.NewEvent(invocationID)
	callEvent.Author = agentName
	callEvent.LLMResponse = adkmodel.LLMResponse{
		Content: &genai.Content{
			Role: genai.RoleModel,
			Parts: []*genai.Part{
				{
					FunctionCall: &genai.FunctionCall{
						ID:   callID,
						Name: toolName,
						Args: args,
					},
				},
			},
		},
	}

	if err := svc.AppendEvent(ctx, sess, callEvent); err != nil {
		return fmt.Errorf("append synthetic call event: %w", err)
	}

	// Tool returns the result.
	respEvent := session.NewEvent(invocationID)
	respEvent.Author = agentName
	respEvent.LLMResponse = adkmodel.LLMResponse{
		Content: &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{
					FunctionResponse: &genai.FunctionResponse{
						ID:       callID,
						Name:     toolName,
						Response: result,
					},
				},
			},
		},
	}
	respEvent.Actions.SkipSummarization = true

	if err := svc.AppendEvent(ctx, sess, respEvent); err != nil {
		return fmt.Errorf("append synthetic response event: %w", err)
	}

	return nil
}

// taskSummaryToMap converts a taskSummary to a map for use as a tool result.
func taskSummaryToMap(t taskSummary) map[string]any {
	m := map[string]any{
		"name":   t.Name,
		"index":  t.Index,
		"status": t.Status,
	}

	if t.StartedAt != "" {
		m["started_at"] = t.StartedAt
	}

	if t.Elapsed != "" {
		m["elapsed"] = t.Elapsed
	}

	return m
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

	// Build the system instruction describing the agent's environment (not the task).
	// The user's actual task prompt is sent as the first user message instead,
	// so the model receives it in the right context-window slot and there is no
	// duplication between Instruction and the user turn.
	var instrBuilder strings.Builder

	instrBuilder.WriteString("You are operating inside a CI/CD pipeline run.\n")
	instrBuilder.WriteString("\n")

	if config.Image != "" {
		fmt.Fprintf(&instrBuilder, "Container image: %s\n", config.Image)
	}

	if config.runID != "" {
		fmt.Fprintf(&instrBuilder, "Pipeline run ID: %s\n", config.runID)
	}

	if config.pipelineID != "" {
		fmt.Fprintf(&instrBuilder, "Pipeline ID: %s\n", config.pipelineID)
	}

	if config.triggeredBy != "" {
		fmt.Fprintf(&instrBuilder, "Triggered by: %s\n", config.triggeredBy)
	}

	if len(config.Mounts) > 0 {
		instrBuilder.WriteString("\nAvailable volumes:\n")

		for name, vol := range config.Mounts {
			fmt.Fprintf(&instrBuilder, "  - %s (at %s)\n", name, vol.Path)
		}
	}

	if agentWorkDir != "" {
		fmt.Fprintf(&instrBuilder, "\nWorking directory: %s\n", agentWorkDir)
	}

	instrBuilder.WriteString("\nTools available:\n")
	instrBuilder.WriteString("  - run_command: execute shell commands inside the container\n")
	instrBuilder.WriteString("  - list_tasks: list all tasks in the current run with their statuses (pre-fetched at start)\n")
	instrBuilder.WriteString("  - get_task_result: retrieve stdout, stderr, and exit code for a specific task by name\n")

	instruction := instrBuilder.String()

	// Build list_tasks tool — zero input, returns all tasks for the current run.
	listTasksTool, err := functiontool.New[struct{}, listTasksOutput](
		functiontool.Config{
			Name:        "list_tasks",
			Description: "List all tasks executed in the current pipeline run with their name, status, start time, and elapsed duration.",
		},
		func(_ adktool.Context, _ struct{}) (listTasksOutput, error) {
			if config.storage == nil || config.runID == "" {
				return listTasksOutput{}, nil
			}

			tasks, err := loadTaskSummaries(ctx, config.storage, config.runID)
			if err != nil {
				return listTasksOutput{}, err
			}

			return listTasksOutput{Tasks: tasks}, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("agent: failed to create list_tasks tool: %w", err)
	}

	// Build get_task_result tool — fuzzy name matching, returns full stdout/stderr/exit code.
	getTaskResultTool, err := functiontool.New[getTaskResultInput, getTaskResultOutput](
		functiontool.Config{
			Name:        "get_task_result",
			Description: "Retrieve the stdout, stderr, and exit code for a task in the current run. Use a partial or full task name; the closest match is returned.",
		},
		func(_ adktool.Context, input getTaskResultInput) (getTaskResultOutput, error) {
			if config.storage == nil || config.runID == "" {
				return getTaskResultOutput{}, fmt.Errorf("task storage not available")
			}

			summaries, err := loadTaskSummaries(ctx, config.storage, config.runID)
			if err != nil {
				return getTaskResultOutput{}, err
			}

			matched, ok := fuzzyFindTask(summaries, input.Name)
			if !ok {
				return getTaskResultOutput{}, fmt.Errorf("no tasks found in current run")
			}

			// Fetch full payload for the matched task.
			stepID := fmt.Sprintf("%d-%s", matched.Index, matched.Name)
			key := "/pipeline/" + config.runID + "/tasks/" + stepID

			payload, err := config.storage.Get(ctx, key)
			if err != nil {
				return getTaskResultOutput{}, fmt.Errorf("get task %q: %w", matched.Name, err)
			}

			maxBytes := input.MaxBytes
			if maxBytes <= 0 {
				maxBytes = 4096
			}

			out := getTaskResultOutput{
				Name:  matched.Name,
				Index: matched.Index,
			}

			if s, ok := payload["status"].(string); ok {
				out.Status = s
			}

			if v, ok := payload["code"].(float64); ok {
				out.ExitCode = int(v)
			}

			if s, ok := payload["started_at"].(string); ok {
				out.StartedAt = s
			}

			if s, ok := payload["elapsed"].(string); ok {
				out.Elapsed = s
			}

			stdout, _ := payload["stdout"].(string)
			stderr, _ := payload["stderr"].(string)

			var truncStdout, truncStderr bool

			out.Stdout, truncStdout = truncateStr(stdout, maxBytes)
			out.Stderr, truncStderr = truncateStr(stderr, maxBytes)
			out.Truncated = truncStdout || truncStderr

			return out, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("agent: failed to create get_task_result tool: %w", err)
	}

	// Create the ADK agent.
	genCfg := buildGenerateContentConfig(provider, config.LLM, config.Thinking, config.Safety)

	myAgent, err := llmagent.New(llmagent.Config{
		Name:                  config.Name,
		Model:                 llmModel,
		Description:           "An agent running in a CI/CD system with access to a containerized environment.",
		Instruction:           instruction,
		Tools:                 []adktool.Tool{runCmd, listTasksTool, getTaskResultTool},
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

	// Pre-inject a synthetic list_tasks result so the agent knows the run
	// state from turn 0 without spending a tool-call turn on orientation.
	if config.storage != nil && config.runID != "" {
		summaries, err := loadTaskSummaries(ctx, config.storage, config.runID)
		if err == nil && len(summaries) > 0 {
			taskMaps := make([]any, len(summaries))
			for i, t := range summaries {
				taskMaps[i] = taskSummaryToMap(t)
			}

			_ = injectSyntheticToolCall(
				ctx, sessionService, sessResp.Session,
				config.Name, "list_tasks",
				map[string]any{},
				map[string]any{"tasks": taskMaps},
			)
		}
	}

	// Pre-inject explicitly declared context tasks as get_task_result results.
	if config.Context != nil && config.storage != nil && config.runID != "" {
		maxBytes := config.Context.MaxBytes
		if maxBytes <= 0 {
			maxBytes = 4096
		}

		summaries, _ := loadTaskSummaries(ctx, config.storage, config.runID)

		for _, ct := range config.Context.Tasks {
			matched, ok := fuzzyFindTask(summaries, ct.Name)
			if !ok {
				continue
			}

			stepID := fmt.Sprintf("%d-%s", matched.Index, matched.Name)
			payload, err := config.storage.Get(ctx, "/pipeline/"+config.runID+"/tasks/"+stepID)
			if err != nil {
				continue
			}

			stdout, _ := payload["stdout"].(string)
			stderr, _ := payload["stderr"].(string)

			field := ct.Field
			if field == "" {
				field = "both"
			}

			switch field {
			case "stdout":
				stderr = ""
			case "stderr":
				stdout = ""
			}

			stdout, _ = truncateStr(stdout, maxBytes)
			stderr, _ = truncateStr(stderr, maxBytes)

			result := map[string]any{
				"name":  matched.Name,
				"index": matched.Index,
			}

			if s, ok := payload["status"].(string); ok {
				result["status"] = s
			}

			if v, ok := payload["code"].(float64); ok {
				result["exit_code"] = int(v)
			}

			if stdout != "" {
				result["stdout"] = stdout
			}

			if stderr != "" {
				result["stderr"] = stderr
			}

			_ = injectSyntheticToolCall(
				ctx, sessionService, sessResp.Session,
				config.Name, "get_task_result",
				map[string]any{"name": ct.Name},
				result,
			)
		}
	}

	// Run the agent, collecting the final text response, tool call history, and usage.
	// The user's task prompt is the first user message; the Instruction field
	// holds environment context so there is no duplication.
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
