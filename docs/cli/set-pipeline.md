# ci set-pipeline

Store a pipeline on a remote CI server.

```bash
ci set-pipeline <pipeline-file> --server <url> [options]
```

## Options

- `--server` — server URL (required; e.g., `http://localhost:8080`)
- `--name` — pipeline name (if omitted, derived from filename)
- `--driver` — orchestration driver DSN
- `--webhook-secret` — secret for webhook requests (optional)
- `--basic-auth-username` — server basic auth user (env: `CI_BASIC_AUTH_USERNAME`)
- `--basic-auth-password` — server basic auth password (env: `CI_BASIC_AUTH_PASSWORD`)

## Example

```bash
ci set-pipeline my-pipeline.ts \
  --server http://localhost:8080 \
  --name my-pipeline \
  --driver docker?// \
  --webhook-secret my-secret-key
```

Once stored, trigger with `ci run`:

```bash
ci run my-pipeline --server-url http://localhost:8080
```
