# pocketci run

Execute a stored pipeline by name on a remote CI server.

```bash
pocketci run <name> [args...] --server-url <url> [options]
```

## Options

- `--server-url` — CI server URL (required; env: `CI_SERVER_URL`)
- `--timeout` — client-side deadline (env: `CI_TIMEOUT`)

All positional arguments after `<name>` are passed to the pipeline as
`pipelineContext.args`.

## Example

```bash
pocketci run my-pipeline arg1 arg2 --server-url http://localhost:8080
```

Inside the pipeline, `pipelineContext.args === ["arg1", "arg2"]`.

See [Run Pipelines](../guides/run.md) for detailed examples including k6 load
testing and background execution.
