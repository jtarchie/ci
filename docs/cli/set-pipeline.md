# pocketci set-pipeline

Store a pipeline on a remote CI server.

```bash
pocketci set-pipeline <pipeline-file> --server <url> [options]
```

## Options

- `--server` — server URL (required; e.g., `http://localhost:8080`)
- `--name` — pipeline name (if omitted, derived from filename)
- `--driver` — orchestration driver DSN
- `--webhook-secret` — secret for webhook requests (optional)
- `--basic-auth-username` — server basic auth user (env:
  `CI_BASIC_AUTH_USERNAME`)
- `--basic-auth-password` — server basic auth password (env:
  `CI_BASIC_AUTH_PASSWORD`)
- `--secret` — set pipeline secret (repeatable; format: `KEY=VALUE`)
- `--secret-file` — load secrets from a file (repeatable; format:
  `KEY=filepath`)
- `--resume` — enable resume support for the pipeline
- `--rbac` — RBAC expression restricting pipeline access (env: `CI_RBAC`)
- `--auth-token` — JWT auth token (env: `CI_AUTH_TOKEN`)
- `--config-file` — auth config file path (env: `CI_AUTH_CONFIG`; default:
  `~/.pocketci/auth.config`)

## Example

```bash
pocketci set-pipeline my-pipeline.ts \
  --server http://localhost:8080 \
  --name my-pipeline \
  --driver docker?// \
  --webhook-secret my-secret-key
```

Once stored, trigger with `pocketci run`:

```bash
pocketci run my-pipeline --server-url http://localhost:8080
```

## Authentication

With OAuth-enabled servers, authenticate first with `pocketci login`:

```bash
pocketci login -s https://ci.example.com
pocketci set-pipeline my-pipeline.ts -s https://ci.example.com
```

Or provide a token directly:

```bash
pocketci set-pipeline my-pipeline.ts \
  --server https://ci.example.com \
  --auth-token eyJhbGciOiJIUzI1NiIs...
```

## RBAC

Restrict who can access a pipeline:

```bash
pocketci set-pipeline my-pipeline.ts \
  --server https://ci.example.com \
  --rbac '"deploy-team" in Organizations'
```

See [Authorization](../operations/rbac.md) for expression syntax.
