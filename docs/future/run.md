# Run Stored Pipelines by Name

## Overview

Add a `ci run <name> [args...]` command that triggers a stored pipeline on the
CI server by name, streams its output back to the terminal in real time, and
exits with the container's exit code. This makes server-side pipelines feel like
native CLI tools — `ci run k6 run --vus=10 script.js` looks and behaves like
running `k6` directly, but executes entirely on the server.

**Nothing runs locally.** The client is a thin HTTP layer: it tars the working
directory, POSTs it to the server along with the args, streams the SSE response
to stdout/stderr, and exits. All execution, secrets, drivers, and configuration
live on the server where `ci server` is already configured and running.

**Design principle**: `ci server` is the execution engine. `ci run` is just a
phone call to it.

## Core Primitive

A single new command: `ci run`. It does four things:

1. **Resolves** the pipeline by exact name via the server API
2. **Uploads** the current working directory as a tar blob to the server
3. **Triggers** server-side pipeline execution with the args
4. **Streams** stdout/stderr back to the terminal; exits with the pipeline's
   exit code

Built on existing infrastructure:

- `set-pipeline` + `ci server` — pipelines live on the server (already true)
- `VolumeDataAccessor.CopyToVolume()` — volume injection (already implemented by
  all drivers)
- `ExecutionService.TriggerPipeline()` — pipeline execution (already used by
  trigger API)
- `pipelineContext` — argument passing (new `args` field)
- SSE or ndjson — log streaming (already used by the web UI)

## Non-Goals

- Local pipeline execution — `ci runner` is the file-based local runner;
  `ci
  run` never starts a JS VM locally
- Driver configuration on the client — drivers are the server's concern; the
  stored pipeline's `DriverDSN` is used
- Secret management on the client — secrets are stored and resolved server-side
- Replacing `ci runner` — `run` delegates to a server, `runner` executes a local
  file directly

## CLI Syntax

### Minimal (90% use case)

```bash
# Upload a pipeline once (existing workflow)
ci set-pipeline k6.ts --name k6 --driver docker --server-url http://localhost:8080

# Run it — everything after "k6" is passed through to the pipeline
ci run k6 run --vus=10 script.js --server-url http://localhost:8080
```

The server looks up the `k6` pipeline, receives the current directory as a tar
blob, seeds a volume with those files, and executes the pipeline with
`pipelineContext.args = ["run", "--vus=10", "script.js"]`. Logs stream back to
the client in real time.

### With Overrides

```bash
# Point at a remote server
ci run k6 --server-url https://ci.example.com run --vus=10 script.js

# Via env var (mirrors set-pipeline pattern)
CI_SERVER_URL=https://ci.example.com ci run k6 run script.js

# No workdir upload (pipeline needs no local files)
ci run k6 --no-workdir version

# Custom timeout
ci run k6 --timeout 30m run --duration 20m script.js
```

### Cross-Driver Portability

The driver is a server concern — the client doesn't specify it. The same
`ci
run` command works whether the server is configured for Docker, k8s, Fly.io,
or Hetzner:

```bash
# Server configured with --allowed-drivers docker
ci run k6 run script.js

# Server configured with --allowed-drivers k8s (pipeline stored with k8s driver)
ci run k6 run script.js   # identical invocation
```

The pipeline's stored `DriverDSN` (set via `set-pipeline --driver`) determines
where containers run.

## Pipeline Examples

### k6 Load Testing

```typescript
const pipeline = async () => {
  const workdir = await runtime.createVolume("workdir", 100);
  // workdir is pre-seeded with the client's uploaded directory
  const result = await runtime.run({
    name: "k6-run",
    image: "grafana/k6:latest",
    command: { path: "k6", args: pipelineContext.args },
    mounts: { "/workspace": workdir },
  });

  if (result.code !== 0) {
    throw new Error(`k6 exited with code ${result.code}:\n${result.stderr}`);
  }
};
export { pipeline };
```

```bash
# From the directory containing script.js
ci run k6 run --vus=10 --duration=30s /workspace/script.js
```

### Terraform

```typescript
const pipeline = async () => {
  const workdir = await runtime.createVolume("workdir", 500);
  await runtime.run({
    name: "terraform",
    image: "hashicorp/terraform:latest",
    command: { path: "terraform", args: pipelineContext.args },
    mounts: { "/workspace": workdir },
    env: {
      AWS_ACCESS_KEY_ID: "secret:aws_access_key",
      AWS_SECRET_ACCESS_KEY: "secret:aws_secret_key",
    },
  });
};
export { pipeline };
```

```bash
ci run terraform plan
ci run terraform apply -auto-approve
```

Secrets (`aws_access_key`, `aws_secret_key`) are resolved server-side. The
client knows nothing about them.

### Deno

```typescript
const pipeline = async () => {
  const workdir = await runtime.createVolume("workdir", 200);
  await runtime.run({
    name: "deno-run",
    image: "denoland/deno:latest",
    command: { path: "deno", args: pipelineContext.args },
    mounts: { "/workspace": workdir },
  });
};
export { pipeline };
```

```bash
ci run deno run /workspace/server.ts
ci run deno test /workspace/tests/
```

## Implementation

### Architecture

```
commands/run.go              → ci run client command (Kong struct + HTTP calls)
server/router.go             → POST /api/pipelines/:name/run endpoint
server/execution.go          → TriggerRunWithWorkdir() method
runtime/js.go                → pipelineContext.args injection
storage/storage.go           → GetPipelineByName interface method
storage/sqlite/driver.go     → GetPipelineByName SQL implementation
```

No new drivers, container types, or orchestration logic. The server already
knows how to execute pipelines, inject volumes, and manage secrets.

### Data Flow

```
Client (ci run)                          Server (ci server)
─────────────────────────────            ──────────────────────────────────────
1. tar CWD → multipart POST ──────────▶ POST /api/pipelines/k6/run
   {args: ["run", "--vus", "10"],         2. GetPipelineByName("k6") → content
    workdir: <tar stream>}                3. Store workdir tar temporarily
                                          4. TriggerRunWithWorkdir(pipeline, args, tar)
                                             → VolumeDataAccessor.CopyToVolume()
                                             → pipelineContext.args = args
                                             → ExecuteWithOptions()
4. Stream SSE events ◀─────────────────  5. Stream log events via SSE:
   {stream:"stdout", data:"..."}            {stream, data} per log chunk
   {stream:"stderr", data:"..."}            {event:"exit", code:0} on finish
   {event:"exit", code:0}
5. Exit with code 0
```

### Client Command (`commands/run.go`)

```go
type Run struct {
    Name      string        `arg:"" help:"Pipeline name to execute"`
    Args      []string      `arg:"" optional:"" passthrough:""`
    ServerURL string        `env:"CI_SERVER_URL" help:"URL of the CI server" required:"" short:"s"`
    Timeout   time.Duration `env:"CI_TIMEOUT"    help:"Timeout for pipeline execution"`
    NoWorkdir bool          `help:"Skip uploading the current working directory"`
}
```

Execution (`Run.Run`):

1. Resolve CWD to absolute path
2. Create `multipart/form-data` request:
   - Field `args`: JSON-encoded string array
   - File `workdir`: tar stream of CWD (skipped if `--no-workdir`)
3. POST to `{ServerURL}/api/pipelines/{name}/run`
4. Read SSE stream; write `stdout` events to `os.Stdout`, `stderr` events to
   `os.Stderr`
5. On `exit` event: `os.Exit(code)`

The client has no knowledge of drivers, storage, secrets, or Go runtime. It is
~80 lines of Go.

### Server Endpoint (`POST /api/pipelines/:name/run`)

New route added to `registerPipelineRoutes()` in `server/router.go`:

```
POST /api/pipelines/:name/run
Content-Type: multipart/form-data

Fields:
  args     — JSON array of strings, e.g. ["run", "--vus=10", "script.js"]
  workdir  — optional tar stream of the client's working directory
```

Handler:

1. `GetPipelineByName(ctx, name)` → pipeline or 404
2. Parse `args` field from multipart
3. Read `workdir` tar from multipart (if present), store to temp file
4. Call `execService.TriggerRunWithWorkdir(ctx, pipeline, args, tarReader)`
5. Set `Content-Type: text/event-stream`, flush headers
6. Write SSE events as the pipeline produces output:
   - `data: {"stream":"stdout","data":"..."}}\n\n`
   - `data: {"stream":"stderr","data":"..."}}\n\n`
   - `data: {"event":"exit","code":0}\n\n`

### `TriggerRunWithWorkdir()` in `ExecutionService`

New method on `ExecutionService` in `server/execution.go`. Extends
`TriggerPipeline()` with two additional fields passed to `ExecuteOptions`:

```go
func (s *ExecutionService) TriggerRunWithWorkdir(
    ctx context.Context,
    pipeline *storage.Pipeline,
    args []string,
    workdir io.Reader, // nil if --no-workdir
    logChan chan<- LogEvent,
) (*storage.PipelineRun, error)
```

On the execution goroutine:

1. If `workdir` is non-nil: create a named volume `"workdir"` via
   `driver.CreateVolume()`, then call
   `driver.(VolumeDataAccessor).CopyToVolume(ctx, "workdir", tarReader)` to seed
   it with the client's files
2. Set `ExecuteOptions.Args = args`
3. Set an `OnOutput` callback that writes SSE events to `logChan`
4. Call `js.ExecuteWithOptions()` as normal

The pipeline sees the volume already populated. A pipeline calling
`runtime.createVolume("workdir", size)` gets this pre-seeded volume through the
existing volume creation path (server checks if a volume with that name was
pre-created).

### `pipelineContext.args`

In `runtime/js.go`, add `Args []string` to `ExecuteOptions` and expose it in the
`pipelineContext` map:

```go
pipelineContext := map[string]any{
    "runID":       opts.RunID,
    "pipelineID":  opts.PipelineID,
    "triggeredBy": triggeredBy,
    "args":        opts.Args, // ← new; nil → []string{}
}
```

Pipelines access it as `pipelineContext.args` — a plain JS string array.

### `GetPipelineByName` in Storage

```go
// storage/storage.go — added to Driver interface
GetPipelineByName(ctx context.Context, name string) (*Pipeline, error)
```

SQLite implementation:

```sql
SELECT id, name, content, driver_dsn, webhook_secret, created_at, updated_at
FROM pipelines
WHERE name = ?
ORDER BY updated_at DESC
LIMIT 1
```

Returns `storage.ErrNotFound` if no pipeline has that name. The server endpoint
maps this to HTTP 404.

### Registration in `main.go`

```go
type CLI struct {
    Run            commands.Run            `cmd:"" help:"Run a stored pipeline by name on a server"`
    Runner         commands.Runner         `cmd:"" help:"Run a pipeline from a local file"`
    // ... existing commands
}
```

No new blank imports — `ci run` is a pure HTTP client with no driver
dependencies.

## Configuration

### Pipeline Registration

Pipelines are registered via the existing `set-pipeline` command. The server URL
and driver are the server's configuration:

```bash
# Register once — server handles driver, secrets, execution
ci set-pipeline k6.ts --name k6 --server-url https://ci.example.com --driver k8s://prod
ci set-pipeline terraform.ts --name terraform --server-url https://ci.example.com --driver k8s://prod
```

### Client Configuration

| Flag / Env Var                   | Default  | Purpose                        |
| -------------------------------- | -------- | ------------------------------ |
| `--server-url` / `CI_SERVER_URL` | required | Server address                 |
| `--timeout` / `CI_TIMEOUT`       | none     | Client-side stream timeout     |
| `--no-workdir`                   | false    | Skip uploading local directory |

That's it. No `--driver`, no `--storage`, no `--secrets` — those are server
concerns.

### Server-Side Additions

No new server flags needed. The server already has `--allowed-features`. A new
feature flag `runs` (or included under the existing execution path) controls
whether the `/api/pipelines/:name/run` endpoint is active.

### Defaults

| Setting    | Default           | Override                         |
| ---------- | ----------------- | -------------------------------- |
| Server URL | _(required)_      | `--server-url` / `CI_SERVER_URL` |
| Timeout    | None              | `--timeout 30m`                  |
| Workdir    | Current directory | `--no-workdir` to skip           |
| Args       | Empty             | Positional passthrough           |

## Testing Strategy

```go
func TestRunByName_NotFound(t *testing.T) {
    // POST /api/pipelines/nonexistent/run → 404
}

func TestRunByName_StreamsOutput(t *testing.T) {
    // Store a pipeline that echos pipelineContext.args
    // ci run echo hello world → streams "hello world" to stdout
    // SSE exit event carries code 0
}

func TestRunByName_WorkdirInjected(t *testing.T) {
    // Store a pipeline that cats /workspace/file.txt
    // Upload a tar containing file.txt
    // Verify content arrives in the container
}

func TestGetPipelineByName(t *testing.T) {
    // SavePipeline("k6", ...), GetPipelineByName("k6") → correct record
    // GetPipelineByName("nonexistent") → ErrNotFound
}

func TestPipelineContextArgs(t *testing.T) {
    // ExecuteWithOptions with Args: ["a", "b"]
    // pipelineContext.args accessible in JS as ["a", "b"]
}
```

Test matrix: Docker driver (full workdir injection), native driver. The client
(`commands/run.go`) is tested with an `httptest.Server` — no real container
execution needed for client unit tests.

## Future Enhancements

- **Run URL in output**: Print `Run: https://ci.example.com/runs/{id}` before
  streaming so the user can watch in the UI
- **Non-blocking mode**: `--detach` flag returns the run ID immediately without
  streaming
- **Output volume**: Reverse direction — collect files from `/output` volume
  back to the client after the run
- **Auth**: `--token` / `CI_TOKEN` for bearer auth when the server has
  `--basic-auth` configured
- **Workdir filtering**: Skip `.git/`, `node_modules/`, etc. when tarring CWD
  (configurable via `.ciignore`)
- **`ci runner` deprecation**: Once `ci run` covers the common cases,
  `ci
  runner` can be marked deprecated and eventually removed
- **Pipeline aliases**: `ci alias k6-cloud "k6 --driver k8s://prod"` stored
  server-side for shorthand invocations

## Appendix: Design Decisions

**What we avoided:**

- Local pipeline execution — secrets and drivers must not be configured on
  developer machines; the server is the single execution environment
- `--driver` / `--storage` / `--secrets` flags on `ci run` — these are server
  concerns; the client should not know or care about infrastructure
- A new `local://` cache store for local execution — obviated by the
  client→server upload model
- Bypassing the existing `ExecutionService` — reusing it means logs, run
  records, and the web UI work automatically
- Tool registry / dotfile config — pipelines stored in `ci server` _are_ the
  registry; registration is `set-pipeline`
- `--arg` flag syntax — Kong `passthrough` is cleaner: `ci run k6 run --vus=10`
  not `ci run k6 --arg run --arg --vus=10`

**What we kept / built on:**

- `set-pipeline` as the registration step — existing workflow, unchanged
- `ExecutionService.TriggerPipeline()` — extended with workdir and args, not
  replaced
- `VolumeDataAccessor.CopyToVolume()` — already universally implemented; used
  server-side to inject the uploaded tar into a volume
- `pipelineContext` — `args` is one new field on an existing JS global
- SSE streaming — consistent with how the web UI observes runs
- `storage.ErrNotFound` / HTTP 404 — consistent with existing pipeline API error
  handling
- Kong `passthrough` tag — captures all args after the pipeline name including
  flags (`--vus=10`), no `--arg` prefix needed
