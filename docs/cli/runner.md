# pocketci runner

Execute a pipeline locally in a single iteration.

```bash
pocketci runner <pipeline-file> [options]
```

## Options

- `--driver` — orchestration driver (`docker`, `native`, `k8s`, etc.; default:
  `docker`)
- `--storage` — persistence backend (default: `sqlite://test.db`)
- `--secret` — set pipeline-scoped secret (repeatable; format: `KEY=VALUE`)
- `--global-secret` — set global secret (repeatable)
- `--secrets` — secrets backend DSN (e.g., `sqlite://secrets.db?key=passphrase`)
- `--log-level` — log level (`debug`, `info`, `warn`, `error`)
- `--log-format` — log format (`json` or text)

## Example

```bash
pocketci runner examples/both/hello-world.ts --driver native --log-level debug
```

See [Secrets](../operations/secrets.md) for details on secret handling.
