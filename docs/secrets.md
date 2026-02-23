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

There are two ways to set secrets: via the CLI or via environment variables.

### 1. CLI Flags (`--secret`)

Pass secrets directly on the command line using `--secret KEY=VALUE` (or
`-e KEY=VALUE`). This flag can be repeated for multiple secrets. The `--secrets`
flag configures the backend DSN (the backend name is inferred from the scheme).

```bash
ci run pipeline.ts \
  --secrets "local://secrets.db?key=my-passphrase" \
  --secret API_KEY=sk-1234567890 \
  --secret DB_PASSWORD=hunter2
```

Each `--secret` flag stores the value (encrypted) into the backend before the
pipeline executes. The secret is scoped to the pipeline being run.

### 2. Environment Variables

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

This also works with the server command:

```bash
export CI_SECRETS="local:///var/lib/ci/secrets.db?key=my-passphrase"

ci server --port 8080
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

Reference secrets in a task's `env` map by prefixing the value with `secret:`.
The system resolves the actual value at runtime.

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
      API_KEY: "secret:API_KEY",
      DB_PASSWORD: "secret:DB_PASSWORD",
    },
  });
};

export { pipeline };
```

Regular (non-secret) environment variables work as before — only values with the
`secret:` prefix trigger a secret lookup:

```typescript
env: {
  API_KEY: "secret:API_KEY",   // Resolved from secrets backend
  NODE_ENV: "production",      // Plain value, passed as-is
}
```

## Scoping

Secrets are scoped to limit access:

- **Pipeline scope** (`pipeline/<id>`): Secrets set via `--secret` are scoped to
  the pipeline being run. Each pipeline only sees its own secrets.
- **Global scope** (`global`): Shared secrets accessible by all pipelines. The
  system checks pipeline scope first, then falls back to global scope.

## Output Redaction

Secret values are automatically scrubbed from pipeline output. If a task prints
a secret value to stdout or stderr, it is replaced with `***REDACTED***` before
the output is stored or displayed.

This uses longest-match-first ordering, so if one secret's value is a substring
of another, the longer value is redacted first to avoid partial matches.

## Full Example

```bash
# Set secrets and run a pipeline
ci run examples/both/secrets-basic.ts \
  --driver docker \
  --secrets "local://my-secrets.db?key=change-me-in-production" \
  --secret API_KEY=sk-live-abc123 \
  --secret WEBHOOK_TOKEN=whsec-xyz789
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
[implementing-new-driver.md](implementing-new-driver.md) for the analogous
pattern used by orchestra drivers.
