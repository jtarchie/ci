# Agent Steps in CI Pipelines

## Overview

Integrate
[Charm Bracelet's fantasy library](https://github.com/charmbracelet/fantasy) to
enable AI agent steps in YAML and TS/JS pipelines. Agents perform autonomous
multi-step tasks using available tools (container operations, file checks, API
calls), with traces stored in the pipeline's hierarchical storage tree.

## Goals

- Delegate complex debugging/analysis tasks to AI agents within pipelines
- Provide safe, sandboxed tool access with explicit whitelists
- Store structured agent execution traces for auditability
- Support iterative command execution via persistent sandbox containers
- Maintain backward compatibility with existing Concourse YAML syntax

## Non-Goals

- Unsupervised agent execution
- Real-time streaming UI (batch execution for MVP)
- Agent access to host file system outside pipeline volumes

## YAML Syntax

```yaml
jobs:
  - name: debug-and-fix
    plan:
      - task: failing-test
        config:
          platform: linux
          image_resource:
            type: registry-image
            source: { repository: golang, tag: "1.22" }
          run:
            path: sh
            args: ["-c", "go test ./... || exit 1"]

      - agent: investigate-failure
        prompt: |
          The previous task failed. Investigate by:
          1. Check test output for missing dependencies
          2. List files in the workspace
          3. Run diagnostic commands in the sandbox
          4. Suggest a fix
        model: anthropic/claude-sonnet-4 # Format: provider/model-name
        system: "You are a CI debugging assistant."
        tools:
          - read_file # Read from mounted volumes
          - list_directory # List workspace contents
          - read_output # Read previous step stdout/stderr
          - container_start # Start persistent container
          - container_exec # Execute in persistent container
        sandbox:
          # Sandbox uses same config as task
          platform: linux
          image_resource:
            type: registry-image
            source: { repository: golang, tag: "1.22" }
          inputs:
            - name: workspace # Mount workspace input
            - name: cache # Mount cache from previous step
          outputs:
            - name: analysis # Write analysis results
          caches:
            - path: /go/pkg/mod # Persist Go module cache
          params:
            GO111MODULE: "on"
            GOCACHE: "/go/pkg/mod"
          container_limits:
            cpu: 2000
            memory: 2147483648 # 2GB
          run:
            path: sh
            args: ["-c", "go version"] # Initialization command
        max_steps: 10 # Stop after 10 tool calls
        timeout: 5m # Agent execution timeout
        output: analysis # Store result for chaining
        on_failure: # Nested agent on failure
          agent: create-github-issue
          prompt: "Create issue with debug details"
          tools: [http_request]
```

**Key Features:**

- **Sandbox = Task config**: Sandbox uses same configuration as tasks (inputs,
  outputs, caches, params, run command)
- **Initialization command**: `run` executes once on container start for setup
- **Persistent container**: Container stays alive for iterative `container_exec`
  calls (Docker/K8s only)
- **Output chaining**: Store agent results as pipeline inputs via `output` field
- **Failure handling**: Nested agents via `on_failure`
- **Tool whitelist**: Explicit `tools` list enforces security boundaries

## TypeScript/JavaScript API

```typescript
// examples/both/agent-debug.ts
import { Agent } from "@charmbracelet/fantasy";

const pipeline = async () => {
  const workspace = await runtime.createVolume();
  const cache = await runtime.createVolume();

  // Run failing test
  let testResult = await runtime.run({
    name: "failing-test",
    image: "golang:1.22",
    command: { path: "go", args: ["test", "./..."] },
    mounts: { "/workspace": workspace },
  });

  // Start persistent sandbox container for agent
  const sandbox = await runtime.startContainer({
    name: "agent-sandbox",
    image: "golang:1.22",
    command: { path: "sh", args: ["-c", "go version"] }, // Initialization
    mounts: {
      "/workspace": workspace,
      "/go/pkg/mod": cache,
    },
    env: { GO111MODULE: "on" },
    keepAlive: true, // Don't exit after init command
  });

  // Create agent with container tools
  const agent = new Agent({
    model: "anthropic/claude-sonnet-4",
    system: "You are a CI debugging assistant.",
  });

  // Register CI tools
  agent.addTool({
    name: "read_file",
    description: "Read file from workspace",
    parameters: { path: "string" },
    fn: async ({ path }) => {
      const result = await sandbox.exec(["cat", `/workspace/${path}`]);
      return result.stdout;
    },
  });

  agent.addTool({
    name: "container_exec",
    description: "Execute command in sandbox",
    parameters: { command: "string", args: "string[]" },
    fn: async ({ command, args }) => {
      const result = await sandbox.exec([command, ...args]);
      return `stdout: ${result.stdout}\nstderr: ${result.stderr}\nexit: ${result.exitCode}`;
    },
  });

  agent.addTool({
    name: "read_previous_output",
    description: "Read output from previous step",
    parameters: { step: "string" },
    fn: async ({ step }) => {
      return testResult.stdout + "\n" + testResult.stderr;
    },
  });

  // Run agent
  const result = await agent.generate(
    `The previous test failed. Investigate by checking files and running diagnostics. Suggest a fix.`,
    { maxSteps: 10 },
  );

  console.log(result.text);
  console.log(`Tokens: ${result.tokensUsed}, Steps: ${result.steps.length}`);

  // Cleanup
  await sandbox.cleanup();
};
export { pipeline };
```

**Key Changes:**

- **JavaScript-driven agent**: Agent logic lives in JavaScript using fantasy
  library directly
- **`runtime.startContainer`**: Low-level primitive returns container handle
  with `exec()` method
- **Tool registration**: JavaScript registers tools that interact with container
  via `exec()`
- **Explicit lifecycle**: JavaScript controls container start, agent execution,
  and cleanup

## Implementation Overview

### Architecture

```
backwards/src/index.ts     → AgentStep interface (YAML → JS transpilation)
runtime/container.go       → StartContainer(config) -> Container
orchestra/orchestrator.go  → Container interface with Exec() method
runtime/js.go              → Expose Container.Exec() to Goja VM
storage/                   → Persist agent traces via JS storage API
```

**Philosophy**: Agent execution is JavaScript-driven. Go runtime provides
low-level container primitives (`startContainer`, `container.exec`), and
JavaScript uses fantasy library to orchestrate agent behavior.

### Core Components

**1. Container Start Configuration**

```go
// runtime/container.go
type StartContainerConfig struct {
  Name            string                 `json:"name"`
  Image           string                 `json:"image"`
  Command         *Command               `json:"command"`        // Init command
  Mounts          map[string]VolumeResult `json:"mounts"`
  Env             map[string]string      `json:"env"`
  ContainerLimits ContainerLimits        `json:"containerLimits"`
  KeepAlive       bool                   `json:"keepAlive"`      // Don't exit after command
}

// Returns Container with Exec() method
func (r *Runtime) StartContainer(ctx context.Context, config StartContainerConfig) (Container, error)
```

**Note**: Sandbox in YAML uses full task config
(inputs/outputs/caches/params/run) which transpiles to `StartContainerConfig` +
volume setup.

**2. Container Interface with Exec**

Containers expose `Exec()` for running commands without restarting:

```go
// Returned by runtime.startContainer (exposed to Goja VM)
type Container interface {
  Exec(ctx context.Context, command []string) (ExecResult, error)
  Cleanup(ctx context.Context) error
  ID() string
}

type ExecResult struct {
  Stdout   string `json:"stdout"`
  Stderr   string `json:"stderr"`
  ExitCode int    `json:"exitCode"`
}
```

**JavaScript usage**:

```typescript
const container = await runtime.startContainer({
  image: "alpine",
  keepAlive: true,
});
const result = await container.exec(["ls", "-la"]);
console.log(result.stdout);
await container.cleanup();
```

**3. Orchestra Driver Implementation**

```go
// orchestra/orchestrator.go - Container interface extension
type Container interface {
  Cleanup(ctx context.Context) error
  Logs(ctx context.Context, stdout, stderr io.Writer) error
  Status(ctx context.Context) (ContainerStatus, error)
  ID() string
  
  // New: Execute commands in running container
  Exec(ctx context.Context, command []string) (ExecResult, error)
}

type ExecResult struct {
  Stdout   string
  Stderr   string
  ExitCode int
}
```

**Docker implementation** (`orchestra/docker/docker.go`):

```go
func (c *Container) Exec(ctx context.Context, command []string) (ExecResult, error) {
  execConfig := types.ExecConfig{Cmd: command, AttachStdout: true, AttachStderr: true}
  execID, err := c.client.ContainerExecCreate(ctx, c.id, execConfig)
  // ... attach, run, collect output
}
```

Native driver returns `orchestra.ErrNotSupported`.

**4. YAML Transpilation**

Backwards compatibility layer transpiles agent YAML to JavaScript:

```typescript
// backwards/src/agent_runner.ts
async function processAgentStep(step: AgentStep) {
  // Setup sandbox container from task-like config
  const sandbox = await setupSandboxFromConfig(step.sandbox);

  // Create agent with whitelisted tools
  const agent = createAgentWithTools(
    step.model,
    step.system,
    step.tools,
    sandbox,
  );

  // Execute agent
  const result = await agent.generate(step.prompt, { maxSteps: step.maxSteps });

  // Store results and cleanup
  await storeAgentTrace(result);
  await sandbox.cleanup();
}

function setupSandboxFromConfig(config: TaskConfig): Promise<Container> {
  // Convert task config (inputs/outputs/caches/params) to container config
  // Run initialization command from config.run
  // Return container handle with exec() exposed
}
```

**5. Runtime API Additions**

```go
// runtime/runtime.go - New methods exposed to Goja VM

func (r *Runtime) StartContainer(ctx context.Context, config StartContainerConfig) (Container, error) {
  // Create container with keepAlive=true (overrides entrypoint with sleep infinity)
  // Run initialization command
  // Return container handle
}
```

```typescript
// Available in Goja VM
interface Runtime {
  startContainer(config: StartContainerConfig): Promise<Container>;
  createVolume(): Promise<Volume>;
  run(config: RunConfig): Promise<RunResult>;
  // ... existing methods
}

interface Container {
  exec(command: string[]): Promise<ExecResult>;
  cleanup(): Promise<void>;
  id: string;
}
```

**6. Storage Structure**

```
/pipeline/{runID}/jobs/{job}/agent-{name}/
  ├── prompt         (string)
  ├── response       (string)
  ├── steps          (JSON array of tool calls)
  ├── finishReason   (string: "stop", "max_steps", "error")
  ├── tokensUsed     (int)
  └── duration       (duration)
```

Tool call format:

```json
{
  "tool": "sandbox_exec",
  "input": { "command": "go", "args": ["test", "./..."] },
  "output": "PASS\nok github.com/example 0.123s",
  "exitCode": 0,
  "duration": "1.2s"
}
```

## Configuration

### Model Provider DSN

Format: `provider/model-name` (e.g., `anthropic/claude-sonnet-4`,
`openai/gpt-4`, `openrouter/anthropic/claude-3.5-sonnet`)

Credentials from environment: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`OPENROUTER_API_KEY`

### Tool Safety

- **Container isolation**: All agent tools execute within container boundaries
- **Volume restrictions**: Only access explicitly mounted inputs/outputs/caches
- **Resource limits**: Sandbox inherits container limits (CPU, memory, timeout)
- **No host access**: Cannot access host file system outside pipeline volumes
- **JavaScript sandboxing**: Goja VM prevents arbitrary system calls

### Driver Compatibility

| Feature                  | Docker | K8s | Native |
| ------------------------ | ------ | --- | ------ |
| `runtime.run`            | ✅     | ✅  | ✅     |
| `runtime.startContainer` | ✅     | ✅  | ❌     |
| `container.exec()`       | ✅     | ✅  | ❌     |

Native driver returns `ErrNotSupported` for `startContainer` - agents must use
one-shot `runtime.run` instead.

## Future Enhancements

- **Streaming execution**: Real-time UI updates via fantasy streaming callbacks
- **Write operations**: `write_file`, `create_pr`, `update_config` tools with
  approval workflows
- **Multi-agent collaboration**: `spawn_agent` tool for delegating subtasks
- **Cost tracking**: Per-job token usage metrics in UI
- **Agent caching**: Cache responses for deterministic tool sequences
- **Command allowlists**: Restrict `sandbox_exec` to approved commands (e.g.,
  `["sh", "go", "cat"]`)

## Testing Strategy

**Container tests** (`runtime/container_test.go`): Test `startContainer` and
`exec()` across drivers **Integration tests** (`examples/examples_test.go`):
Real agent calls using fantasy library in JavaScript **Driver tests**
(`orchestra/drivers_test.go`): Verify `Exec()` implementation with gomega
**Pipeline tests**: End-to-end YAML → JavaScript transpilation → agent execution

```go
func TestContainerExec(t *testing.T) {
  assert := NewGomegaWithT(t)
  rt := setupTestRuntime(t, "docker") // Skip for native driver
  
  container, err := rt.StartContainer(context.Background(), StartContainerConfig{
    Image:     "alpine",
    KeepAlive: true,
  })
  assert.Expect(err).NotTo(HaveOccurred())
  defer container.Cleanup(context.Background())
  
  // Execute multiple commands
  result1, err := container.Exec(context.Background(), []string{"echo", "hello"})
  assert.Expect(err).NotTo(HaveOccurred())
  assert.Expect(result1.Stdout).To(Equal("hello\n"))
  
  result2, err := container.Exec(context.Background(), []string{"ls", "-la"})
  assert.Expect(err).NotTo(HaveOccurred())
  assert.Expect(result2.ExitCode).To(Equal(0))
}
```
