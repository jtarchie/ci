# Agent Steps in CI Pipelines

## Overview

Add AI agent steps to pipelines using
[Charm Bracelet's fantasy library](https://github.com/charmbracelet/fantasy).
Agents debug failures, analyze output, and perform multi-step tasks within
containers.

**Design principle**: Minimal configuration for common cases, optional overrides
for advanced use.

## Core Primitive

A single new step type: `agent`. It gets:

- **Prompt**: What to do
- **Container**: Where to execute (defaults to previous step's image)
- **Tools**: Basic tools (`exec`, `read_file`, `read_output`) + composable tools
  (containers, agents)
- **Context**: Automatic access to previous step's results and volumes
- **Composability**: Containers and agents are tools that can be added to agents

## Goals

- Zero-config agent debugging: `agent: "Debug the failure"` just works
- Safe execution within existing container boundaries
- Visible traces in storage tree

## Non-Goals

- Complex tool whitelisting (agents get standard tools only)
- Arbitrary host access (container-scoped only)
- Infinite recursion (sub-agent depth limited to 3 levels)

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

      - agent: "Debug the test failure. Check logs and suggest a fix."
```

**What happens automatically:**

- Agent runs in `golang:1.22` image (inherits from previous step)
- Gets basic tools: `exec(cmd)`, `read_file(path)`, `read_output(step)`
- Previous step's volumes auto-mounted as `/workspace`
- Uses Claude Sonnet 4 (via `ANTHROPIC_API_KEY`)
- Stops after 20 tool calls or 10 minutes
- Result stored at `/pipeline/{run}/agent-{name}/`

### With Overrides

```yaml
- agent: investigate
  prompt: "Analyze the memory leak."
  image: golang:1.22-debug # Use different image
  model: openai/gpt-4 # Override model (needs OPENAI_API_KEY)
  params: { DEBUG: "true" } # Add env vars
  max_steps: 50 # More tool calls
  timeout: 30m # Longer timeout
```

**Override options (all optional):**

- `image`: Container image (default: previous step's image)
- `model`: Provider/model format `openai/gpt-4`, `anthropic/claude-sonnet-4`
  (default: `anthropic/claude-sonnet-4`)
- `params`: Environment variables (default: inherits from previous step)
- `max_steps`: Tool call limit (default: 20)
- `timeout`: Execution timeout (default: 10m)
- `max_depth`: Sub-agent nesting limit (default: 3)

### Advanced: Adding Container and Agent Tools

```yaml
- agent: complex-analysis
  prompt: |
    Analyze this codebase:
    1. Run tests in multiple containers (Go, Python, Node)
    2. Delegate security analysis to a specialized sub-agent
    3. Compile results
  tools:
    - container:
        name: golang_runner
        image: golang:1.22
        description: Run Go commands
        persistent: true # Container stays alive for multiple commands
    - container:
        name: python_runner
        image: python:3.12
        description: Run Python commands
        persistent: true
    - agent:
        name: security_specialist
        prompt_prefix: "You are a security expert. "
        image: security-scanner:latest
        max_steps: 30
  max_steps: 100
  timeout: 30m
```

**Two types of container tools:**

````yaml
# One-shot container (default: persistent: false):
# golang_runner({ command: "go", args: ["test", "./..."] })
# → Spins up container, runs command, exits, returns { stdout, stderr, exitCode }

# Persistent container (persistent: true):
# golang_runner({ command: "go", args: ["test", "./..."] })
# → First call: starts container, runs command, keeps alive
# → Subsequent calls: reuses same container, runs command
# → Returns { stdout, stderr, exitCode } each time

# Example usage:
# Call 1: golang_runner({ command: "go", args: ["mod", "download"] })
# Call 2: golang_runner({ command: "go", args: ["test", "./.."] })
# Call 3: golang_runner({ command: "go", args: ["build"] })
# All run in same container, preserving state (downloaded modules, build cache, etc.)

# Agent tools:
# security_specialist({ prompt: "Check for SQL injection vulnerabilities" })
# → Delegates to sub-agent with security context, returns { text, tokensUsed }
## TypeScript/JavaScript API

### Minimal

```typescript
const pipeline = async () => {
  // Run test
  let result = await runtime.run({
    name: "test",
    image: "golang:1.22",
    command: { path: "go", args: ["test", "./..."] },
  });

  // Agent automatically gets context from previous step
  let analysis = await runtime.agent({
    prompt: "Debug the test failure. Check logs and suggest a fix.",
  });

  console.log(analysis.text);
  console.log(
    `Used ${analysis.tokensUsed} tokens in ${analysis.steps.length} steps`,
  );
};
export { pipeline };
````

### With Overrides

```typescript
let analysis = await runtime.agent({
  prompt: "Analyze the memory leak in detail.",
  image: "golang:1.22-debug", // Override image
  model: "openai/gpt-4", // Override model
  env: { DEBUG: "true" }, // Add env vars
  maxSteps: 50, // More tool calls
  timeout: "30m", // Longer timeout
  context: {
    // Explicit context instead of auto-inherit
    previousResult: result,
    extraFiles: ["/app/config.yaml"],
  },
});
```

**Built-in tools (always available):**

```typescript
// Basic tools - no registration needed:
{
  exec: (command: string[]) => { stdout, stderr, exitCode },
  read_file: (path: string) => string,
  read_output: (stepName: string) => { stdout, stderr },
  list_files: (path: string) => string[],
}
```

**Composable tools (add as needed):**

```typescript
let analysis = await runtime.agent({
  prompt: "Analyze the codebase",
  tools: [
    // One-shot container tool
    {
      type: "container",
      name: "quick_test",
      image: "golang:1.22",
      description: "Run single Go command",
      persistent: false, // Default: runs command and exits
    },
    // Persistent container tool
    {
      type: "container",
      name: "golang_env",
      image: "golang:1.22",
      description: "Interactive Go environment",
      persistent: true, // Stays alive for multiple commands
    },
    // Agent tool
    {
      type: "agent",
      name: "security_specialist",
      promptPrefix: "You are a security expert. ",
      image: "security-scanner:latest",
      maxSteps: 30,
    },
  ],
});

// In agent execution, tools are available as:
// - quick_test({ command: "go", args: ["test", "./..."] }) // New container each call
// - golang_env({ command: "go", args: ["mod", "download"] }) // 1st call: starts container
// - golang_env({ command: "go", args: ["test", "./..."] })    // 2nd call: reuses container
// - golang_env({ command: "go", args: ["build"] })             // 3rd call: reuses container
// - security_specialist({ prompt: "Check for SQL injection" })
```

**When to use persistent vs one-shot containers:**

| Use Persistent (`persistent: true`)           | Use One-Shot (`persistent: false`, default) |
| --------------------------------------------- | ------------------------------------------- |
| Multiple related commands in same environment | Single command per tool invocation          |
| Preserve state (installed deps, build cache)  | Stateless operations (scans, linters)       |
| Interactive debugging workflows               | Parallel tool execution                     |
| Example: `npm install` then `npm test`        | Example: `trivy scan`, `golangci-lint run`  |
| Example: `go mod download` then `go test`     | Example: One-off database queries           |
| Agent decides when to reuse same container    | Fresh container every time                  |

**Key benefit:** Persistent containers let agents build up state across multiple
tool calls, like a human would in a shell session. The agent can install
dependencies once, then run multiple tests/builds without reinstalling.

### Advanced Example: Multi-Container Analysis

```typescript
const pipeline = async () => {
  // Initial failing test
  await runtime.run({
    name: "test",
    image: "golang:1.22",
    command: { path: "go", args: ["test", "./..."] },
  });

  // Agent with container and agent tools
  let result = await runtime.agent({
    prompt: `
      This test failed. Perform comprehensive analysis:
      1. Run 'go test -v' using go_tester
      2. Run linter using linter_runner
      3. Delegate database migration check to db_specialist
      4. Summarize findings
    `,
    tools: [
      {
        type: "container",
        name: "go_tester",
        image: "golang:1.22",
        description: "Run Go tests and commands",
        persistent: true, // Reuse container across multiple test runs
      },
      {
        type: "container",
        name: "linter_runner",
        image: "golangci/golangci-lint:latest",
        description: "Run golangci-lint",
        persistent: false, // One-shot linter run
      },
      {
        type: "agent",
        name: "db_specialist",
        promptPrefix: "You are a database expert. ",
        image: "postgres:15",
        maxSteps: 20,
      },
    ],
    maxSteps: 100,
  });

  console.log(result.text);
  // Agent will internally call:
  // - go_tester({ command: "go", args: ["mod", "download"] }) // 1st: start container
  // - go_tester({ command: "go", args: ["test", "-v"] })      // 2nd: reuse container
  // - go_tester({ command: "go", args: ["test", "-race"] })   // 3rd: reuse container
  // - linter_runner({ command: "golangci-lint", args: ["run"] }) // Fresh container
  // - db_specialist({ prompt: "Check migration..." })
};
```

## Use Cases

### 1. Multi-Language Test Analysis

Agent runs tests across different language environments:

```yaml
- agent: test-all-services
  prompt: |
    Run tests for all services and report which ones fail:
    - Frontend: Use node_runner for 'npm test'
    - Backend API: Use go_runner for 'go test ./...'
    - Worker: Use python_runner for 'pytest'
    Summarize all failures with recommendations.
  tools:
    - container:
        name: node_runner
        image: node:20
        persistent: true
    - container:
        name: go_runner
        image: golang:1.22
        persistent: true
    - container:
        name: python_runner
        image: python:3.12
        persistent: true
  max_steps: 50
```

**Why persistent matters here:** Agent might run `npm install` then `npm test`,
reusing installed dependencies.

### 2. Security Audit with Delegation

Agent coordinates security checks across multiple tools:

```yaml
- agent: security-audit
  prompt: |
    Perform comprehensive security audit:
    1. Use trivy_scanner to scan images
    2. Delegate to dependency_auditor for npm audit
    3. Delegate to sast_analyzer for static analysis
    4. Compile findings into priority-ranked report
  tools:
    - container:
        name: trivy_scanner
        image: aquasec/trivy:latest
        description: Scan container images for vulnerabilities
        persistent: false # One-shot scanner
    - agent:
        name: dependency_auditor
        prompt_prefix: "You are a dependency security expert. "
        image: node:20
        max_steps: 30
    - agent:
        name: sast_analyzer
        prompt_prefix: "You are a static analysis expert. "
        image: scanner:latest
        max_steps: 40
  max_steps: 100
  timeout: 30m
```

**Note:** Security scanners are one-shot (persistent: false) since they
typically run once per scan target.

### 3. Gradual Debugging Escalation

```typescript
const pipeline = async () => {
  let testResult = await runtime.run({
    name: "integration-test",
    image: "app:latest",
    command: { path: "npm", args: ["test"] },
  });

  if (testResult.exitCode !== 0) {
    // First-level agent: Quick diagnosis
    let quickDiag = await runtime.agent({
      prompt:
        "Quickly check if this is a known issue. Review logs and workspace files.",
      maxSteps: 10,
    });

    if (quickDiag.text.includes("needs deeper analysis")) {
      // Second-level: Add specialized tools
      let deepDiag = await runtime.agent({
        prompt: `
          Perform deep analysis:
          1. Use db_checker to verify migrations
          2. Use api_tester for health checks
          3. Use dep_checker for dependencies
          4. If uncertain, delegate to security_expert
        `,
        tools: [
          { type: "container", name: "db_checker", image: "postgres:15" },
          { type: "container", name: "api_tester", image: "curl/curl:latest" },
          { type: "container", name: "dep_checker", image: "node:20" },
          {
            type: "agent",
            name: "security_expert",
            promptPrefix: "You are a security specialist. ",
            maxSteps: 30,
          },
        ],
        maxSteps: 50,
      });

      console.log("Deep analysis:", deepDiag.text);
    }
  }
};
```

## Implementation Overview

### Philosophy

**The agent is just another pipeline step with a container that runs commands.**
No new abstractions needed - reuse existing container infrastructure.

### Architecture

```
runtime/agent.go           → runtime.Agent(config) using existing Container
runtime/agent_tools.go     → Built-in tool implementations (exec, read_file, etc.)
backwards/src/agent.ts     → YAML transpilation (agent: "prompt" → runtime.agent())
```

**No new:** Driver interfaces, container types, or lifecycle management. Use
existing `runtime.run()` internals.

### Core Implementation

**1. Agent Config (Simple)**

```go
// runtime/agent.go
type AgentConfig struct {
  Name      string            `json:"name"`
  Prompt    string            `json:"prompt"`
  Image     string            `json:"image"`     // Optional: defaults to previous step
  Model     string            `json:"model"`     // Optional: default "anthropic/claude-sonnet-4"
  Env       map[string]string `json:"env"`       // Optional: inherits from previous
  MaxSteps  int               `json:"maxSteps"`  // Default: 20
  Timeout   string            `json:"timeout"`   // Default: "10m"
  Tools     []Tool            `json:"tools"`     // Optional: container and agent tools
}

type Tool struct {
  Type         string            `json:"type"`          // "container" or "agent"
  Name         string            `json:"name"`          // Tool call name
  Description  string            `json:"description"`   // For container tools
  Image        string            `json:"image"`         // Container image or agent image
  Persistent   bool              `json:"persistent"`    // For container tools: keep alive
  PromptPrefix string            `json:"promptPrefix"`  // For agent tools
  MaxSteps     int               `json:"maxSteps"`      // For agent tools
  Env          map[string]string `json:"env"`           // For container tools
}

type AgentResult struct {
  Text        string      `json:"text"`
  TokensUsed  int         `json:"tokensUsed"`
  Steps       []ToolCall  `json:"steps"`
  Duration    string      `json:"duration"`
}

func (r *Runtime) Agent(ctx context.Context, config AgentConfig) (AgentResult, error) {
  return r.agentWithDepth(ctx, config, 0)
}

func (r *Runtime) agentWithDepth(ctx context.Context, config AgentConfig, depth int) (AgentResult, error) {
  // 1. Start agent's main container
  container := r.startAgentContainer(ctx, config)
  defer container.Cleanup(ctx)
  
  // 2. Create tools: basic tools + container tools (persistent or one-shot) + agent tools
  tools := createAllTools(r, config, container, depth)
  
  // 3. Create fantasy agent with all tools
  agent := fantasy.NewAgent(config.Model, tools)
  
  // 4. Execute agent
  result := agent.Generate(ctx, config.Prompt, config.MaxSteps)
  
  // 5. Cleanup all persistent containers
  for _, tool := range tools {
    if tool.Cleanup != nil {
      tool.Cleanup()
    }
  }
  
  // 6. Store trace and return result
  return result, nil
}
```

**2. Tool System**

```go
// runtime/agent_tools.go

// Basic tools - always available
func createBasicTools(container Container) []fantasy.Tool {
  return []fantasy.Tool{
    {
      Name: "exec",
      Description: "Execute command in agent's container",
      Fn: func(command []string) (string, error) {
        result := container.Exec(ctx, command)
        return fmt.Sprintf("stdout: %s\nstderr: %s\nexit: %d", 
                result.Stdout, result.Stderr, result.ExitCode), nil
      },
    },
    {
      Name: "read_file",
      Description: "Read file from workspace",
      Fn: func(path string) (string, error) {
        result := container.Exec(ctx, []string{"cat", filepath.Join("/workspace", path)})
        return result.Stdout, nil
      },
    },
    {
      Name: "read_output",
      Description: "Read previous step output from pipeline storage",
      Fn: func(stepName string) (string, error) {
        return r.storage.Get(fmt.Sprintf("/pipeline/%s/stdout", stepName))
      },
    },
    {
      Name: "list_files",
      Description: "List files in directory",  
      Fn: func(path string) ([]string, error) {
        result := container.Exec(ctx, []string{"ls", "-1", filepath.Join("/workspace", path)})
        return strings.Split(result.Stdout, "\n"), nil
      },
    },
  }
}

// Create container tool from config
func createContainerTool(r *Runtime, tool Tool, baseVolumes map[string]Volume) fantasy.Tool {
  var persistentContainer Container // Nil if not persistent
  
  return fantasy.Tool{
    Name: tool.Name,
    Description: tool.Description,
    Parameters: map[string]string{
      "command": "Command to run",
      "args": "Command arguments",
    },
    Fn: func(command string, args []string) (RunResult, error) {
      if tool.Persistent {
        // Start container on first call, reuse thereafter
        if persistentContainer == nil {
          persistentContainer = r.StartContainer(ctx, StartContainerConfig{
            Name:   tool.Name,
            Image:  tool.Image,
            Env:    tool.Env,
            Mounts: baseVolumes,
          })
        }
        // Execute in persistent container
        result := persistentContainer.Exec(ctx, append([]string{command}, args...))
        return RunResult{
          Stdout:   result.Stdout,
          Stderr:   result.Stderr,
          ExitCode: result.ExitCode,
        }, nil
      } else {
        // One-shot: start container, run command, exit
        return r.Run(ctx, RunConfig{
          Name:    tool.Name,
          Image:   tool.Image,
          Command: &Command{Path: command, Args: args},
          Env:     tool.Env,
          Mounts:  baseVolumes,
        })
      }
    },
    // Cleanup function called when agent finishes
    Cleanup: func() error {
      if persistentContainer != nil {
        return persistentContainer.Cleanup(ctx)
      }
      return nil
    },
  }
}

// Create agent tool from config
func createAgentTool(r *Runtime, tool Tool, depth int) fantasy.Tool {
  return fantasy.Tool{
    Name: tool.Name,
    Description: fmt.Sprintf("Delegate to %s sub-agent", tool.Name),
    Parameters: map[string]string{
      "prompt": "Task description for the sub-agent",
    },
    Fn: func(prompt string) (AgentResult, error) {
      if depth >= 3 {
        return AgentResult{}, fmt.Errorf("max agent depth reached")
      }
      // Prepend context to prompt
      fullPrompt := tool.PromptPrefix + prompt
      return r.agentWithDepth(ctx, AgentConfig{
        Prompt:   fullPrompt,
        Image:    tool.Image,
        MaxSteps: tool.MaxSteps,
      }, depth+1)
    },
  }
}

// Combine all tools
func createAllTools(r *Runtime, config AgentConfig, container Container, depth int) []fantasy.Tool {
  tools := createBasicTools(container)
  
  // Add container and agent tools from config
  for _, tool := range config.Tools {
    switch tool.Type {
    case "container":
      tools = append(tools, createContainerTool(r, tool, container.Volumes()))
    case "agent":
      tools = append(tools, createAgentTool(r, tool, depth))
    }
  }
  
  return tools
}
```

**3. Container Reuse**

```go
// Reuse existing Container interface - no changes needed
// orchestra/orchestrator.go already has Exec():

type Container interface {
  Exec(ctx context.Context, command []string) (ExecResult, error)
  Cleanup(ctx context.Context) error
  // ... existing methods
}
```

**Docker driver already implements Exec()** via `ContainerExecCreate`. **Native
driver** returns `ErrNotSupported` (agents unavailable).

**4. YAML Transpilation**

```typescript
// backwards/src/agent.ts
function transpileAgentStep(step: string | AgentStep): string {
  const config = typeof step === "string"
    ? { prompt: step } // Shorthand: agent: "prompt text"
    : step; // Full: agent: { prompt: "...", image: "..." }

  return `
    await runtime.agent({
      prompt: ${JSON.stringify(config.prompt)},
      ${config.image ? `image: "${config.image}",` : ""}
      ${config.model ? `model: "${config.model}",` : ""}
      ${config.params ? `env: ${JSON.stringify(config.params)},` : ""}
      ${config.max_steps ? `maxSteps: ${config.max_steps},` : ""}
      ${config.timeout ? `timeout: "${config.timeout}",` : ""}
    });
  `;
}
```

## Configuration

### Model Credentials

Set environment variables based on provider:

- Anthropic: `ANTHROPIC_API_KEY`
- OpenAI: `OPENAI_API_KEY`
- OpenRouter: `OPENROUTER_API_KEY`

Format in config: `provider/model` (e.g., `anthropic/claude-sonnet-4`,
`openai/gpt-4`)

Default: `anthropic/claude-sonnet-4`

### Defaults

| Setting     | Default                                                                                  | Override               |
| ----------- | ---------------------------------------------------------------------------------------- | ---------------------- |
| Image       | Previous step's image                                                                    | `image: myimage:tag`   |
| Model       | `anthropic/claude-sonnet-4`                                                              | `model: openai/gpt-4`  |
| Max steps   | 20                                                                                       | `max_steps: 50`        |
| Timeout     | 10m                                                                                      | `timeout: 30m`         |
| Max depth   | 3 (sub-agent nesting)                                                                    | `max_depth: 5`         |
| Environment | Inherits from previous step                                                              | `params: { FOO: bar }` |
| Tools       | Basic: exec, read_file, read_output, list_files<br>Composable: add containers and agents | `tools: [...]`         |
| Volumes     | Auto-mount from previous                                                                 | (cannot override)      |

### Storage Structure

```
/pipeline/{runID}/jobs/{job}/agent-{name}/
  ├── trace.json       (prompt, response, steps, tokens, duration)
  ├── result.txt       (final text output)
  ├── containers/      (outputs from run() tool calls)
  │   ├── run-0/
  │   │   ├── stdout
  │   │   └── stderr
  │   └── run-1/
  └── sub-agents/      (nested agent traces)
      ├── delegate-0/
      │   ├── trace.json
      │   └── result.txt
      └── delegate-1/
```

## Testing Strategy

```go
// runtime/agent_test.go
func TestAgentMinimal(t *testing.T) {
  assert := NewGomegaWithT(t)
  rt := setupTestRuntime(t, "docker")
  
  // Previous step
  result, _ := rt.Run(context.Background(), RunConfig{
    Name: "test",
    Image: "alpine",
    Command: &Command{Path: "echo", Args: []string{"hello"}},
  })
  
  // Agent with zero config
  agentResult, err := rt.Agent(context.Background(), AgentConfig{
    Prompt: "What did the test output?",
  })
  
  assert.Expect(err).NotTo(HaveOccurred())
  assert.Expect(agentResult.Text).To(ContainSubstring("hello"))
}
```

Test matrix: Docker driver (full support), Native driver (returns
`ErrNotSupported`)

## Future Enhancements

Once core primitive is stable:

- **Write tools**: `write_file`, `append_file` (requires output volume)
- **HTTP tool**: `http_request` for external APIs (webhook notifications, GitHub
  issues)
- **Retry on failure**: `on_failure: { agent: "..." }` for fallback agents
- **Streaming UI**: Real-time tool call visibility via WebSocket
- **Cost dashboard**: Token usage per job/pipeline (including sub-agents)
- **Agent caching**: Cache responses for identical prompts + context
- **Parallel delegation**: `delegate_parallel()` for concurrent sub-agents
- **Resource limits**: CPU/memory limits per agent to prevent abuse

## Appendix: Why This Is Simple

**Before (complex):**

- Explicit tool registration
- Manual container lifecycle (`startContainer`, `exec`, `cleanup`)
- Duplicate task config for sandbox
- Multiple configuration surfaces (sandbox vs agent vs task)
- Two divergent APIs (YAML vs TypeScript)
- Complex orchestration logic exposed to users

**After (simple):**

- Basic tools always available (no registration)
- Composable tools: define containers and agents as tools
- One method: `runtime.agent(config)` handles everything
- Config is 7 optional fields with smart defaults
- YAML and TypeScript have identical capabilities
- Reuses existing container infrastructure
- Tools are explicitly named and described

**Value:**

- **90% case**: `agent: "Debug this"` → 1 line (uses basic tools only)
- **Complex orchestration**: Add named container/agent tools when needed
- **Overrides**: Available when needed, not required
- **Implementation**: ~600 LOC (runtime/agent.go + runtime/agent_tools.go)
- **Testing**: Existing container test infrastructure works unchanged
- **Safety**: Built-in depth limits prevent infinite recursion
- **Visibility**: Full trace of all container runs and sub-agent calls in
  storage tree
- **Composability**: Containers and agents are first-class tools

**Key insight**: Instead of hardcoding orchestration primitives, containers and
agents are tools you can add to agents. This makes the system more composable
and explicit. You name tools meaningfully (`golang_runner`,
`security_specialist`) which makes prompts clearer and traces more readable. The
agent picks the right tool for each task.
