# PocketCI Project — Coding Agent Instructions

> Trust these instructions. Only search the codebase if information here is
> incomplete or found to be in error.

## Project Summary

Local-first CI/CD runtime written in Go (module `github.com/jtarchie/pocketci`).
Executes JS/TS pipelines via Goja VM with Concourse YAML backward compatibility.
Pluggable container orchestration drivers (docker, native, k8s, fly,
digitalocean, hetzner, qemu, vz), SQLite storage, and an Echo HTTP server with
HTMx + idiomorph UI. ~50k lines of Go, ~2k lines of TypeScript.

## Prerequisites

Install all tools: `brew bundle` (reads `Brewfile`). Required: **Go 1.25+**,
**go-task**, **deno** (v2.x), **Node.js + npm**, **shellcheck**, **shfmt**.
**Docker must be running** for integration tests. No `.tool-versions` or
`.nvmrc` exists — versions come from `go.mod` and Brewfile.

## Build, Test, Lint — Command Reference

Always run commands from the repo root. The build task runner is
[go-task](https://taskfile.dev) (`Taskfile.yml`).

### Build (always run before test or lint)

```bash
task build          # Runs all three steps below in order:
# 1. task build:static  — cd server/static && npm install && npm run build
# 2. task build:docs    — cd docs && npm install && npm run build
# 3. go generate ./...  — regenerates backwards/bundle.js from backwards/src/*.ts
```

### Lint & Format

```bash
task fmt            # Runs in order: deno fmt, deno lint, deno check,
                    # shellcheck, shfmt, gofmt, golangci-lint run ./... --fix
```

No `.golangci.yml` config exists — golangci-lint uses default rules.

### Test

```bash
go test -race ./... -count=1 -parallel=1   # Always use these exact flags
```

Always use `-race`, `-count=1`, and `-parallel=1`. Omitting any of these causes
flaky results. To run a single package:
`go test -race ./storage/... -count=1 -parallel=1`.

### End-to-End Tests

```bash
cd e2e && npm install && npx playwright install --with-deps  # First time only
task test:e2e       # Runs Playwright against a local server (port 8080)
```

### Full CI (replicates GitHub Actions)

```bash
task                # Runs: build → fmt → cleanup → go test → test:e2e
```

This is the `default` task and matches what CI runs. **The GitHub Actions
workflow (`.github/workflows/go.yml`) runs `task build:static`,
`task build:docs`, `golangci-lint`, then `task` (default). Timeout is 10
minutes.**

### Cleanup

```bash
task cleanup        # Removes leaked Docker containers/volumes (bin/cleanup.sh)
```

Always run after test failures that may leave Docker resources behind.

## Critical Build Rules

1. **After editing `backwards/src/*.ts`**: always run `go generate ./...` to
   regenerate `backwards/bundle.js`. The file is `//go:embed`-ed — stale bundles
   cause silent test failures.
2. **After editing `server/static/src/`**: always run `task build:static` to
   regenerate `server/static/dist/bundle.js` (also embedded).
3. **After editing `docs/**/\*.md`or`docs/.vitepress/`**: always run
   `task build:docs`to regenerate`server/docs/site/` (embedded).
4. **After editing `storage/sqlite/schema.sql`**: no regeneration needed
   (directly embedded), but run tests to validate schema changes.
5. **After editing HTML templates in `server/templates/`**: no regeneration
   needed (directly embedded via `//go:embed templates/*`).

### Embedded Assets (`//go:embed`)

| Directive location         | Source                      | Generated artifact      |
| -------------------------- | --------------------------- | ----------------------- |
| `backwards/pipeline.go`    | `backwards/src/*.ts`        | `backwards/bundle.js`   |
| `server/templates.go`      | `server/templates/*`        | (direct, no build step) |
| `server/templates.go`      | `server/static/src/`        | `server/static/dist/*`  |
| `server/templates.go`      | `docs/`                     | `server/docs/site/`     |
| `storage/sqlite/driver.go` | `storage/sqlite/schema.sql` | (direct, no build step) |

## Project Layout

```
main.go                  CLI entry point (kong). Blank imports register all plugins.
main_darwin.go           macOS-only: registers orchestra/vz driver.
Taskfile.yml             Build/test/lint task definitions (go-task).
go.mod                   Go 1.25+, module github.com/jtarchie/pocketci
Brewfile                 macOS tool dependencies.
commands/                CLI subcommands: runner, server, run, transpile, set-pipeline, delete-pipeline, resource.
runtime/                 Goja VM execution engine.
  js.go                  TS→JS transpilation via esbuild, script validation.
  runtime.go             JS API: Run(), CreateVolume(), StartSandbox(), Agent().
  pipeline_runner.go     Container lifecycle management.
orchestra/               Container orchestration layer.
  orchestrator.go        Core interfaces: Driver, Container, Volume, Sandbox.
  drivers.go             Driver registry, DSN parsing (Add/Get/ParseDriverDSN).
  docker/                Docker driver (most complete).
  native/                Native (host process) driver.
  k8s/                   Kubernetes driver.
  fly/, digitalocean/, hetzner/, qemu/, vz/  Cloud/VM drivers.
  cache/                 Volume caching layer (s3/ backend).
storage/                 Persistence layer.
  storage.go             Driver interface: pipelines, runs, key-value, search.
  sqlite/                SQLite implementation. Schema in schema.sql.
backwards/               Concourse YAML → JS transpiler.
  pipeline.go            YAML parse + validate + transpile. go:generate for bundle.js.
  src/                   TypeScript source (index.ts, job_runner.ts, pipeline_runner.ts, task_runner.ts).
  config.go              Concourse-compatible YAML config structs.
  validation/            Pipeline validation rules.
server/                  Echo HTTP server + HTMx UI.
  router.go              Route definitions.
  templates.go           Go embed directives for templates, static, docs.
  templates/             HTML templates (HTMx + idiomorph for DOM morphing).
  static/                Frontend: TailwindCSS, esbuild, htmx, idiomorph, asciinema-player.
  docs/site/             VitePress-generated documentation site (embedded).
secrets/                 Secrets manager interface + local backend.
resources/               Concourse-compatible resource interface + mock impl.
webhooks/                Webhook provider framework (generic, github, slack).
examples/                Pipeline examples. both/ = docker+native, docker/ = docker-only.
  examples_test.go       Integration tests: runs each example against drivers.
e2e/                     Playwright browser tests (Chromium, port 8080).
testhelpers/             Test utilities (minio.go — starts local MinIO for S3 cache tests).
packages/pocketci/       TypeScript type definitions for the pipeline API.
```

## Plugin Registration Pattern

All plugins self-register via `init()` and are activated by blank imports in
`main.go`:

```go
func init() { orchestra.Add("docker", NewDocker) }   // in orchestra/docker/docker.go
_ "github.com/jtarchie/pocketci/orchestra/docker"           // in main.go
```

When adding a new driver/storage/secret/resource/webhook plugin: implement the
interface, add `init()` with registration call, add blank import to `main.go`.

## Testing Conventions

- **Black-box packages**: use `package foo_test`.
- **Assertions**: gomega — `assert := NewGomegaWithT(t)`,
  `assert.Expect(...).NotTo(HaveOccurred())`.
- **In-memory DB**: use `sqlite://:memory:` for tests (never file-backed unless
  testing persistence).
- **Driver imports in tests**: add
  `_ "github.com/jtarchie/pocketci/orchestra/docker"` (and/or `/native`).
- **JSON tags**: all structs exposed to Goja JS VM must have `json:"fieldName"`
  tags.
- **Logging**: use `slog` (structured). Never `log` or `fmt.Println` for
  operational output.
- **Errors**: wrap with `fmt.Errorf("context: %w", err)`.
- **UI patterns**: server uses HTMx + idiomorph
  (`morph:innerHTML`/`morph:outerHTML` swap). Use semantic HTML with ARIA
  attributes. Avoid custom JS for DOM state — idiomorph handles it.

## CI Validation (GitHub Actions)

The PR gate (`.github/workflows/go.yml`) runs on `ubuntu-latest` with Docker,
minikube, Node, Deno v2.x, Go stable. Steps: `task build:static` →
`task build:docs` → `golangci-lint` → `task` (default = build + fmt + cleanup +
tests + e2e). 10-minute timeout. To replicate locally before pushing:

```bash
task build && task fmt && go test -race ./... -count=1 -parallel=1
```
