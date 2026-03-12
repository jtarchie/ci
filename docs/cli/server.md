# pocketci server

Start an HTTP server that manages and executes pipelines.

```bash
pocketci server [options]
```

## Options

- `--port` — HTTP port (default: `8080`)
- `--storage` — persistence backend DSN (default: `sqlite://pocketci.db`).
  Supports `sqlite://` and `s3://` backends. See
  [Storage](../operations/storage.md).
- `--driver` — default orchestration driver
- `--allowed-drivers` — comma-separated list of drivers to allow
- `--allowed-features` — comma-separated list of feature gates to enable
- `--secret` — set global secret (repeatable; format: `KEY=VALUE`)
- `--secrets` — secrets backend DSN (e.g., `sqlite://secrets.db?key=passphrase`)
- `--basic-auth-username` — require basic auth on web UI (env:
  `CI_BASIC_AUTH_USERNAME`)
- `--basic-auth-password` — basic auth password (env: `CI_BASIC_AUTH_PASSWORD`)
- `--webhook-timeout` — time allowed for `http.respond()` in webhooks (default:
  `5s`)
- `--log-level` — log level (`debug`, `info`, `warn`, `error`)
- `--log-format` — log format (`json` or text)

## Example

```bash
pocketci server \
  --port 8080 \
  --storage sqlite://pocketci.db \
  --allowed-drivers docker \
  --basic-auth-username admin \
  --basic-auth-password secret123
```

The server provides:

- Web UI at `http://localhost:8080/pipelines/`
- JSON API at `http://localhost:8080/api/`
- Webhook endpoint at `http://localhost:8080/api/webhooks/:pipeline-id`

See [Server API](../api/index.md) for full endpoint documentation.
