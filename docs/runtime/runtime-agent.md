# runtime.agent()

Run an LLM agent that can use a sandboxed container as a tool.

The agent receives a prompt, calls an LLM, and iteratively invokes a built-in
`run_command` tool backed by the container image you provide. Results stream
back in real time.

```typescript
const result = await runtime.agent(options);
```

## Options

### Required

| Field    | Type   | Description                                                          |
| -------- | ------ | -------------------------------------------------------------------- |
| `name`   | string | Unique task/agent name                                               |
| `prompt` | string | Initial user message or instruction                                  |
| `model`  | string | Model specifier: `provider/model-name` (see [Providers](#providers)) |
| `image`  | string | Container image for the sandbox (e.g., `"alpine"`, `"ubuntu:22.04"`) |

### Optional

| Field              | Type   | Description                                                     |
| ------------------ | ------ | --------------------------------------------------------------- |
| `mounts`           | object | Volume mounts: `{ "name": volumeHandle }`                       |
| `outputVolumePath` | string | Path inside the container to write `result.json`                |
| `llm`              | object | LLM generation overrides (see [LLM Config](#llm))               |
| `thinking`         | object | Extended thinking config (see [Thinking](#thinking))            |
| `safety`           | object | Safety filter overrides (see [Safety](#safety))                 |
| `context_guard`    | object | Context window management (see [Context Guard](#context-guard)) |

## Providers

The `model` field uses the format `provider/model-name`. Values after the first
`/` are passed verbatim to the provider.

| Provider     | `model` prefix   | Auth env var         | Notes                             |
| ------------ | ---------------- | -------------------- | --------------------------------- |
| `anthropic`  | `anthropic/...`  | `ANTHROPIC_API_KEY`  | Direct Anthropic API              |
| `openai`     | `openai/...`     | `OPENAI_API_KEY`     | Direct OpenAI API                 |
| `openrouter` | `openrouter/...` | `OPENROUTER_API_KEY` | Proxies 200+ models               |
| `ollama`     | `ollama/...`     | _(none)_             | Local Ollama at `localhost:11434` |

**API key resolution order** (first match wins):

1. Pipeline-scoped secret `agent/<provider>`
2. Global-scoped secret `agent/<provider>`
3. Environment variable `{PROVIDER}_API_KEY`

## LLM Config {#llm}

Fine-tune generation parameters. All fields are optional; omitting a field uses
the provider's built-in default.

```yaml
llm:
  temperature: 0.2 # float, 0.0–2.0 (default: provider default, typically 1.0)
  max_tokens: 8192 # int, max output tokens (default: provider default)
```

```typescript
llm: {
  temperature?: number;  // 0.0–2.0; lower = more deterministic
  max_tokens?: number;   // caps output length; provider default if omitted
}
```

> **Note for Anthropic with `thinking`:** `max_tokens` must be greater than the
> thinking budget. If you set a `thinking.budget` without `max_tokens`, the
> runtime defaults `max_tokens` to `8192`.

## Thinking {#thinking}

Enable extended reasoning / chain-of-thought for supported models. The extra
thinking tokens are billed but not included in `result.text`.

```yaml
thinking:
  budget: 10000 # int >= 1024, required when this block is present
  level: medium # string, Gemini only; omit for Anthropic
```

```typescript
thinking: {
  budget: number;  // thinking token budget (minimum 1024)
  level?: "low" | "medium" | "high" | "minimal"; // Gemini only
}
```

| Field    | Provider support | Notes                                              |
| -------- | ---------------- | -------------------------------------------------- |
| `budget` | All              | Anthropic: maps to `ThinkingBudgetTokens`          |
| `level`  | Gemini only      | Ignored for Anthropic; controls depth of reasoning |

## Safety {#safety}

Override per-category safety filters. Keys are harm category names
(case-insensitive); values are threshold names.

```yaml
safety:
  harassment: block_none
  dangerous_content: block_none
```

```typescript
safety?: {
  [category: string]: string;
};
```

### Category names

| YAML key            | Maps to                           |
| ------------------- | --------------------------------- |
| `harassment`        | `HARM_CATEGORY_HARASSMENT`        |
| `hate_speech`       | `HARM_CATEGORY_HATE_SPEECH`       |
| `sexually_explicit` | `HARM_CATEGORY_SEXUALLY_EXPLICIT` |
| `dangerous_content` | `HARM_CATEGORY_DANGEROUS_CONTENT` |
| `civic_integrity`   | `HARM_CATEGORY_CIVIC_INTEGRITY`   |

### Threshold values

| YAML value               | Effect                                 |
| ------------------------ | -------------------------------------- |
| `block_none`             | Allow all content                      |
| `block_only_high`        | Block only high-confidence harm        |
| `block_medium_and_above` | Block medium + high (provider default) |
| `block_low_and_above`    | Block low, medium, and high            |
| `off`                    | Disable the filter entirely            |

Safety settings are applied to Gemini and OpenAI-compatible models. Anthropic
manages safety at the API level and ignores this field.

## Context Guard {#context-guard}

Automatically manage the context window to prevent token-limit errors on long
agent runs.

```yaml
context_guard:
  strategy: threshold # "threshold" or "sliding_window"
  max_tokens: 100000 # for threshold strategy (default: 128000)
  max_turns: 30 # for sliding_window strategy (default: 30)
```

```typescript
context_guard?: {
  strategy: "threshold" | "sliding_window";
  max_tokens?: number;  // threshold: evict history when total exceeds this
  max_turns?: number;   // sliding_window: keep only the last N turns
};
```

| Strategy         | `max_tokens` default | `max_turns` default | Behaviour                                            |
| ---------------- | -------------------- | ------------------- | ---------------------------------------------------- |
| `threshold`      | 128000               | _N/A_               | Truncates history once total tokens exceed the limit |
| `sliding_window` | _N/A_                | 30                  | Keeps only the most-recent N conversation turns      |

Omitting `context_guard` entirely disables context management; the full
conversation history is sent to the model on every turn.

## Return Value

```typescript
{
  text: string; // final agent response text
  status: string; // "success"
  toolCalls: Array<{
    name: string;
    args?: Record<string, unknown>;
    result?: Record<string, unknown>;
    exitCode?: number;
  }>;
  usage: {
    promptTokens: number;
    completionTokens: number;
    totalTokens: number;
    llmRequests: number;
    toolCallCount: number;
  }
}
```

## Examples

### Minimal agent

```typescript
const result = await runtime.agent({
  name: "summarize",
  prompt: "Summarize the files in /workspace",
  model: "openrouter/google/gemini-2.0-flash",
  image: "alpine",
});

console.log(result.text);
```

### Agent with volumes and LLM tuning

```typescript
const repo = await runtime.createVolume("repo", 500);

await runtime.run({
  name: "clone",
  image: "alpine/git",
  command: {
    path: "git",
    args: ["clone", "https://github.com/example/app", "/repo"],
  },
  mounts: { "/repo": repo },
});

const result = await runtime.agent({
  name: "review",
  prompt: "Review the code for security issues and summarize findings.",
  model: "anthropic/claude-3-5-sonnet-20241022",
  image: "alpine",
  mounts: { "repo": repo },
  llm: { temperature: 0.1, max_tokens: 4096 },
  thinking: { budget: 2048 },
  safety: { dangerous_content: "block_only_high" },
  context_guard: { strategy: "threshold", max_tokens: 80000 },
});
```

### Streaming output callback

```typescript
await runtime.agent({
  name: "agent",
  prompt: "Run the test suite and report failures.",
  model: "openrouter/anthropic/claude-3-5-sonnet",
  image: "golang:1.22",
  mounts: { "src": srcVolume },
  onOutput: (stream, chunk) => {
    // stream is "stdout" or "stderr"
    process.stdout.write(chunk);
  },
});
```
