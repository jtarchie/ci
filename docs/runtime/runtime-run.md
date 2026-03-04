# runtime.run()

Execute a container task.

```typescript
const result = await runtime.run(options);
```

## Options

- `name` — task name (string)
- `image` — container image (e.g., `"alpine:latest"`, required)
- `command` — command to run (required)
  - `path` — executable or script path
  - `args` — command arguments (array)
- `env` (optional) — environment variables object (supports `secret:KEY` prefix)
- `mounts` (optional) — volume mounts: `{ "/container/path": volumeHandle }`
- `caches` (optional) — cache paths (for S3-backed caching)
- `inputVariables` (optional) — named inputs for resource operations

## Return Value

```typescript
{
  code: number;          // exit code
  stdout: string;        // captured stdout (redacted if secrets used)
  stderr: string;        // captured stderr (redacted)
  startedAt: string;     // ISO timestamp
  endedAt: string;       // ISO timestamp
}
```

## Example

```typescript
const result = await runtime.run({
  name: "test",
  image: "golang:1.22",
  command: { path: "go", args: ["test", "./..."] },
  env: {
    GOFLAGS: "-race",
    DB_PASSWORD: "secret:db_password"  // resolved at runtime
  }
});

if (result.code !== 0) {
  throw new Error(`tests failed: ${result.stderr}`);
}
```

See [Secrets](../operations/secrets.md) for secret injection details.
