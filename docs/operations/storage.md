# Storage Backends

PocketCI persists pipelines, runs, and task data using a pluggable storage
backend. The backend is selected via the `--storage` DSN flag on the
[server](../cli/server.md) command.

## SQLite (default)

```bash
pocketci server --storage sqlite://pocketci.db
```

SQLite is the default backend. It stores all data in a single file with
full-text search (FTS5) for pipeline and run search queries. Use
`sqlite://:memory:` for ephemeral in-memory storage (useful for testing).

## S3

```bash
pocketci server --storage "s3://s3.amazonaws.com/bucket-name?region=us-east-1"
```

The S3 backend stores all data as JSON objects in an S3-compatible bucket. It
works with AWS S3, MinIO, R2, and any S3-compatible object store.

### DSN Format

```
s3://[http://|https://][ACCESS_KEY_ID:SECRET_ACCESS_KEY@]host[:port]/bucket[/prefix]?region=us-east-1
```

The host portion is the S3 endpoint. Prefix it with `http://` or `https://` to
control the transport scheme — if no scheme is given, `https://` is assumed.
Credentials may be embedded as `id:secret@` userinfo immediately after the
scheme.

| Parameter          | Description                                                                       | Default                 | Example                        |
| ------------------ | --------------------------------------------------------------------------------- | ----------------------- | ------------------------------ |
| `region`           | AWS region                                                                        | AWS SDK default         | `us-east-1`                    |
| `force_path_style` | Force path-style URLs (`true`/`false`). Auto-enabled when a custom host is given. | `true` when custom host | `false` for virtual-host style |
| `encrypt`          | Provider SSE: `sse-s3` (AES-256), `sse-kms` (KMS), or `sse-c` (customer key)      | None (no SSE headers)   | `sse-s3`                       |
| `sse_kms_key_id`   | KMS key ARN/ID (only with `encrypt=sse-kms`; omit for provider default key)       | Provider default key    | `arn:aws:kms:…:key/mrk-abc`    |
| `key`              | Customer-provided key passphrase (required for `encrypt=sse-c`)                   | —                       | `my-passphrase`                |

The URL path component (`/optional/prefix`) scopes all objects under a key
prefix, allowing multiple PocketCI instances to share a single bucket.

### Data Layout

Objects are stored at the following paths within the bucket (after any
configured prefix):

| Data      | Key Pattern                              |
| --------- | ---------------------------------------- |
| Tasks     | `tasks/{namespace}/{key-hierarchy}.json` |
| Pipelines | `pipelines/by-id/{id}.json`              |
|           | `pipelines/by-name/{name}.json`          |
| Runs      | `runs/{id}.json`                         |

### Authentication

Credentials can be embedded directly in the DSN or supplied via the standard AWS
SDK credential chain (environment variables, `~/.aws/credentials`, IAM role):

```bash
# Inline credentials
pocketci server \
  --storage "s3://http://ACCESS_KEY:SECRET@localhost:9000/bucket?region=us-east-1"

# Environment variable credential chain
export AWS_ACCESS_KEY_ID=your-key
export AWS_SECRET_ACCESS_KEY=your-secret
export AWS_REGION=us-east-1
```

### Search Behavior

Search uses S3 `ListObjectsV2` with prefix filtering. Unlike the SQLite backend,
full-text search is not available — queries match against object content using
simple substring matching after listing.

### Examples

**AWS S3:**

```bash
pocketci server \
  --storage "s3://s3.amazonaws.com/my-ci-bucket/production?region=us-west-2"
```

**MinIO (local development):**

```bash
pocketci server \
  --storage "s3://http://minioadmin:minioadmin@localhost:9000/ci-data?region=us-east-1"
```

**Cloudflare R2:**

```bash
pocketci server \
  --storage "s3://https://AKID:SECRET@ACCOUNT_ID.r2.cloudflarestorage.com/ci-data?region=auto"
```

**AWS S3 with SSE-S3 (AES-256) encryption:**

```bash
pocketci server \
  --storage "s3://s3.amazonaws.com/my-ci-bucket/production?region=us-east-1&encrypt=sse-s3"
```

**AWS S3 with SSE-KMS (default KMS key):**

```bash
pocketci server \
  --storage "s3://s3.amazonaws.com/my-ci-bucket/production?region=us-east-1&encrypt=sse-kms"
```

**AWS S3 with SSE-KMS (specific KMS key):**

```bash
pocketci server \
  --storage "s3://s3.amazonaws.com/my-ci-bucket/production?region=us-east-1&encrypt=sse-kms&sse_kms_key_id=arn:aws:kms:us-east-1:123456789012:key/mrk-abc123"
```

**AWS S3 with SSE-C (customer-provided key):**

```bash
pocketci server \
  --storage "s3://s3.amazonaws.com/my-ci-bucket/production?region=us-east-1&encrypt=sse-c&key=my-passphrase"
```

**Shared bucket with prefix isolation:**

```bash
# Team A
pocketci server --storage "s3://s3.amazonaws.com/shared-bucket/team-a"

# Team B
pocketci server --storage "s3://s3.amazonaws.com/shared-bucket/team-b"
```

### Trade-offs

| Feature     | SQLite           | S3                      |
| ----------- | ---------------- | ----------------------- |
| Search      | Full-text (FTS5) | Prefix + substring      |
| Latency     | Local disk       | Network round-trip      |
| Concurrency | Single writer    | Last-writer-wins        |
| Persistence | Local file       | Durable object store    |
| Scaling     | Single node      | Shared across instances |
