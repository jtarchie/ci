# Development notes

This is a development repository for CI/CD system.

- 19-01-2025: Initial shape of running containers across docker, fly.io, and
  native support. It was to ensure that it could be done. There is still an
  optimization to be done to get the fly.io logs.
- 22-01-2025:
  - Refactor runtime to use a _sandbox_ context. Its purpose is to represent the
    execution environment. It was a refactoring step to see if we could have the
    runtime be populated for tests, different orchestrators, and then for builds
    (like in a true CI/CD system).
  - Added typescript support with a node module to support the type definition.
- 23-01-2025:
  - Added `assert` into the runtime, which will stop the runtime when something
    does not work. TODO: Print to the stdout/stderr of the pipeline.
- 24-01-2025:
  - Add task support from a pipeline YAML. An `assert` can also be used to help
    with testing. Will probably not be useful in production environments.
