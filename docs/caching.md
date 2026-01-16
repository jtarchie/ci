# Volume Caching

The CI system supports transparent volume caching backed by S3-compatible
storage. Caches persist data across pipeline runs, making subsequent runs faster
by restoring previously computed artifacts, dependencies, or build outputs.

## How It Works

1. **Volume Creation**: When a pipeline creates a volume (directly or via
   `caches` in YAML), the system checks if a cached version exists in S3.
2. **Cache Restore**: If found, the cached data is downloaded, decompressed, and
   extracted into the volume before the task runs.
3. **Cache Persist**: When the pipeline completes, all volumes are persisted
   back to S3 with compression.

This is transparent to the pipeline — volumes behave identically whether caching
is enabled or not.

## Configuration

Caching is configured via the driver DSN using query parameters:

```bash
--driver=docker://?cache=s3://bucket-name&cache_compression=zstd&cache_prefix=myproject
```

### DSN Parameters

| Parameter           | Description                         | Default | Example                                          |
| ------------------- | ----------------------------------- | ------- | ------------------------------------------------ |
| `cache`             | S3 URL for cache storage (required) | —       | `s3://my-cache-bucket`                           |
| `cache_compression` | Compression algorithm               | `zstd`  | `zstd`, `gzip`, `none`                           |
| `cache_prefix`      | Key prefix for all cache entries    | `""`    | `myproject` → keys become `myproject/volume.tar` |

### S3 URL Format

```
s3://bucket-name/optional-prefix?region=us-east-1&endpoint=http://localhost:9000&ttl=24h
```

| Parameter  | Description                          | Default         | Example                 |
| ---------- | ------------------------------------ | --------------- | ----------------------- |
| `region`   | AWS region                           | AWS SDK default | `us-east-1`             |
| `endpoint` | Custom S3 endpoint (for MinIO, etc.) | AWS S3          | `http://localhost:9000` |
| `ttl`      | Cache expiration duration            | No expiration   | `24h`, `7d`, `168h`     |

## Full Examples

### AWS S3

```bash
ci run pipeline.yml \
  --driver='docker://?cache=s3://my-ci-cache?region=us-west-2&cache_prefix=project-a'
```

### MinIO (Local S3-Compatible)

```bash
# Start MinIO locally
docker run -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin \
  -e MINIO_ROOT_PASSWORD=minioadmin \
  minio/minio server /data --console-address ":9001"

# Create bucket
aws --endpoint-url http://localhost:9000 s3 mb s3://cache-bucket

# Run with caching
ci run pipeline.yml \
  --driver='docker://?cache=s3://cache-bucket?endpoint=http://localhost:9000&region=us-east-1'
```

### With Compression Options

```bash
# Use gzip instead of zstd
ci run pipeline.yml \
  --driver='docker://?cache=s3://bucket&cache_compression=gzip'

# Disable compression (faster for already-compressed data)
ci run pipeline.yml \
  --driver='docker://?cache=s3://bucket&cache_compression=none'
```

## YAML Pipeline Usage

Use the `caches` field in task configs to define cache directories:

```yaml
jobs:
  - name: build
    plan:
      - task: install-deps
        config:
          platform: linux
          image_resource:
            type: registry-image
            source:
              repository: node:20
          caches:
            - path: node_modules
            - path: .npm
          run:
            path: sh
            args:
              - -c
              - |
                  npm ci
                  npm run build
```

### Cache Behavior

- **Path**: Relative to the task's working directory
- **Name**: Derived from the path (e.g., `node_modules` → `cache-node_modules`)
- **Sharing**: Caches with the same name share data across tasks in the same
  pipeline run
- **Persistence**: Caches are uploaded to S3 when the pipeline completes

### Multiple Caches

```yaml
caches:
  - path: .cache/go-build # Go build cache
  - path: .cache/golangci # Linter cache
  - path: vendor # Vendored dependencies
```

## TypeScript/JavaScript Usage

For direct JS/TS pipelines, create named volumes:

```typescript
const pipeline = async () => {
  // Create a cached volume
  const cache = await runtime.createVolume({ name: "build-cache" });

  // Use the volume in a task
  await runtime.run({
    name: "build",
    image: "node:20",
    command: { path: "npm", args: ["run", "build"] },
    mounts: [{ name: cache.name, path: "node_modules" }],
  });
};

export { pipeline };
```

## Supported Drivers

Caching works with drivers that implement `VolumeDataAccessor`:

| Driver   | Caching Support | Notes                                      |
| -------- | --------------- | ------------------------------------------ |
| `docker` | ✅ Yes          | Uses `docker cp` for volume data transfer  |
| `native` | ✅ Yes          | Uses tar directly on the filesystem        |
| `k8s`    | ✅ Yes          | Uses a helper pod for volume data transfer |

## Cache Key Structure

Cache keys are structured as:

```
{cache_prefix}/{volume_name}.tar.{compression}
```

Examples:

- `myproject/cache-node_modules.tar.zst`
- `build-cache.tar.zst` (no prefix)
- `ci/main/vendor.tar.gzip`

## Environment Variables

AWS credentials can be provided via standard AWS SDK environment variables:

```bash
export AWS_ACCESS_KEY_ID=your-key
export AWS_SECRET_ACCESS_KEY=your-secret
export AWS_REGION=us-east-1

ci run pipeline.yml --driver='docker://?cache=s3://bucket'
```

Or use IAM roles, instance profiles, or other AWS SDK credential sources.

## Troubleshooting

### Cache Not Being Restored

1. **Check cache key**: Ensure `cache_prefix` and volume names match between
   runs
2. **Verify S3 access**: Check AWS credentials and bucket permissions
3. **Check logs**: Look for "cache miss" or "restoring volume from cache"
   messages

### Cache Not Being Persisted

1. **Pipeline must complete**: Caches are persisted when the pipeline finishes
2. **Check S3 write permissions**: Ensure the credentials allow `PutObject`
3. **Check logs**: Look for "persisting volume to cache" messages

### Performance Tips

- Use `zstd` compression (default) for best speed/ratio balance
- Use `none` compression for already-compressed data (tar.gz archives, etc.)
- Set appropriate `ttl` to automatically expire stale caches
- Use specific cache paths rather than caching entire directories
