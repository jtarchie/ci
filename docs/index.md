# Development notes

This is a development repository for CI/CD system.

- 2025-03-05: Support for `put` in resource pipeline.
- 2025-03-05: Support for `get` and `check` in resource pipeline.
- 2025-02-08:
  - fix: cleanup for docker namespaces needed to retry when multiple runs prunes
    were happening
  - feat: added support for environment variables
- 2025-02-07: Added volume support for containers and native. It scopes things
  into a `/tmp` directory.
- 2025-02-03: Added promise support for running multiple containers.
- 2025-02-02: Added validations to the YAML format.
- 2025-02-01:
  - Added support for volumes on the docker driver
  - Drop support for fly driver
  - cleanup volumes on test runs
- 2025-01-31: Added support for volumes on the native driver
- 2025-01-24: Add task support from a pipeline YAML. An `assert` can also be
  used to help with testing. Will probably not be useful in production
  environments.
- 2025-01-23: Added `assert` into the runtime, which will stop the runtime when
  something does not work. TODO: Print to the stdout/stderr of the pipeline.
- 2025-01-22:
  - Refactor runtime to use a _sandbox_ context. Its purpose is to represent the
    execution environment. It was a refactoring step to see if we could have the
    runtime be populated for tests, different orchestrators, and then for builds
    (like in a true CI/CD system).
  - Added typescript support with a node module to support the type definition.
- 2025-01-19: Initial shape of running containers across docker, fly.io, and
  native support. It was to ensure that it could be done. There is still an
  optimization to be done to get the fly.io logs.
