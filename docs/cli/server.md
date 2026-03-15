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

### OAuth Options

- `--oauth-github-client-id` — GitHub OAuth app client ID (env:
  `CI_OAUTH_GITHUB_CLIENT_ID`)
- `--oauth-github-client-secret` — GitHub OAuth app client secret (env:
  `CI_OAUTH_GITHUB_CLIENT_SECRET`)
- `--oauth-gitlab-client-id` — GitLab OAuth app client ID (env:
  `CI_OAUTH_GITLAB_CLIENT_ID`)
- `--oauth-gitlab-client-secret` — GitLab OAuth app client secret (env:
  `CI_OAUTH_GITLAB_CLIENT_SECRET`)
- `--oauth-gitlab-url` — GitLab instance URL for self-hosted (env:
  `CI_OAUTH_GITLAB_URL`)
- `--oauth-microsoft-client-id` — Microsoft OAuth app client ID (env:
  `CI_OAUTH_MICROSOFT_CLIENT_ID`)
- `--oauth-microsoft-client-secret` — Microsoft OAuth app client secret (env:
  `CI_OAUTH_MICROSOFT_CLIENT_SECRET`)
- `--oauth-microsoft-tenant` — Microsoft tenant ID (env:
  `CI_OAUTH_MICROSOFT_TENANT`)
- `--oauth-session-secret` — session/JWT signing secret (env:
  `CI_OAUTH_SESSION_SECRET`)
- `--oauth-callback-url` — public callback URL for OAuth redirects (env:
  `CI_OAUTH_CALLBACK_URL`)
- `--server-rbac` — server-wide RBAC expression (env: `CI_SERVER_RBAC`)

> **Note:** Basic auth and OAuth are mutually exclusive. You cannot enable both
> at the same time.

See [Authentication](../operations/authentication.md) and
[Authorization](../operations/rbac.md) for full details.

## Example

```bash
pocketci server \
  --port 8080 \
  --storage sqlite://pocketci.db \
  --allowed-drivers docker \
  --basic-auth-username admin \
  --basic-auth-password secret123
```

With OAuth:

```bash
pocketci server \
  --port 8080 \
  --storage sqlite://pocketci.db \
  --allowed-drivers docker \
  --oauth-github-client-id YOUR_CLIENT_ID \
  --oauth-github-client-secret YOUR_CLIENT_SECRET \
  --oauth-session-secret "$(openssl rand -hex 32)" \
  --oauth-callback-url https://ci.example.com
```

The server provides:

- Web UI at `http://localhost:8080/pipelines/`
- JSON API at `http://localhost:8080/api/`
- Webhook endpoint at `http://localhost:8080/api/webhooks/:pipeline-id`
- MCP endpoint at `http://localhost:8080/mcp`

See [Server API](../api/index.md) for full endpoint documentation.
