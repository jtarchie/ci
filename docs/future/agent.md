# Agent Steps in CI Pipelines

## Overview

Integrate
[Charm Bracelet's fantasy library](https://github.com/charmbracelet/fantasy) to
enable AI agent steps in YAML and TS/JS pipelines. Agents perform autonomous
multi-step tasks using available tools (container operations, file checks, API
calls), with output including tool execution traces, completion reasoning, and
final results stored in the pipeline's hierarchical storage tree.

## Goals

1. Allow pipelines to delegate complex debugging/analysis tasks to AI agents
2. Provide safe, sandboxed tool access for agents within pipeline context
3. Store structured agent execution traces for auditability
4. Maintain backward compatibility with existing Concourse YAML syntax

## Non-Goals

- Direct file system writes by agents (read-only initially)
- Unsupervised agent execution (explicit tool whitelists required)
- Real-time streaming UI (batch execution for MVP)

## YAML Syntax

### Basic Agent Step

```yaml
jobs:
  - name: debug-container
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
          The previous task failed. Investigate why by:
          1. Check if test output indicates missing dependencies
          2. Verify go.mod is present
          3. Suggest a fix
        model: anthropic/claude-sonnet-4
        system: "You are a CI debugging assistant."
        tools:
          - read_file
          - list_directory
          - read_output
          - run_container
        max_steps: 10
        timeout: 5m
```

### Agent Output Chaining

```yaml
jobs:
  - name: agent-testing
    plan:
      - agent: generate-test-cases
        prompt: "Analyze code and generate edge case test scenarios"
        model: openai/gpt-4
        tools: [read_file, list_directory]
        output: test-plan # Store result for next step

      - task: run-generated-tests
        config:
          platform: linux
          image_resource:
            type: registry-image
            source: { repository: golang, tag: "1.22" }
          inputs:
            - name: test-plan # Read agent output
          run:
            path: sh
            args:
              - -c
              - |
                  cat test-plan/response | jq -r '.test_commands[]' | \
                    while read cmd; do eval "$cmd"; done
```

### Failure Handling

```yaml
jobs:
  - name: agent-code-review
    plan:
      - agent: review-changes
        prompt: "Review git diff for bugs and security issues"
        model: openai/gpt-4
        tools: [run_container, read_file, http_request]
        on_failure:
          agent: fix-issues
          prompt: "Apply suggested fixes from the review"
          tools: [run_container, write_file]
```

## TypeScript/JavaScript API

```typescript
// examples/both/agent-debug.ts
const pipeline = async () => {
  let testResult = await runtime.run({
    name: "failing-test",
    image: "golang:1.22",
    command: { path: "go", args: ["test", "./..."] },
  });

  let agentResult = await runtime.agent({
    name: "investigate-failure",
    prompt: "Investigate test failure and suggest fix",
    model: "anthropic/claude-sonnet-4",
    system: "You are a CI debugging assistant.",
    tools: ["read_file", "list_directory", "read_output"],
    maxSteps: 10,
    timeout: "5m",
  });

  console.log(agentResult.response);
  console.log(
    `Used ${agentResult.tokensUsed} tokens in ${agentResult.steps.length} steps`,
  );
};
export { pipeline };
```

## Implementation Plan

### 1. Define Agent Step Interface

**File**: `backwards/src/index.ts`

Add `AgentStep` to pipeline step types:

```typescript
interface AgentStep {
  agent: string; // Step name
  prompt: string;
  model: string; // DSN-style: "provider/model-name"
  system?: string;
  tools: string[]; // Whitelist of allowed tools
  maxSteps?: number; // Default: 10
  timeout?: string; // Duration string (5m, 30s)
  output?: string; // Store result as pipeline input
  onFailure?: AgentStep; // Chained agent on failure
}
```

### 2. Implement Runtime Agent Executor

**File**: `runtime/agent.go`

```go
package runtime

import (
  "context"
  "time"
  "github.com/charmbracelet/fantasy"
)

type AgentConfig struct {
  Name      string   `json:"name"`
  Prompt    string   `json:"prompt"`
  Model     string   `json:"model"`
  System    string   `json:"system"`
  Tools     []string `json:"tools"`
  MaxSteps  int      `json:"maxSteps"`
  Timeout   string   `json:"timeout"`
}

type AgentResult struct {
  Prompt       string      `json:"prompt"`
  Steps        []ToolCall  `json:"steps"`
  FinishReason string      `json:"finishReason"`
  TokensUsed   int         `json:"tokensUsed"`
  Response     string      `json:"response"`
}

type ToolCall struct {
  Tool     string                 `json:"tool"`
  Input    map[string]interface{} `json:"input"`
  Output   string                 `json:"output"`
  Duration time.Duration          `json:"duration"`
}

func (r *Runtime) AgentRun(ctx context.Context, config AgentConfig) (AgentResult, error) {
  // Parse model DSN (e.g., "anthropic/claude-sonnet-4")
  provider, model := parseModelDSN(config.Model)
  
  // Create fantasy agent with provider credentials from env
  agent := fantasy.NewAgent(provider, model)
  agent.SystemPrompt = config.System
  
  // Register CI-specific tools
  for _, toolName := range config.Tools {
    tool := r.createAgentTool(toolName)
    agent.AddTool(tool)
  }
  
  // Execute with timeout
  timeout, _ := time.ParseDuration(config.Timeout)
  ctx, cancel := context.WithTimeout(ctx, timeout)
  defer cancel()
  
  result, err := agent.Generate(ctx, config.Prompt, fantasy.WithMaxSteps(config.MaxSteps))
  if err != nil {
    return AgentResult{}, fmt.Errorf("agent execution failed: %w", err)
  }
  
  // Convert fantasy result to AgentResult
  return convertFantasyResult(result), nil
}
```

### 3. Create CI-Specific Agent Tools

**File**: `runtime/agent_tools.go`

```go
package runtime

import (
  "github.com/charmbracelet/fantasy"
)

func (r *Runtime) createAgentTool(name string) fantasy.AgentTool {
  switch name {
  case "read_file":
    return fantasy.AgentTool{
      Name: "read_file",
      Description: "Read contents of a file from the workspace",
      Parameters: map[string]interface{}{
        "path": "File path relative to workspace root",
      },
      Function: func(args map[string]interface{}) (string, error) {
        path := args["path"].(string)
        // Delegate to existing volume operations
        return r.readFileFromVolume(path)
      },
    }
    
  case "list_directory":
    return fantasy.AgentTool{
      Name: "list_directory",
      Description: "List files in a directory",
      Parameters: map[string]interface{}{
        "path": "Directory path",
      },
      Function: func(args map[string]interface{}) (string, error) {
        path := args["path"].(string)
        return r.listDirectory(path)
      },
    }
    
  case "read_output":
    return fantasy.AgentTool{
      Name: "read_output",
      Description: "Read stdout/stderr from previous pipeline step",
      Parameters: map[string]interface{}{
        "step": "Step name",
      },
      Function: func(args map[string]interface{}) (string, error) {
        stepName := args["step"].(string)
        return r.getStepOutput(stepName)
      },
    }
    
  case "run_container":
    return fantasy.AgentTool{
      Name: "run_container",
      Description: "Execute command in a container",
      Parameters: map[string]interface{}{
        "image":   "Container image",
        "command": "Command to run",
        "args":    "Command arguments (array)",
      },
      Function: func(args map[string]interface{}) (string, error) {
        image := args["image"].(string)
        command := args["command"].(string)
        cmdArgs := args["args"].([]string)
        
        result, err := r.Run(r.ctx, &RunConfig{
          Name:    "agent-tool-execution",
          Image:   image,
          Command: &Command{Path: command, Args: cmdArgs},
        })
        if err != nil {
          return "", err
        }
        return result.Stdout, nil
      },
    }
    
  case "http_request":
    return fantasy.AgentTool{
      Name: "http_request",
      Description: "Make HTTP request to external service",
      Parameters: map[string]interface{}{
        "method": "HTTP method (GET, POST, etc.)",
        "url":    "Request URL",
        "body":   "Request body (optional)",
      },
      Function: func(args map[string]interface{}) (string, error) {
        // Implement HTTP client with timeout/rate limiting
        return r.httpRequest(args)
      },
    }
    
  default:
    panic(fmt.Sprintf("unknown agent tool: %s", name))
  }
}
```

### 4. Add Agent Step Processor

**File**: `backwards/src/pipeline_runner.ts`

```typescript
async function processAgentStep(step: AgentStep, context: PipelineContext) {
  try {
    const result = await runtime.agent({
      name: step.agent,
      prompt: step.prompt,
      model: step.model,
      system: step.system || "",
      tools: step.tools,
      maxSteps: step.maxSteps || 10,
      timeout: step.timeout || "5m",
    });

    // Store agent trace in pipeline storage
    const storagePath =
      `/pipeline/${context.runID}/jobs/${context.job}/agent-${step.agent}`;
    await storage.set(`${storagePath}/prompt`, step.prompt);
    await storage.set(`${storagePath}/response`, result.response);
    await storage.set(`${storagePath}/steps`, JSON.stringify(result.steps));
    await storage.set(
      `${storagePath}/tokensUsed`,
      result.tokensUsed.toString(),
    );

    // If output specified, create pipeline input
    if (step.output) {
      await storage.set(
        `/pipeline/${context.runID}/inputs/${step.output}/response`,
        result.response,
      );
    }

    // Trigger success/failure hooks
    if (result.finishReason === "error" && step.onFailure) {
      await processAgentStep(step.onFailure, context);
    }
  } catch (error) {
    console.error(`Agent step ${step.agent} failed:`, error);
    throw error;
  }
}
```

### 5. Storage Structure

Agent results stored in hierarchical tree:

```
/pipeline/{runID}/jobs/{job}/agent-{name}/
  ├── prompt          (string)
  ├── response        (string)
  ├── steps           (JSON array)
  ├── finishReason    (string)
  ├── tokensUsed      (int)
  └── duration        (duration)
```

Each tool call in `steps`:

```json
{
  "tool": "read_file",
  "input": { "path": "go.mod" },
  "output": "module github.com/example...",
  "duration": "120ms"
}
```

## Configuration

### Model Provider DSN

Format: `provider/model-name`

Examples:

- `anthropic/claude-sonnet-4`
- `openai/gpt-4`
- `openrouter/anthropic/claude-3.5-sonnet`

Credentials from environment variables:

- `ANTHROPIC_API_KEY`
- `OPENAI_API_KEY`
- `OPENROUTER_API_KEY`

### Tool Safety

All tools operate within pipeline security boundaries:

- `read_file`/`list_directory`: Only access mounted volumes
- `run_container`: Uses existing orchestrator with resource limits
- `http_request`: Rate-limited, timeout-enforced
- No direct host file system access

### Resource Limits

```yaml
agent: investigate
prompt: "..."
model: openai/gpt-4
tools: [read_file]
max_steps: 10 # Stop after 10 tool calls
timeout: 5m # Kill after 5 minutes
max_tokens: 4096 # Token limit for response
```

## Future Enhancements

### Phase 2: Streaming Execution

Support real-time UI updates using fantasy's streaming callbacks:

```go
agent.Stream(ctx, prompt, fantasy.StreamOptions{
  OnTextDelta: func(delta string) {
    r.logger.Info("agent output", "text", delta)
  },
  OnToolCall: func(tool string, args map[string]interface{}) {
    r.logger.Info("tool execution", "tool", tool, "args", args)
  },
})
```

### Phase 3: Write Operations

Add controlled write tools:

- `write_file`: Write to workspace with approval
- `create_pr`: Open GitHub PR with changes
- `update_config`: Modify pipeline YAML

### Phase 4: Multi-Agent Collaboration

Allow agents to delegate subtasks:

```yaml
agent: orchestrator
prompt: "Coordinate testing and deployment"
tools: [spawn_agent] # Creates child agents
sub_agents:
  - name: tester
    prompt: "Run comprehensive tests"
  - name: deployer
    prompt: "Deploy if tests pass"
```

## Testing Strategy

1. **Unit tests**: Mock fantasy agent responses in `runtime/agent_test.go`
2. **Integration tests**: Real agent calls against test models in
   `examples/examples_test.go`
3. **Tool tests**: Verify each tool in isolation with `gomega` assertions
4. **Pipeline tests**: End-to-end YAML → agent execution → storage verification

Example test:

```go
func TestAgentInvestigateFailure(t *testing.T) {
  assert := NewGomegaWithT(t)
  
  rt := setupTestRuntime(t)
  result, err := rt.AgentRun(context.Background(), AgentConfig{
    Name:     "test-agent",
    Prompt:   "Explain why this test failed",
    Model:    "mock/test-model",
    Tools:    []string{"read_output"},
    MaxSteps: 5,
  })
  
  assert.Expect(err).NotTo(HaveOccurred())
  assert.Expect(result.Steps).To(HaveLen(BeNumerically(">", 0)))
  assert.Expect(result.Response).To(ContainSubstring("test failed"))
}
```

## Open Questions

1. **Token cost tracking**: Should we expose per-job token usage in UI?
2. **Agent caching**: Can we cache agent responses for identical prompts
   (deterministic tools)?
3. **Tool versioning**: How to handle breaking changes in tool interfaces?
4. **Rate limiting**: Global rate limits across all pipeline agents?
