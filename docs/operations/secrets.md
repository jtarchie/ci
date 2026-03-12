# Secrets

The CI system supports encrypted secrets that are injected into pipeline tasks
as environment variables. Secrets are encrypted at rest using AES-256-GCM, so
they remain inaccessible even if someone has direct access to the underlying
storage.

## How It Works

1. **Storage**: Secrets are stored in an encrypted backend (SQLite or S3). Each
   secret is encrypted with a key derived from a passphrase you provide.
2. **Injection**: Pipelines reference secrets in `env` using a `secret:` prefix.
   At runtime, the system resolves the secret and injects the plaintext value
   into the container's environment.
3. **Redaction**: Any secret values that appear in task stdout or stderr are
   automatically replaced with `***REDACTED***` before being stored or returned.
4. **Fail Fast**: If a pipeline references a secret that doesn't exist, the
   pipeline fails immediately with a clear error message naming the missing key.

## Setting Secrets

Secrets can be set at two scopes: **pipeline** (only visible to one pipeline) or
**global** (visible to all pipelines, used as a fallback).

### Pipeline-Scoped Secrets (`pocketci run --secret`)

Pass secrets on the `pocketci run` command line using `--secret KEY=VALUE` (or
`-e KEY=VALUE`). These are scoped to the pipeline being run.

```bash
pocketci run pipeline.ts \
  --secrets "sqlite://secrets.db?key=my-passphrase" \
  --secret API_KEY=sk-1234567890 \
  --secret DB_PASSWORD=hunter2
```

### Global Secrets

Global secrets are shared across all pipelines. There are two ways to set them:

**Via the server command** (`pocketci server --secret`):

```bash
pocketci server \
  --secrets "sqlite://secrets.db?key=my-passphrase" \
  --secret SHARED_TOKEN=tok-global-abc \
  --secret REGISTRY_PASSWORD=ghp-xyz
```

**Via the runner** (`pocketci run --global-secret`):

```bash
pocketci run pipeline.ts \
  --secrets "sqlite://secrets.db?key=my-passphrase" \
  --global-secret SHARED_TOKEN=tok-global-abc \
  --secret PIPELINE_KEY=per-pipeline-only
```

At runtime, the system checks pipeline scope first, then falls back to global.
This means a pipeline can override a global secret with its own value.

### Environment Variables

The secrets DSN can also be configured via an environment variable, which is
useful for server mode or CI environments where you don't want to pass flags on
every invocation.

| Environment Variable | CLI Flag    | Description                           |
| -------------------- | ----------- | ------------------------------------- |
| `CI_SECRETS`         | `--secrets` | Secrets backend DSN (includes scheme) |

```bash
export CI_SECRETS="sqlite://secrets.db?key=my-passphrase"

pocketci run pipeline.ts --secret API_KEY=sk-1234567890
```

```bash
export CI_SECRETS="sqlite:///var/lib/pocketci/secrets.db?key=my-passphrase"

pocketci server --secret SHARED_TOKEN=tok-global-abc
```

## Backend Configuration

### SQLite

The `sqlite` backend stores secrets in a SQLite database, encrypted with
AES-256-GCM. The encryption key is derived from the passphrase in the DSN using
SHA-256.

**DSN Format**:

```
sqlite://<sqlite-path>?key=<passphrase>
```

| Component       | Description                           | Example                   |
| --------------- | ------------------------------------- | ------------------------- |
| `<sqlite-path>` | Path to the SQLite database file      | `secrets.db`, `/tmp/s.db` |
| `<passphrase>`  | Passphrase used to derive the AES key | `my-strong-passphrase`    |

**Examples**:

```bash
# File-based storage
--secrets "sqlite://secrets.db?key=my-passphrase"

# Absolute path
--secrets "sqlite:///var/lib/pocketci/secrets.db?key=my-passphrase"

# In-memory (useful for testing, secrets don't persist)
--secrets "sqlite://:memory:?key=test-key"
```

## Using Secrets in Pipelines

Any string value prefixed with `secret:` is resolved from the secrets backend
before it is used. This works across **task environment variables**, **native
resource configuration**, and **notification config fields**.

### Task Environment Variables

Reference secrets in a task's `env` map:

```typescript
const pipeline = async () => {
  let result = await runtime.run({
    name: "deploy",
    image: "alpine",
    command: {
      path: "sh",
      args: [
        "-c",
        'curl -H "Authorization: Bearer $API_KEY" https://api.example.com',
      ],
    },
    env: {
      API_KEY: "secret:API_KEY", // Resolved from secrets backend
      NODE_ENV: "production", // Plain value, passed as-is
    },
  });
};

export { pipeline };
```

### Native Resource Source and Params

Secret references work in the `source` and `params` maps of native resource
operations (`nativeResources.check`, `.fetch`, `.push`). Nested maps are walked
recursively — only string values with the `secret:` prefix are substituted;
non-string values such as numbers and booleans are left unchanged.

```typescript
const pipeline = async () => {
  // check — source credentials resolved from secrets
  const versions = nativeResources.check({
    type: "git",
    source: {
      uri: "https://github.com/my-org/private-repo.git",
      private_key: "secret:GIT_DEPLOY_KEY",
    },
  });

  // fetch — nested source + params both resolved
  const result = await nativeResources.fetch({
    type: "s3",
    source: {
      bucket: "my-bucket",
      credentials: {
        access_key: "secret:AWS_ACCESS_KEY",
        secret_key: "secret:AWS_SECRET_KEY",
      },
    },
    version: versions.versions[0],
    params: { unpack: true },
    destDir: "/workspace",
  });
};

export { pipeline };
```

### Notification Config Fields

Secret references work in notification backend configuration fields: `token`
(Slack), `webhook` (Teams), `url` (HTTP), and every entry in `headers` (HTTP).
The secret is resolved at the moment `notify.send()` is called, not when
`notify.setConfigs()` is called, so the stored config always uses the `secret:`
prefix string as a placeholder.

```typescript
const pipeline = async () => {
  notify.setConfigs({
    // Slack — token resolved from secrets
    "slack-builds": {
      type: "slack",
      token: "secret:SLACK_BOT_TOKEN",
      channels: ["#builds"],
    },
    // Microsoft Teams — webhook resolved from secrets
    "teams-alerts": {
      type: "teams",
      webhook: "secret:TEAMS_WEBHOOK_URL",
    },
    // HTTP — URL and Authorization header resolved from secrets
    "http-hook": {
      type: "http",
      url: "secret:WEBHOOK_URL",
      method: "POST",
      headers: {
        Authorization: "secret:WEBHOOK_TOKEN",
      },
    },
  });

  await notify.send({ name: "slack-builds", message: "Build started" });
};

export { pipeline };
```

## Scoping

Secrets are scoped to limit access:

- **Pipeline scope** (`pipeline/<id>`): Set via `pocketci run --secret`. Each
  pipeline only sees its own pipeline-scoped secrets.
- **Global scope** (`global`): Set via `pocketci server --secret` or
  `pocketci run --global-secret`. Shared across all pipelines.

The system checks pipeline scope first, then falls back to global. A
pipeline-scoped secret with the same key overrides its global counterpart.

## Output Redaction

Secret values are automatically scrubbed from pipeline output. If a task prints
a secret value to stdout or stderr, it is replaced with `***REDACTED***` before
the output is stored or displayed.

This uses longest-match-first ordering, so if one secret's value is a substring
of another, the longer value is redacted first to avoid partial matches.

## Full Example

```bash
# Set global secrets on the server
pocketci server \
  --secrets "sqlite://my-secrets.db?key=change-me-in-production" \
  --secret REGISTRY_TOKEN=ghp-abc123

# Set pipeline-scoped secrets and run
pocketci run examples/both/secrets-basic.ts \
  --driver docker \
  --secrets "sqlite://my-secrets.db?key=change-me-in-production" \
  --secret API_KEY=sk-live-abc123 \
  --global-secret WEBHOOK_TOKEN=whsec-xyz789
```

## Backend Configuration

### S3

The `s3` backend stores encrypted secrets as JSON objects in an S3-compatible
bucket. Every object is protected by two independent encryption layers:

1. **Application-layer AES-256-GCM** — applied by PocketCI before any bytes
   leave the process. Key derived from the `key=` passphrase.
2. **S3 Server-Side Encryption (SSE)** — enforced at construction time. If the
   provider does not support SSE, the driver refuses to start.

**DSN Format**:

```
s3://[http://|https://][ACCESS_KEY_ID:SECRET_ACCESS_KEY@]host[:port]/bucket[/prefix]?region=...&sse=AES256&key=passphrase
```

| Parameter        | Description                           | Required | Example                        |
| ---------------- | ------------------------------------- | -------- | ------------------------------ |
| `sse`            | `AES256` or `aws:kms` (mandatory)     | ✅       | `sse=AES256`                   |
| `key`            | Passphrase for app-layer AES-256-GCM  | ✅       | `key=my-strong-passphrase`     |
| `region`         | AWS region                            | —        | `region=us-east-1`             |
| `sse_kms_key_id` | KMS key ARN (only with `sse=aws:kms`) | —        | `sse_kms_key_id=arn:aws:kms:…` |

**Examples**:

```bash
# AWS S3 with AES256 SSE
--secrets "s3://s3.amazonaws.com/my-secrets-bucket?region=us-east-1&sse=AES256&key=my-passphrase"

# MinIO (local) with inline credentials
--secrets "s3://http://minioadmin:minioadmin@localhost:9000/secrets?region=us-east-1&sse=AES256&key=my-passphrase"

# Cloudflare R2
--secrets "s3://https://AKID:SECRET@ACCOUNT_ID.r2.cloudflarestorage.com/secrets?region=auto&sse=AES256&key=my-passphrase"
```

Object layout within the bucket:

```
[prefix/]secrets/{scope}/{url-encoded-key}.json
```

## Architecture

The secrets system follows the same pluggable backend pattern as the
`orchestra/` drivers:

```
secrets/
  secrets.go          # Manager interface, Register/New registry
  encryption.go       # AES-256-GCM encryption primitives
  sqlite/
    sqlite.go         # SQLite-backed encrypted backend (self-registers via init())
  s3/
    s3.go             # S3-backed double-encrypted backend (self-registers via init())
```

New backends (e.g., HashiCorp Vault, AWS Secrets Manager) can be added by
implementing the `secrets.Manager` interface and calling `secrets.Register()` in
an `init()` function. See
[implementing-driver](../drivers/implementing-driver.md) for the analogous
pattern used by orchestra drivers.
