# CI Project - GitHub Copilot Instructions

## Project Overview

This is a CI/CD system that provides container orchestration capabilities for automation workflows. The project is designed to be **local-first** with an accessible, non-abstracted runtime. It allows users to define pipelines in JavaScript/TypeScript or YAML format (with backward compatibility for Concourse CI-style configurations).

**Status**: Work in progress, subject to change

## High-Level Architecture

### Repository Structure

- **Size**: Medium-sized Go project with TypeScript/JavaScript integration
- **Languages**: Go (primary), TypeScript/JavaScript (pipeline definitions)
- **Runtime**: Goja VM for JavaScript/TypeScript execution
- **Orchestration**: Docker and Native drivers
- **Storage**: SQLite (default)
- **Web Framework**: Echo (v4) for server endpoints

### Core Components

- `/orchestra/` - Container orchestration abstraction layer

  - `docker/` - Docker driver implementation (feature-complete)
  - `native/` - Native process driver implementation
  - `orchestrator.go` - Main orchestration interface
  - `task.go` - Task execution logic
  - `drivers.go` - Driver registration and management

- `/runtime/` - JavaScript/TypeScript execution engine

  - `js.go` - Goja VM integration with esbuild transpilation
  - `pipeline_runner.go` - Pipeline execution coordinator
  - `yaml.go` - YAML pipeline parser (Concourse compatibility)
  - `assert.go` - Test assertion utilities

- `/storage/` - Data persistence layer

  - `sqlite/` - SQLite driver implementation
  - `storage.go` - Storage interface
  - `tree.go` - Task tree data structures

- `/commands/` - CLI command implementations

  - `runner.go` - Pipeline execution command
  - `server.go` - Web server command
  - `transpile.go` - Pipeline transpilation command

- `/backwards/` - Concourse CI backward compatibility

  - `src/` - TypeScript pipeline runner implementations
  - `config.go` - YAML configuration parsing
  - `pipeline.go` - Pipeline execution logic

- `/examples/` - Example pipelines (JS/TS/YAML)
- `/server/` - Web server templates and routing
- `/packages/ci/` - TypeScript type definitions

## Build and Development Workflow

### Prerequisites

- **Go**: Version 1.24.0 or higher
- **Deno**: For TypeScript formatting, linting, and type checking
- **Docker**: Required for Docker driver testing
- **Task**: Task runner (see Taskfile.yml)
- **golangci-lint**: For Go linting

### Build Commands

**Always run these commands from the project root.**

1. **Generate code** (bundles TypeScript, runs code generation):

   ```bash
   go generate ./...
   ```

   This MUST be run before building if TypeScript files in `backwards/src/` have changed.

2. **Format and lint all code**:

   ```bash
   task fmt
   ```

   This runs:

   - `deno fmt` on TypeScript/JavaScript files
   - `deno lint` on TypeScript files
   - `gofmt` on Go files
   - `golangci-lint` with auto-fix

3. **Run tests** (integration and unit):

   ```bash
   go test -race ./... -count=1
   ```

   Always use `-count=1` to disable test caching
   Always use `-race` to detect race conditions

4. **Full validation** (equivalent to CI):

   ```bash
   task default
   ```

   This runs: code generation → formatting → type checking → tests

5. **Run development server** (with live reload):

   ```bash
   task server
   ```

   Uses `wgo` to watch `.html`, `.ts`, `.go` files and auto-reload

6. **Clean Docker resources**:
   ```bash
   task cleanup
   ```
   Removes all Docker containers and volumes created during testing

### Running Pipelines

**Execute a pipeline** (single run):

```bash
go run main.go runner <pipeline-file>
```

Supported formats:

- JavaScript: `go run main.go runner examples/both/hello-world.js`
- TypeScript: `go run main.go runner examples/both/hello-world.ts`
- YAML: `go run main.go runner examples/both/hello-world.yml`

**View results** (web UI):

```bash
go run main.go server --storage sqlite://test.db
# Navigate to http://localhost:8080/tasks
```

**Transpile pipeline** (convert to canonical format):

```bash
go run main.go transpile <pipeline-file>
```

### Command Options

All commands support:

- `--storage <uri>` - SQLite database path (default: `sqlite://test.db`)
- `--driver <name>` - Orchestration driver: `docker` (default) or `native`
- `--log-level <level>` - Log level: `debug`, `info` (default), `warn`, `error`
- `--log-format <format>` - Log format: `text` (default) or `json`
- `--add-source` - Add source location to log messages

## Testing Strategy

**Philosophy**: Strict integration testing with consistent interfaces across drivers

### Test Execution

- Run all tests: `go test -race ./... -count=1`
- Run specific package: `go test -race ./runtime -count=1`
- Run with coverage: `go test -race -coverprofile=coverage.out ./...`

### Test Organization

- `*_test.go` files use `package <name>_test` (black-box testing)
- Tests validate behavior across both `docker` and `native` drivers
- Integration tests in `/examples/examples_test.go`
- Backward compatibility tests in `/backwards/backwards_test.go`

### Key Test Files

- `orchestra/drivers_test.go` - Driver interface validation
- `runtime/js_test.go` - JavaScript execution tests
- `runtime/yaml_test.go` - YAML parsing tests
- `backwards/backwards_test.go` - Concourse compatibility tests
- `examples/examples_test.go` - End-to-end pipeline tests

## Coding Standards

### Go Code

- Use Go 1.24 features
- Follow standard Go formatting (enforced by `gofmt`)
- All errors must be properly handled
- Use structured logging with `log/slog`
- Interfaces should be small and focused
- Driver pattern for extensibility (orchestra, storage)

### TypeScript/JavaScript

- Use TypeScript for type safety
- Follow Deno formatting standards
- Target ES2017 for Goja compatibility
- Use CommonJS module format (transpiled by esbuild)
- Type definitions in `/packages/ci/src/global.d.ts`

### Project Conventions

- Use `kong` for CLI parsing
- Use `gomega` for test assertions
- Driver registration via `init()` functions
- Context-based cancellation throughout
- Immutable task trees in storage layer

## Key Interfaces

### Orchestra Driver

```go
type Driver interface {
    Container(context.Context, string) (Container, error)
    Volume(context.Context, string) (Volume, error)
}
```

### Storage Driver

```go
type Driver interface {
    Set(context.Context, string, *Node) error
    Tree(context.Context) (map[string]NodeReference, error)
}
```

### Container Operations

- `Run()` - Execute command in container
- `Get()` - Download resource from URI
- `Put()` - Upload resource to URI

## Common Pitfalls

1. **TypeScript changes require regeneration**: After modifying `backwards/src/`, always run `go generate ./...`

2. **Test caching**: Always use `-count=1` flag to disable Go test cache

3. **Race detection**: Always run tests with `-race` flag

4. **Docker cleanup**: Run `task cleanup` if experiencing "address already in use" or volume mount errors

5. **Context cancellation**: Ensure all goroutines respect context cancellation

6. **Module imports**: Relative imports in TypeScript must use `.ts` extension

## Dependencies

### Go Modules (key dependencies)

- `github.com/dop251/goja` - JavaScript VM
- `github.com/evanw/esbuild` - TypeScript transpilation
- `github.com/docker/docker` - Docker client
- `github.com/labstack/echo/v4` - Web framework
- `modernc.org/sqlite` - Pure Go SQLite
- `github.com/onsi/gomega` - Test assertions
- `github.com/alecthomas/kong` - CLI parser
- `github.com/goccy/go-yaml` - YAML parsing

### Development Tools

- Deno (formatting, linting, type checking)
- Task (task runner)
- golangci-lint (Go linting)
- wgo (file watching for development)

## Environment Variables

No required environment variables. All configuration via CLI flags.

## Validation Steps

Before committing changes:

1. Run `task default` to ensure all checks pass
2. If tests fail on Docker, try `task cleanup` first
3. Verify both `docker` and `native` drivers work (if orchestration changes)
4. Check that examples still execute: `go run main.go runner examples/both/hello-world.ts`

## Additional Notes

- The web server does NOT provide live updates - refresh manually
- Pipeline execution is single-iteration (not continuous)
- Storage is append-only for task history
- Concourse compatibility is read-only (no `fly` integration)
- Native driver has limited feature parity with Docker driver
