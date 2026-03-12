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
pocketci server --storage "s3://bucket-name?region=us-east-1"
```

The S3 backend stores all data as JSON objects in an S3-compatible bucket. It
works with AWS S3, MinIO, R2, and any S3-compatible object store.

### DSN Format

```
s3://bucket-name/optional/prefix?region=us-east-1&endpoint=http://localhost:9000
```

| Parameter  | Description                          | Default         | Example                 |
| ---------- | ------------------------------------ | --------------- | ----------------------- |
| `region`   | AWS region                           | AWS SDK default | `us-east-1`             |
| `endpoint` | Custom S3 endpoint (for MinIO, etc.) | AWS S3          | `http://localhost:9000` |

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

The S3 driver uses the standard AWS SDK credential chain. Configure credentials
via environment variables or AWS config files:

```bash
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
  --storage "s3://my-ci-bucket/production?region=us-west-2"
```

**MinIO (local development):**

```bash
pocketci server \
  --storage "s3://ci-data?region=us-east-1&endpoint=http://localhost:9000"
```

**Shared bucket with prefix isolation:**

```bash
# Team A
pocketci server --storage "s3://shared-bucket/team-a"

# Team B
pocketci server --storage "s3://shared-bucket/team-b"
```

### Trade-offs

| Feature     | SQLite           | S3                      |
| ----------- | ---------------- | ----------------------- |
| Search      | Full-text (FTS5) | Prefix + substring      |
| Latency     | Local disk       | Network round-trip      |
| Concurrency | Single writer    | Last-writer-wins        |
| Persistence | Local file       | Durable object store    |
| Scaling     | Single node      | Shared across instances |
