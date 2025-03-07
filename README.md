# CI

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
