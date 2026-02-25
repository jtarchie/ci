# Documentation

## Development Changelog

This is a development repository for CI/CD system. These notes can probably be
inferred from the `git` commit messages. I'm journaling for my own benefit.

- 2026-02-25:
  - Added `ci run` command for remote pipeline execution by name. Pipelines are
    stored on the server via `ci set-pipeline`; `ci run` triggers them over HTTP
    and streams output back in real time. See [run.md](run.md) for details.
- 2026-02-22:
  - Added encrypted secrets management with AES-256-GCM and pluggable backends.
    See [secrets.md](secrets.md) for details.
- 2026-02-16:
  - Added QEMU driver for running pipelines inside local VMs. Uses QGA for
    command execution, 9p virtfs for volumes, and cloud-init for guest setup.
    See [driver-dsn.md](driver-dsn.md) for configuration details.
- 2026-01-15:
  - Added S3-backed volume caching with zstd/gzip compression. See
    [caching.md](caching.md) for details.
  - Added `caches` field to YAML task configs for declarative cache paths.
- 2025-03-10:
  - Support for task level `on_success`, `on_failure`, `on_error`, and
    `on_abort`.
  - Added `try`, `do`, and `in_parallel`.
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
