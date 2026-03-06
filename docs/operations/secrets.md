# Secrets

The CI system supports encrypted secrets that are injected into pipeline tasks
as environment variables. Secrets are encrypted at rest using AES-256-GCM, so
they remain inaccessible even if someone has direct access to the underlying
storage.

## How It Works

1. **Storage**: Secrets are stored in an encrypted backend (currently SQLite).
   Each secret is encrypted with a key derived from a passphrase you provide.
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

### Pipeline-Scoped Secrets (`ci run --secret`)

Pass secrets on the `ci run` command line using `--secret KEY=VALUE` (or
`-e KEY=VALUE`). These are scoped to the pipeline being run.

```bash
ci run pipeline.ts \
  --secrets "local://secrets.db?key=my-passphrase" \
  --secret API_KEY=sk-1234567890 \
  --secret DB_PASSWORD=hunter2
```

### Global Secrets

Global secrets are shared across all pipelines. There are two ways to set them:

**Via the server command** (`ci server --secret`):

```bash
ci server \
  --secrets "local://secrets.db?key=my-passphrase" \
  --secret SHARED_TOKEN=tok-global-abc \
  --secret REGISTRY_PASSWORD=ghp-xyz
```

**Via the runner** (`ci run --global-secret`):

```bash
ci run pipeline.ts \
  --secrets "local://secrets.db?key=my-passphrase" \
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
export CI_SECRETS="local://secrets.db?key=my-passphrase"

ci run pipeline.ts --secret API_KEY=sk-1234567890
```

```bash
export CI_SECRETS="local:///var/lib/ci/secrets.db?key=my-passphrase"

ci server --secret SHARED_TOKEN=tok-global-abc
```

## Backend Configuration

### Local (SQLite)

The `local` backend stores secrets in a SQLite database, encrypted with
AES-256-GCM. The encryption key is derived from the passphrase in the DSN using
SHA-256.

**DSN Format**:

```
local://<sqlite-path>?key=<passphrase>
```

| Component       | Description                           | Example                   |
| --------------- | ------------------------------------- | ------------------------- |
| `<sqlite-path>` | Path to the SQLite database file      | `secrets.db`, `/tmp/s.db` |
| `<passphrase>`  | Passphrase used to derive the AES key | `my-strong-passphrase`    |

**Examples**:

```bash
# File-based storage
--secrets "local://secrets.db?key=my-passphrase"

# Absolute path
--secrets "local:///var/lib/ci/secrets.db?key=my-passphrase"

# In-memory (useful for testing, secrets don't persist)
--secrets "local://:memory:?key=test-key"
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

- **Pipeline scope** (`pipeline/<id>`): Set via `ci run --secret`. Each pipeline
  only sees its own pipeline-scoped secrets.
- **Global scope** (`global`): Set via `ci server --secret` or
  `ci run --global-secret`. Shared across all pipelines.

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
ci server \
  --secrets "local://my-secrets.db?key=change-me-in-production" \
  --secret REGISTRY_TOKEN=ghp-abc123

# Set pipeline-scoped secrets and run
ci run examples/both/secrets-basic.ts \
  --driver docker \
  --secrets "local://my-secrets.db?key=change-me-in-production" \
  --secret API_KEY=sk-live-abc123 \
  --global-secret WEBHOOK_TOKEN=whsec-xyz789
```

## Architecture

The secrets system follows the same pluggable backend pattern as the
`orchestra/` drivers:

```
secrets/
  secrets.go          # Manager interface, Register/New registry
  encryption.go       # AES-256-GCM encryption primitives
  local/
    local.go          # SQLite-backed encrypted backend (self-registers via init())
```

New backends (e.g., HashiCorp Vault, AWS Secrets Manager) can be added by
implementing the `secrets.Manager` interface and calling `secrets.Register()` in
an `init()` function. See
[implementing-driver](../drivers/implementing-driver.md) for the analogous
pattern used by orchestra drivers.
