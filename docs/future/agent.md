# Agent Steps in CI Pipelines

## Overview

Add AI agent steps to pipelines using
[Google's ADK](https://github.com/google/adk-go) with
[adk-utils-go](https://github.com/achetronic/adk-utils-go) for multi-provider
model support (Anthropic, Google, OpenAI, OpenRouter, any OpenAI-compatible
endpoint). Agents debug failures, analyze output, and perform multi-step tasks
by running one-off containers. Bots (Slack, PR review, etc.) are just TypeScript
pipelines triggered by the existing webhook system.

**Design principle**: Minimal config for common cases, optional overrides for
advanced use.

## Core Primitive

A single new step type: `agent`. It gets:

- **Prompt**: Explicit string or shorthand (`debug`, `review`, `analyze`)
- **Built-in tools** (always available):
  - `run_container` — one-off container → `{ stdout, stderr, exitCode }`
    (reuses `runtime.run()`)
  - `get_step_result` — reads a previous step's output by name (smart
    truncation: first 4KB + last 60KB, configurable)
  - `conclude` — ends the agent with `pass`/`fail` status + summary. If not
    called, verdict is **auto-inferred** from the last model response.
- **MCP toolsets**: Optional external MCP servers
- **Tool reuse**: Tools/MCP servers defined at job level or server level,
  referenced by name
- **Workspace volume**: Opt-in shared volume (`workspace: true`) mounted at
  `/workspace` across all `run_container` calls

## Non-Goals

- Long-running Go bot infrastructure — bot logic stays in TypeScript
- New event-loop primitives — the existing webhook system handles inbound events
- Changes to the Container interface — `run_container` uses the existing
  one-shot container flow

## YAML Syntax

### Minimal (90% use case)

```yaml
jobs:
  - name: debug-test
    plan:
      - task: test
        config:
          platform: linux
          image_resource:
            type: registry-image
            source: { repository: golang, tag: "1.22" }
          run:
            path: go
            args: ["test", "./..."]

      - agent:
          prompt: debug
          model: anthropic/claude-sonnet-4
```

Prompt shorthands expand to well-crafted system prompts:

| Shorthand | Expands to                                                           |
| --------- | -------------------------------------------------------------------- |
| `debug`   | Investigate the failure, run diagnostics, identify root cause.       |
| `review`  | Review code changes, run linters/tests, provide actionable feedback. |
| `analyze` | Analyze the previous step output, summarize findings.                |

Custom strings are used verbatim.

### With Overrides

```yaml
- agent: investigate
  prompt: "Analyze the memory leak."
  model: openrouter/anthropic/claude-sonnet-4
  image: golang:1.22-debug
  params: { DEBUG: "true" }
  workspace: true
  max_steps: 50
  max_tokens: 200000
  truncate_head: 8192
  truncate_tail: 131072
  timeout: 30m
```

### Named Container Tools and MCP Servers

```yaml
- agent: complex-analysis
  model: google/gemini-2.0-flash
  prompt: |
    Run tests in Go, Python, and Node. Cross-reference with Snyk.
  tools:
    - container:
        name: golang_runner
        image: golang:1.22
        description: Run Go commands
    - container:
        name: python_runner
        image: python:3.12
        description: Run Python commands
  mcp_servers:
    - type: http
      url: https://mcp.snyk.io/sse
      auth: $SNYK_TOKEN
  max_steps: 100
```

Named tools expose a shorthand — `golang_runner({ command: "go test ./..." })`
pre-fills the image.

### Tool Reuse

**Job-level**: Define once, reference by name across agent steps:

```yaml
jobs:
  - name: audit
    tools:
      golang_runner: { image: "golang:1.22", description: "Run Go commands" }
    mcp_servers:
      snyk: { type: http, url: "https://mcp.snyk.io/sse", auth: "$SNYK_TOKEN" }
    plan:
      - agent: debug
        model: anthropic/claude-sonnet-4
        prompt: debug
        tools: [golang_runner]
        mcp_servers: [snyk]

      - agent: scan
        model: anthropic/claude-sonnet-4
        prompt: "Scan Go dependencies for CVEs."
        tools: [golang_runner]
        mcp_servers: [snyk]
```

**Server-level**: Register globally via CLI flags:

```bash
ci server \
  --global-tool "linter:golangci/golangci-lint:latest:Run Go linter" \
  --global-mcp "github:stdio:npx:-y,@github/github-mcp-server"
```

Then reference by name in any pipeline: `tools: [linter]`.

**Resolution order** (innermost wins): inline → job-level → server-level.

## TypeScript API

### Minimal

```typescript
const pipeline = async () => {
  await runtime.run({
    name: "test",
    image: "golang:1.22",
    command: { path: "go", args: ["test", "./..."] },
  });

  let analysis = await runtime.agent({
    prompt: "debug",
    model: "anthropic/claude-sonnet-4",
  });

  console.log(analysis.status); // "pass" or "fail"
  console.log(analysis.text);
};
export { pipeline };
```

### With Overrides

```typescript
let analysis = await runtime.agent({
  prompt: "Analyze the memory leak in detail.",
  model: "openrouter/anthropic/claude-sonnet-4",
  image: "golang:1.22-debug",
  env: { DEBUG: "true" },
  workspace: true,
  maxSteps: 50,
  maxTokens: 200000,
  timeout: "30m",
  tools: [
    { type: "container", name: "linter", image: "golangci/golangci-lint:latest",
      description: "Run Go linter" },
  ],
  mcpServers: [
    { type: "stdio", command: "npx", args: ["-y", "@github/github-mcp-server"],
      env: { GITHUB_PERSONAL_ACCESS_TOKEN: process.env.GITHUB_TOKEN } },
  ],
});

// Alternative providers:
await runtime.agent({ prompt: "Quick check.", model: "ollama/qwen3:8b" });
await runtime.agent({ prompt: "Summarize.", model: "custom/my-model",
  baseURL: "https://my-llm-gateway.internal/v1" });
```

Tools can also be string references to job-level or global registrations:

```typescript
await runtime.agent({
  prompt: "review",
  model: "anthropic/claude-sonnet-4",
  tools: ["linter"],
  mcpServers: ["github"],
});
```

### PR Review Bot (webhook-triggered)

```typescript
const pipeline = async () => {
  const review = await runtime.agent({
    model: "anthropic/claude-sonnet-4",
    prompt: `Review PR #${process.env.PR_NUMBER}. Run linter, summarize issues.`,
    tools: [
      { type: "container", name: "linter",
        image: "golangci/golangci-lint:latest", description: "Lint Go files" },
    ],
    mcpServers: [
      { type: "stdio", command: "npx", args: ["-y", "@github/github-mcp-server"],
        env: { GITHUB_PERSONAL_ACCESS_TOKEN: process.env.GITHUB_TOKEN } },
    ],
    maxSteps: 30,
  });

  await fetch(
    `https://api.github.com/repos/${process.env.REPO}/issues/${process.env.PR_NUMBER}/comments`,
    {
      method: "POST",
      headers: { Authorization: `Bearer ${process.env.GITHUB_TOKEN}` },
      body: JSON.stringify({ body: review.text }),
    },
  );
};
export { pipeline };
```

### Slack Bot (webhook-triggered)

```typescript
const pipeline = async () => {
  const response = await runtime.agent({
    model: "google/gemini-2.0-flash",
    prompt: `CI assistant. Respond to: "${process.env.SLACK_MESSAGE}"`,
    maxSteps: 10,
    timeout: "2m",
  });

  await fetch(process.env.SLACK_RESPONSE_URL, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      channel: process.env.SLACK_CHANNEL_ID,
      text: response.text,
    }),
  });
};
export { pipeline };
```

## Implementation

### Architecture

```
runtime/agent.go        → runtime.Agent(config) (AgentResult, error)
runtime/agent_tools.go  → run_container, get_step_result, conclude, named tools
backwards/src/agent.ts  → YAML transpilation (agent → runtime.agent())
```

No new driver interfaces, container types, or lifecycle management.

### Data Structures

```go
type AgentConfig struct {
  Name         string            `json:"name"`
  Prompt       string            `json:"prompt"`       // Shorthand or custom string
  Image        string            `json:"image"`        // Default image for run_container
  Model        string            `json:"model"`        // Required: "provider/model-name"
  BaseURL      string            `json:"baseURL"`      // Custom API endpoint
  Env          map[string]string `json:"env"`
  Workspace    bool              `json:"workspace"`    // Shared /workspace volume
  MaxSteps     int               `json:"maxSteps"`     // Default: 20
  MaxTokens    int               `json:"maxTokens"`    // 0 = unlimited
  TruncateHead int               `json:"truncateHead"` // get_step_result head bytes (default: 4096)
  TruncateTail int               `json:"truncateTail"` // get_step_result tail bytes (default: 61440)
  Timeout      string            `json:"timeout"`      // Default: "10m"
  Tools        []any             `json:"tools"`        // ToolConfig or string reference
  MCPServers   []any             `json:"mcpServers"`   // MCPServerConfig or string reference
}

type ToolConfig struct {
  Type        string `json:"type"`        // "container"
  Name        string `json:"name"`
  Image       string `json:"image"`
  Description string `json:"description"`
}

type MCPServerConfig struct {
  Type    string            `json:"type"`    // "http" or "stdio"
  URL     string            `json:"url"`     // For HTTP
  Command string            `json:"command"` // For stdio
  Args    []string          `json:"args"`
  Auth    string            `json:"auth"`
  Env     map[string]string `json:"env"`
}

type AgentResult struct {
  Text       string     `json:"text"`
  Status     string     `json:"status"`     // "pass" or "fail"
  TokensUsed int        `json:"tokensUsed"`
  Steps      []ToolCall `json:"steps"`
  Duration   string     `json:"duration"`
}
```

### Agent Execution

```go
func (r *Runtime) Agent(ctx context.Context, config AgentConfig) (AgentResult, error) {
  // 0. Expand prompt shorthands ("debug" → full instruction)
  config.Prompt = expandPromptShorthand(config.Prompt)

  // 1. Resolve API key from secrets provider
  apiKey, _ := r.secrets.Get(ctx, modelProviderSecret(config.Model))

  // 2. Register built-in tools: run_container, get_step_result, conclude
  //    + any named container tools from config.Tools

  // 3. Optionally create shared workspace volume (workspace: true)

  // 4. Connect MCP servers (http or stdio transport)

  // 5. Create ADK agent with ParallelTools: true
  model := resolveModel(config.Model, config.BaseURL, apiKey)
  agent := llmagent.New(llmagent.Config{
    Name: config.Name, Model: model, Instruction: config.Prompt,
    Tools: tools, Toolsets: toolsets, ParallelTools: true,
  })

  // 6. Run with limits (MaxSteps, MaxTokens)
  resp, _ := agent.Run(ctx, llmagent.RunConfig{...})

  // 7. If agent called conclude() → use its status/summary
  //    Otherwise → inferConclude() heuristic (fail indicators → fail, else pass)
  return result, nil
}
```

### Model Resolution (via adk-utils-go)

```go
var defaultBaseURLs = map[string]string{
  "openrouter": "https://openrouter.ai/api/v1",
  "ollama":     "http://localhost:11434/v1",
}

func resolveModel(model, baseURL, apiKey string) genai.Model {
  provider, modelName := splitProvider(model)
  switch provider {
  case "anthropic":
    return genaianthropic.New(genaianthropic.Config{APIKey: apiKey, ModelName: modelName})
  case "google":
    return genai.NewGoogleModel(modelName, apiKey)
  default: // openai, openrouter, ollama, azure, custom
    url := coalesce(baseURL, defaultBaseURLs[provider], "https://api.openai.com/v1")
    return genaiopenai.New(genaiopenai.Config{APIKey: apiKey, BaseURL: url, ModelName: modelName})
  }
}
```

### Key Behaviors

- **Smart truncation**: `get_step_result` returns first `truncateHead` bytes +
  `[...truncated N bytes...]` + last `truncateTail` bytes. Defaults: 4KB head,
  60KB tail.
- **Auto-conclude**: If the agent doesn't call `conclude()` before hitting
  limits, `inferConclude()` scans the last response for failure indicators
  (`"failed"`, `"error"`, `"bug found"`, etc.). Non-empty without indicators =
  pass. Empty = fail.
- **Prompt shorthands**: `expandPromptShorthand()` maps `"debug"` / `"review"` /
  `"analyze"` to full instructions; unknown strings pass through unchanged.
- **Non-zero exit codes**: Returned as data — the agent decides what to do.
- **Parallel calls**: ADK's `ParallelTools: true` allows concurrent containers.

## Configuration

### Model Credentials

API keys resolved via the existing secrets provider. The provider prefix from
`model` maps to a secret name:

| Provider   | Secret               | Example model                          |
| ---------- | -------------------- | -------------------------------------- |
| Anthropic  | `anthropic_api_key`  | `anthropic/claude-sonnet-4`            |
| Google     | `google_api_key`     | `google/gemini-2.0-flash`              |
| OpenAI     | `openai_api_key`     | `openai/gpt-4o`                        |
| OpenRouter | `openrouter_api_key` | `openrouter/anthropic/claude-sonnet-4` |
| Ollama     | _(none)_             | `ollama/qwen3:8b`                      |
| Custom     | `{provider}_api_key` | `myhost/model-name` + `base_url: ...`  |

Format: `provider/model-name`. **No default** — `model` is required.

### Defaults

| Setting       | Default                                      | Override                           |
| ------------- | -------------------------------------------- | ---------------------------------- |
| Image         | Previous step's image                        | `image: myimage:tag`               |
| Model         | **Required**                                 | `model: provider/model-name`       |
| Base URL      | Auto-detected from provider                  | `base_url: https://my-endpoint/v1` |
| Prompt        | Raw string                                   | `prompt: debug` (shorthand)        |
| Workspace     | `false`                                      | `workspace: true`                  |
| Max steps     | 20                                           | `max_steps: 50`                    |
| Max tokens    | Unlimited                                    | `max_tokens: 200000`               |
| Timeout       | 10m                                          | `timeout: 30m`                     |
| Truncate head | 4096 (4 KB)                                  | `truncate_head: 8192`              |
| Truncate tail | 61440 (60 KB)                                | `truncate_tail: 131072`            |
| Tools         | Built-ins always available                   | `tools: [...]` or string refs      |
| MCP servers   | None                                         | `mcp_servers: [...]` or string refs|

### Storage Structure

```
/pipeline/{runID}/jobs/{job}/agent-{name}/
  ├── trace.json    (prompt, response, steps, tokens, duration)
  ├── result.txt    (conclude summary)
  ├── status        ("pass" or "fail")
  └── containers/
      └── call-{N}/ (stdout, stderr per tool call)
```

### Webhook Signature Auto-Detection

The server auto-detects webhook signature formats by inspecting request headers
(no per-pipeline configuration needed):

| Header                | Service | Validation                                                     |
| --------------------- | ------- | -------------------------------------------------------------- |
| `X-Hub-Signature-256` | GitHub  | Strip `sha256=` prefix, HMAC-SHA256 of body                   |
| `X-Slack-Signature`   | Slack   | HMAC-SHA256 of `v0:{timestamp}:{body}` with request timestamp |
| `X-Webhook-Signature` | Generic | HMAC-SHA256 of body (existing behavior)                        |
| _(none)_              | —       | Accept without validation (no secret configured)               |

First matching header wins. Extends `validateWebhookSignature()` in
`server/router.go`. No changes to the pipeline JS API.

## Testing Strategy

```go
func TestAgentRunContainer(t *testing.T) {
  assert := NewGomegaWithT(t)
  rt := setupTestRuntime(t, "docker")
  result, err := rt.Agent(context.Background(), AgentConfig{
    Prompt: "Echo 'hello world' with busybox. Conclude with pass.",
    Model:  "anthropic/claude-sonnet-4", Image: "busybox", MaxSteps: 5,
  })
  assert.Expect(err).NotTo(HaveOccurred())
  assert.Expect(result.Status).To(Equal("pass"))
}

func TestWebhookSignatureAutoDetect(t *testing.T) {
  // Verify GitHub (sha256= prefix), Slack (v0= prefix + timestamp),
  // and generic (raw hex) signatures all validate correctly.
}
```

Test matrix: Docker driver (full support), Native driver (returns
`ErrNotSupported`).

Unit tests for `expandPromptShorthand`, `inferConclude`, `smartTruncate` —
straightforward input/output assertions.

## Future Enhancements

- **Container `Exec()`**: Run commands in already-running containers
- **Write tool**: `write_file` / `append_file` backed by an output volume
- **Session persistence**: Redis-backed sessions via `adk-utils-go`
- **Long-term memory**: PostgreSQL + pgvector via `adk-utils-go`
- **Retry on failure**: `on_failure: { agent: "..." }` for fallback agents
- **Streaming UI**: Real-time tool call trace via WebSocket
- **Cost dashboard**: Token usage per job/pipeline
- **Agent caching**: Cache responses for identical prompts + context

## Appendix: Design Decisions

**What we avoided:**

- `charmbracelet/fantasy` — immature, no MCP, single-model
- Go bot packages — bots are TypeScript pipelines, not Go daemons
- New event loop (`runtime.serve()`) — existing webhooks handle inbound events
- Built-in file tools (`exec`, `read_file`) — `run_container` is simpler and
  container-scoped
- Sub-agent delegation — orchestrate with TypeScript control flow instead
- Container interface changes — `Exec()` deferred to future
- Hard failure on missing `conclude()` — auto-infer via `inferConclude()`
- Per-pipeline webhook signature config — auto-detect from headers
- Unlimited output passthrough — smart truncation keeps context manageable

**What we kept / built on:**

- `google/adk-go` + `achetronic/adk-utils-go` — model-agnostic, MCP-native,
  multi-provider
- Existing `runtime.run()` path — `run_container` reuses it unchanged
- Existing storage tree — agent traces follow the same path convention
- Existing webhook system — bots triggered by webhooks, respond via `fetch()`
- Existing secrets provider — API keys via `((provider_api_key))`
- Existing volume system — `workspace: true` uses `CreateVolume()`
- Goja/TypeScript — `runtime.agent()` is just another async call
- Non-zero exit codes as data, parallel tool calls, explicit `conclude()` with
  auto-infer fallback, prompt shorthands, tool reuse (pipeline + server level),
  webhook auto-detection, smart truncation
