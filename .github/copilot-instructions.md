# CI Project — Senior engineer guidance

This repository implements a local-first CI runtime (Go core, JS/TS pipelines) that is Concourse-compatible — it targets Concourse-style pipelines, resources, and job/step semantics. The notes below are for maintainers and automated agents operating as senior engineers: prioritize reliability, reproducibility, and minimal surface-area changes.

What matters (short)

- Stability: tests and race detection are first-class — run `go test -race ./... -count=1`.
- Reproducibility: static assets and transpiled TS are part of the binary — run `task build:static` and `go generate ./...` when editing frontend or `backwards/src/`.
- Readability & safety: follow formatting/linting (`task fmt`) and explicit error handling in Go.

Essential commands (repo root)

- Build static assets: `task build:static`
- Regenerate TS/static artifacts: `go generate ./...`
- Format & lint: `task fmt` (deno + gofmt + golangci-lint)
- Full CI locally: `task default` (builds, generate, format, type-checks, tests)

Common workflows

- Run a pipeline locally (quick):

  go run main.go runner examples/both/hello-world.ts

- Start the server for UI/debugging:

  go run main.go server --storage sqlite://test.db

- Transpile a pipeline to canonical form:

  go run main.go transpile <pipeline-file>

CLI defaults and useful flags

- `--storage sqlite://test.db`
- `--driver docker` (fallback: `native`)
- `--log-level info`

Style & architecture notes

- Go: target 1.24+, use `gofmt`, small focused interfaces, propagate errors clearly, prefer `slog` for structured logs.
- Runtime: Goja + esbuild for executing JS/TS pipeline code. Keep TS in `backwards/src/` and regenerate when changed.
- Tests: prefer black-box packages (`*_test`), use `gomega` for assertions, include driver parity tests (docker vs native).

Practical pitfalls (what I've seen)

- Forgot `go generate` after TS changes → binary runs different code than checked-in artifacts.
- Tests run without `-race` → subtle concurrency bugs slip in.
- Docker resource leakage during tests → run `task cleanup` periodically.

Where to inspect quickly

- Orchestration primitives: `orchestra/`
- Execution engine: `runtime/` (look at `js.go`, `pipeline_runner.go`)
- Storage & sqlite: `storage/`, `storage/sqlite`
- Examples and tests: `examples/`, `*_test.go` files

Pre-commit sanity checklist

- `task fmt` (format + lint)
- `go generate ./...` (if TS / static changed)
- `go test -race ./... -count=1`

Keep this page minimal. If you need to expand operational runbooks or onboarding, add a `docs/` page and link it here.
