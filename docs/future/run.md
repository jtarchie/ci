# Run Stored Pipelines by Name

## Overview

Add a `ci run <name> [args...]` command that executes a stored pipeline by name,
automatically mounts the current working directory into the container, and passes
all remaining CLI arguments through to the pipeline. This makes stored pipelines
feel like native CLI tools — `ci run k6 run --vus=10 script.js` looks and
behaves like running `k6` directly, but executes inside a container on any
configured driver (Docker, k8s, Fly, DO, Hetzner).

Local files reach remote containers via a new `local://` cache store that reuses
the existing `VolumeDataAccessor` / `CacheStore` infrastructure — the same
mechanism that S3 caching already uses.

**Design principle**: A stored pipeline should be invocable as if it were a
local CLI tool. One command, zero boilerplate.

## Core Primitive

A single new command: `ci run`. It does three things:

1. **Looks up** a pipeline by exact name from storage
2. **Mounts** the current working directory into the container automatically
3. **Passes** all remaining CLI arguments to the pipeline via
   `pipelineContext.args`

Built on existing infrastructure:

- `storage.Driver` — pipeline lookup (new `GetPipelineByName` method)
- `cache.CacheStore` — local file transfer (new `local://` store)
- `cache.VolumeDataAccessor` — volume injection (already implemented by all
  drivers)
- `pipelineContext` — argument passing (new `args` field)
- `runtime.ExecuteWithOptions()` — pipeline execution (unchanged)

## Non-Goals

- New container interface methods — uses existing `RunContainer()` and
  `CreateVolume()`
- Tool registry or dotfile configuration — pipelines _are_ the registry;
  `set-pipeline` is registration
- Replacing `ci runner` — `run` is by-name from storage, `runner` is by-file
  path (can deprecate `runner` later)
- Bind mount support in drivers — `local://` cache store handles file transfer
  portably across all drivers

## CLI Syntax

### Minimal (90% use case)

```bash
# Upload a pipeline once
ci set-pipeline k6.ts --name k6 --driver docker

# Run it — everything after "k6" is passed through
ci run k6 run --vus=10 script.js
```

The pipeline receives `pipelineContext.args = ["run", "--vus=10", "script.js"]`
and the current working directory is automatically available inside the
container.

### With Overrides

```bash
# Override the driver (run on k8s instead of the stored default)
ci run k6 --driver k8s://prod run --vus=10 script.js

# Custom storage location
ci run k6 --storage sqlite:///path/to/ci.db run script.js

# With secrets
ci run k6 --secrets local://secrets.db?key=pass \
  --secret K6_CLOUD_TOKEN=abc123 \
  run --out cloud script.js

# With timeout
ci run k6 --timeout 30m run --duration 20m script.js
```

### Cross-Driver Portability

Same pipeline, same args — different infrastructure:

```bash
# Local Docker
ci run k6 run script.js

# Kubernetes
ci run k6 --driver k8s://prod run script.js

# Fly.io
ci run k6 --driver fly run script.js

# DigitalOcean
ci run k6 --driver digitalocean run script.js

# Hetzner
ci run k6 --driver hetzner run script.js
```

This works because all drivers implement `VolumeDataAccessor`, which the
`local://` cache store uses to inject files.

## Pipeline Examples

### k6 Load Testing

```typescript
const pipeline = async () => {
  const workdir = await runtime.createVolume("workdir", 100);
  const result = await runtime.run({
    name: "k6-run",
    image: "grafana/k6:latest",
    command: { path: "k6", args: pipelineContext.args },
    mounts: { "/workspace": workdir },
  });

  if (result.code !== 0) {
    throw new Error(`k6 exited with code ${result.code}: ${result.stderr}`);
  }
};
export { pipeline };
```

```bash
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
      TF_IN_AUTOMATION: "1",
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

### Deno Scripts

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

### Multi-Step with Args Parsing

Pipelines can inspect `pipelineContext.args` for conditional logic:

```typescript
const pipeline = async () => {
  const args = pipelineContext.args;
  const workdir = await runtime.createVolume("workdir", 100);

  // Run linter first if --lint flag is present
  if (args.includes("--lint")) {
    await runtime.run({
      name: "lint",
      image: "golangci/golangci-lint:latest",
      command: { path: "golangci-lint", args: ["run", "./..."] },
      mounts: { "/workspace": workdir },
    });
  }

  // Run tests
  const testArgs = args.filter((a) => a !== "--lint");
  await runtime.run({
    name: "test",
    image: "golang:1.22",
    command: { path: "go", args: ["test", ...testArgs] },
    mounts: { "/workspace": workdir },
  });
};
export { pipeline };
```

```bash
ci run gotest --lint ./... -race -count=1
```

## Implementation

### Architecture

```
commands/run.go              → ci run command (Kong struct + execution logic)
runtime/js.go                → pipelineContext.args injection
storage/storage.go           → GetPipelineByName interface method
storage/sqlite/driver.go     → GetPipelineByName SQL implementation
orchestra/cache/local/       → local:// cache store (file transfer)
main.go                      → blank import + command registration
```

No new driver interfaces, container types, or lifecycle changes.

### Data Structures

```go
// commands/run.go
type Run struct {
    Name         string        `arg:"" help:"Pipeline name to execute"`
    Args         []string      `arg:"" optional:"" passthrough:""`
    Storage      string        `default:"sqlite://test.db" env:"CI_STORAGE" help:"Storage DSN" required:""`
    Driver       string        `env:"CI_DRIVER" help:"Override pipeline driver DSN"`
    Timeout      time.Duration `env:"CI_TIMEOUT" help:"Timeout for pipeline execution"`
    Secrets      string        `default:"" env:"CI_SECRETS" help:"Secrets backend DSN"`
    Secret       []string      `help:"Set a pipeline-scoped secret as KEY=VALUE" short:"e"`
    GlobalSecret []string      `help:"Set a global secret as KEY=VALUE"`
}
```

Kong's `passthrough` tag on `Args` captures everything after the pipeline name
verbatim — including flags like `--vus=10` that would otherwise be parsed by
Kong. No `--arg` prefix needed.

```go
// runtime/js.go — added to ExecuteOptions
type ExecuteOptions struct {
    // ... existing fields ...
    // Args are CLI arguments passed to the pipeline via pipelineContext.args.
    Args []string
}
```

```go
// storage/storage.go — added to Driver interface
type Driver interface {
    // ... existing methods ...
    // GetPipelineByName returns the most recently updated pipeline with the
    // given name. Returns ErrNotFound if no pipeline matches.
    GetPipelineByName(ctx context.Context, name string) (*Pipeline, error)
}
```

### Command Execution

```go
func (c *Run) Run(logger *slog.Logger) error {
    // 1. Open storage
    initStorage, found := storage.GetFromDSN(c.Storage)
    store, _ := initStorage(c.Storage, "", logger)

    // 2. Look up pipeline by name
    pipeline, err := store.GetPipelineByName(ctx, c.Name)
    // Returns stored content + DriverDSN

    // 3. Resolve driver: --driver flag wins, else pipeline's stored DriverDSN
    driverDSN := coalesce(c.Driver, pipeline.DriverDSN, "docker")

    // 4. Append local:// cache param for CWD mounting
    //    docker → docker:cache=local:///abs/path/to/cwd
    cwd, _ := os.Getwd()
    driverDSN = appendCacheParam(driverDSN, "local://"+cwd)

    // 5. Create driver + wrap with caching (existing flow)
    driverConfig, orchestrator, _ := orchestra.GetFromDSN(driverDSN)
    driver, _ := orchestrator(namespace, logger, driverConfig.Params)
    driver, _ = cache.WrapWithCaching(driver, driverConfig.Params, logger)

    // 6. Set up secrets (same as runner.go)

    // 7. Execute pipeline with args
    js := runtime.NewJS(logger)
    js.ExecuteWithOptions(ctx, pipeline.Content, driver, store, runtime.ExecuteOptions{
        PipelineID: pipeline.ID,
        Args:       c.Args,    // ← new field
        // ...
    })
}
```

### Pipeline Context Injection

In `runtime/js.go`, the `pipelineContext` map gains an `args` field:

```go
pipelineContext := map[string]any{
    "runID":       opts.RunID,
    "pipelineID":  opts.PipelineID,
    "triggeredBy": triggeredBy,
    "args":        opts.Args,       // ← new
}
```

When `opts.Args` is nil, defaults to an empty slice so pipelines can safely
iterate without nil checks.

### Local Cache Store

New file: `orchestra/cache/local/local.go`

Implements `cache.CacheStore` — the same interface as the S3 store. Registered
via `init()` + `cache.RegisterCacheStore("local", NewLocalStore)`.

```go
// URL format: local:///absolute/path/to/directory
type LocalStore struct {
    sourcePath string
}

func NewLocalStore(urlStr string) (cache.CacheStore, error) {
    parsed, _ := url.Parse(urlStr)
    return &LocalStore{sourcePath: parsed.Path}, nil
}

// Restore tars the source directory on the fly and returns the stream.
// The key parameter is ignored — the directory IS the content.
func (s *LocalStore) Restore(ctx context.Context, key string) (io.ReadCloser, error) {
    // Create tar of sourcePath → return io.ReadCloser
    // Reuse the same tar-walking logic as native/cache.go CopyFromVolume()
}

// Persist is a no-op — local dirs are read-only sources.
func (s *LocalStore) Persist(ctx context.Context, key string, r io.Reader) error {
    return nil
}

// Exists returns true if the source directory exists.
func (s *LocalStore) Exists(ctx context.Context, key string) (bool, error) {
    _, err := os.Stat(s.sourcePath)
    return err == nil, nil
}

// Delete is a no-op.
func (s *LocalStore) Delete(ctx context.Context, key string) error {
    return nil
}
```

The data flow reuses existing infrastructure end-to-end:

```
local directory
    → tar (LocalStore.Restore)
    → decompress (Compressor — identity/zstd/gzip)
    → CopyToVolume (VolumeDataAccessor — per driver)
    → container volume (Docker volume / k8s PVC / native dir / etc.)
```

Every driver that supports caching already implements `VolumeDataAccessor`:
Docker, k8s, native, DigitalOcean, Hetzner. No driver changes needed.

### Storage: GetPipelineByName

```sql
-- storage/sqlite/driver.go
SELECT id, name, content, driver_dsn, webhook_secret, created_at, updated_at
FROM pipelines
WHERE name = ?
ORDER BY updated_at DESC
LIMIT 1
```

Returns `storage.ErrNotFound` if no pipeline exists with that name. Uses the
most recently updated pipeline when multiple exist (shouldn't happen with proper
`set-pipeline` upsert, but defensive).

### Registration in main.go

```go
type CLI struct {
    Run            commands.Run            `cmd:"" help:"Run a stored pipeline by name"`
    Runner         commands.Runner         `cmd:"" help:"Run a pipeline from a file"`
    // ... existing commands ...
}
```

Blank import for the local cache store:

```go
_ "github.com/jtarchie/ci/orchestra/cache/local"
```

## Configuration

### Pipeline Storage

Pipelines are registered via the existing `set-pipeline` command:

```bash
ci set-pipeline k6.ts --name k6 --driver docker
ci set-pipeline terraform.ts --name terraform --driver k8s://prod
ci set-pipeline deno.ts --name deno --driver native
```

The stored pipeline includes `name`, `content`, and `driver_dsn`. The `ci run`
command uses all three.

### Driver Resolution

| Priority | Source                  | Example                   |
| -------- | ----------------------- | ------------------------- |
| 1        | `--driver` CLI flag     | `--driver k8s://prod`     |
| 2        | Pipeline's stored `DriverDSN` | Set via `set-pipeline --driver` |
| 3        | Default                 | `docker`                  |

### Secrets

Same mechanism as `ci runner`:

| Flag               | Scope    | Usage                              |
| ------------------ | -------- | ---------------------------------- |
| `--secret K=V`     | Pipeline | `env: { KEY: "secret:K" }` in pipeline |
| `--global-secret K=V` | Global | Fallback for any pipeline          |
| `--secrets DSN`    | Backend  | `local://secrets.db?key=passphrase` |

### Defaults

| Setting | Default              | Override              |
| ------- | -------------------- | --------------------- |
| Storage | `sqlite://test.db`   | `--storage DSN`       |
| Driver  | Pipeline's stored DSN | `--driver DSN`       |
| Timeout | None                 | `--timeout 30m`       |
| Secrets | Disabled             | `--secrets DSN`       |
| Workdir | Current directory    | _(always CWD)_        |
| Args    | Empty                | Positional passthrough |

## Testing Strategy

```go
func TestRunByName(t *testing.T) {
    assert := NewGomegaWithT(t)

    // Store a pipeline
    store := setupTestStorage(t)
    _, err := store.SavePipeline(ctx, "echo",
        `const pipeline = async () => {
            await runtime.run({
                name: "echo",
                image: "busybox",
                command: { path: "echo", args: pipelineContext.args },
            });
        }; export { pipeline };`,
        "docker", "")
    assert.Expect(err).NotTo(HaveOccurred())

    // Look up by name
    pipeline, err := store.GetPipelineByName(ctx, "echo")
    assert.Expect(err).NotTo(HaveOccurred())
    assert.Expect(pipeline.Name).To(Equal("echo"))
}

func TestGetPipelineByNameNotFound(t *testing.T) {
    assert := NewGomegaWithT(t)
    store := setupTestStorage(t)
    _, err := store.GetPipelineByName(ctx, "nonexistent")
    assert.Expect(err).To(MatchError(storage.ErrNotFound))
}

func TestLocalCacheStore(t *testing.T) {
    assert := NewGomegaWithT(t)
    // Create temp dir with files, verify Restore() produces valid tar
    // Verify Persist() is no-op, Delete() is no-op
    // Verify Exists() checks directory existence
}

func TestPipelineContextArgs(t *testing.T) {
    assert := NewGomegaWithT(t)
    // Execute pipeline with Args: ["hello", "world"]
    // Verify pipelineContext.args is accessible in JS
}
```

Test matrix: Docker + native drivers. Integration test:
`ci set-pipeline echo.ts --name echo && ci run echo hello world`.

Unit tests for `GetPipelineByName`, `LocalStore`, `pipelineContext.args` — no
external dependencies.

## Future Enhancements

- **`ci runner` deprecation**: Rename to `ci run-file` or merge into `ci run`
  with `--file` flag
- **Output volume**: Mount a second volume at `/output` for collecting results
  back to the host (reverse of `local://` — persist from container to local)
- **Pipeline aliases**: `ci alias k6-cloud "k6 --driver k8s://prod"` for
  shorthand invocations
- **Server-side run**: `ci run --server-url https://ci.example.com k6 run ...`
  to trigger remotely via API
- **Shared storage**: When `ci run` and `ci server` share the same storage DSN,
  runs appear in the web UI automatically
- **Exit code propagation**: Forward the container's exit code as the CLI
  process exit code
- **`.ci/pipelines/` convention**: Auto-register pipelines from a project-local
  directory
- **Streaming output**: Pipe `container.Logs()` to stdout/stderr in real time
  during `ci run`

## Appendix: Design Decisions

**What we avoided:**

- Separate `ci one-off` command — pipelines _are_ the tool registry; no
  parallel concept needed
- Tool registry config files (`~/.ci/tools.json`) — adds a configuration layer
  that duplicates what pipelines already do
- Bind mount support in drivers — portability nightmare across Docker/k8s/cloud;
  `local://` cache store solves it uniformly
- Bypassing the JS runtime — full pipeline execution preserves secrets, volumes,
  multi-step logic, and observability
- `--arg` flag syntax — Kong `passthrough` is cleaner: `ci run k6 run --vus=10`
  not `ci run k6 --arg run --arg --vus=10`
- Reserved pipeline names — `ci run` looks up by name from storage, there's no
  collision vector

**What we kept / built on:**

- Existing `set-pipeline` as registration — upload once, run many times
- Existing `storage.Driver` for pipeline lookup — new `GetPipelineByName`
  method, one SQL query
- Existing `cache.CacheStore` interface — `local://` store is ~60 lines,
  identical pattern to S3
- Existing `VolumeDataAccessor` — all 5+ drivers already implement it for S3
  caching
- Existing `cache.WrapWithCaching()` — auto-restores local files into volumes
  on creation
- Existing `pipelineContext` — `args` is one new field on an existing JS global
- Existing `ExecuteOptions` — `Args` is one new field on an existing Go struct
- Kong CLI framework — `passthrough` tag captures remaining args naturally