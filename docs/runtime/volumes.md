# Volumes

Manage shared storage between tasks.

## runtime.createVolume()

Create a volume for inter-task communication.

```typescript
const vol = await runtime.createVolume(name, sizeGiB);
```

- `name` — volume identifier
- `sizeGiB` — quota in gigabytes
- Returns a handle for use in `runtime.run()` mounts

## Example

```typescript
const vol = await runtime.createVolume("workspace", 50);

await runtime.run({
  name: "setup",
  image: "alpine",
  command: { path: "mkdir", args: ["-p", "/data"] },
  mounts: { "/data": vol }
});

await runtime.run({
  name: "consume",
  image: "alpine",
  command: { path: "cat", args: ["/data/output.txt"] },
  mounts: { "/data": vol }
});
```

Volumes are scoped to a single pipeline execution. After the pipeline completes, volumes are cleaned up by the driver.
