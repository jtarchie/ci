# CI

Design principles:

- Designed to be local first.
- Runtime is accessible, not abstracted away.

<!-- deno-fmt-ignore-start -->
> [!IMPORTANT]
> This is a work in progress. It will change.
<!-- deno-fmt-ignore-end -->

The project represents a CI/CD that provides container orchestration
capabilities for automation workflows. It allows users to define pipelines in
JavaScript/TypeScript or YAML format (with backward compatibility for Concourse
CI-style configurations). The system currently supports two orchestration
drivers, Docker and Native, with Docker being the more feature-complete
implementation.

The pipeline execution model follows a task-based approach where containers can
be run with defined commands, environment variables, and shared volumes. The
core architecture includes an orchestration layer that abstracts container
operations, a runtime layer for JavaScript/TypeScript execution via Goja VM, and
backward compatibility for Concourse-style YAML pipelines. The project is in
active development, with recent additions focused on support for resource
operations (get/put) and environment variables, with thorough integration
testing across supported platforms.

## Usage

### Running a Pipeline

To execute a pipeline, use the runner command:

```bash
go run main.go runner <pipeline-file>
```

The runner supports both JavaScript/TypeScript and YAML pipeline formats:

```bash
# Run a JavaScript pipeline
go run main.go runner examples/hello-world.js

# Run a YAML pipeline (Concourse-style)
go run main.go runner examples/hello-world.yml
```

**Note:** The runner executes the pipeline in a single iteration and then exits.

### Viewing Pipeline Results

To view pipeline execution results in a web interface:

1. Start the server:

```bash
go run main.go server
```

2. Open your browser and navigate to:

```
http://localhost:8080/tasks
```

The web interface displays the execution results and task tree. **Note:** The
server does not provide live updates - you'll need to refresh the page to see
new results.

### Additional Options

- **Storage:** Both runner and server use SQLite by default
  (`sqlite://test.db`). You can specify a different storage location:

```bash
go run main.go runner --storage sqlite://my-pipeline.db examples/pipeline.js
go run main.go server --storage sqlite://my-pipeline.db --port 9000
```

- **Orchestrator:** Choose between `docker` (default) and `native`
  orchestration:

```bash
go run main.go runner --driver native examples/pipeline.js
```

- **Logging:** Control log level and format:

```bash
go run main.go runner --log-level debug --log-format json examples/pipeline.js
```

## Testing

This is relying on strict integration testing at the moment. I'd like to keep
the interfaces the same, but change underlying implementation.

The platforms of `docker` and `native` are tested against.

```bash
brew bundle
task
```

Please see `examples/` for real world usages. They are run as part of the text
suite to.
