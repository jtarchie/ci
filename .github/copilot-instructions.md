# CI Project — AI Agent Instructions

Local-first CI runtime in Go that executes JS/TS pipelines via Goja VM, with
Concourse YAML backward compatibility.

## Architecture Overview

```
main.go → commands/ → runtime/ → orchestra/ → drivers (docker/native/k8s/...)
                         ↓
                    storage/sqlite
```

- **runtime/**: Goja VM executes pipelines; `js.go` transpiles TS→JS via
  esbuild, `pipeline_runner.go` runs containers
- **orchestra/**: Driver interface for container orchestration
  (`orchestrator.go` defines `Driver`, `Container`, `Volume` interfaces)
- **backwards/**: Concourse YAML → JS transpiler (TS source in `src/`, compiled
  to `bundle.js`)
- **storage/**: Persistence layer for task results (tree structure with
  path-based keys)

## Plugin Registration Pattern

Drivers/storage self-register via `init()` and blank imports in `main.go`:

```go
// orchestra/docker/docker.go
func init() { orchestra.Add("docker", NewDocker) }

// main.go — blank imports activate drivers
_ "github.com/jtarchie/ci/orchestra/docker"
```

## Essential Commands

```bash
task default          # Full CI: build, generate, fmt, lint, typecheck, test
task fmt              # deno fmt/lint + gofmt + golangci-lint
task build            # Build static assets + go generate
go generate ./...     # Regenerate TS bundle + static assets
go test -race ./... -count=1 -parallel=1  # Tests with race detector
task cleanup          # Remove leaked Docker containers/volumes
```

## Pipeline API (JS/TS)

Pipelines export an async `pipeline` function using `runtime.run()`:

```typescript
// examples/both/hello-world.ts
const pipeline = async () => {
  let result = await runtime.run({
    name: "task-name",
    image: "busybox",
    command: { path: "echo", args: ["hello"] },
    env: { FOO: "bar" },
  });
  assert.containsString(result.stdout, "hello");
};
export { pipeline };
```

## Testing Patterns

- Black-box packages: `package foo_test` with
  `_ "github.com/jtarchie/ci/orchestra/docker"` imports
- Use `gomega` assertions: `assert := NewGomegaWithT(t)`,
  `assert.Expect(...).NotTo(HaveOccurred())`
- Driver parity: tests run against both `docker` and `native` drivers (see
  `examples/examples_test.go`)
- In-memory DB for tests: `--storage sqlite://:memory:`

## Code Style

- Go 1.25+, `slog` for structured logging, explicit error wrapping with
  `fmt.Errorf("context: %w", err)`
- Interfaces in `orchestrator.go`/`storage.go`; implementations in
  subdirectories
- JSON field tags for Goja interop: `json:"fieldName"`

## Accessibility & UI Patterns

The UI (server/templates/, server/static/src/) uses idiomorph + HTMx for
intelligent DOM morphing, which preserves CSS animations, event listeners, and
element state during dynamic updates. A few patterns worth noting:

- **DOM Morphing** (`idiomorph`): Use `morph:innerHTML` or `morph:outerHTML`
  swap strategy instead of full replacements. This prevents animation resets and
  preserves SVG viewport transforms. Idiomorph uses ID-set algorithm to match
  nodes in-place.
- **Lazy Initialization**: Use event listeners (e.g., `toggle` on `<details>`)
  rather than `DOMContentLoaded` for content that loads dynamically. Inline
  scripts in dynamically-loaded HTML won't re-execute.
- **Semantic HTML**: Use `<ul role="list">` for lists, `role="radiogroup"` with
  `role="radio"` for radio buttons, `role="toolbar"` for control groups. Pair
  with matching ARIA attributes (`aria-checked`, `aria-label`,
  `aria-roledescription`).
- **Touch Targets**: Mobile controls should be at least 44×44px. Avoid relying
  on CSS `static` positioning for overlays on mobile—use `absolute` or `fixed`
  instead.
- **Experience vs. Custom JS**: Avoid custom JavaScript for DOM state management
  (e.g., comparing HTML structures, tracking expanded nodes). Modern tools like
  idiomorph handle edge cases better than hand-coded solutions.

## Common Pitfalls

- **Stale TS bundle**: Always run `go generate ./...` after editing
  `backwards/src/*.ts`
- **Race conditions**: Never skip `-race` flag in tests
- **Docker leaks**: Run `task cleanup` if tests fail mid-execution

## Key Files

| Purpose                | Location                                  |
| ---------------------- | ----------------------------------------- |
| CLI entry              | `main.go`, `commands/`                    |
| JS/TS execution        | `runtime/js.go`, `runtime/runtime.go`     |
| Container abstraction  | `orchestra/orchestrator.go`               |
| Driver implementations | `orchestra/{docker,native,k8s}/`          |
| Storage interface      | `storage/storage.go`, `storage/sqlite/`   |
| Concourse compat       | `backwards/src/`, `backwards/pipeline.go` |
| Example pipelines      | `examples/both/`                          |
