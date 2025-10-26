# Implementing a New Orchestra Driver

This guide explains how to implement a new driver for the Orchestra container
orchestration system.

## Overview

Orchestra drivers provide an abstraction layer for running containers across
different platforms (Docker, cloud providers, local processes, etc.). All
drivers must implement the same interface to ensure consistent behavior across
platforms.

## Core Interfaces

### Driver Interface

The main driver interface that you must implement:

```go
type Driver interface {
    Close() error
    CreateVolume(ctx context.Context, name string, size int) (Volume, error)
    Name() string
    RunContainer(ctx context.Context, task Task) (Container, error)
}
```

### Container Interface

Containers returned by `RunContainer()` must implement:

```go
type Container interface {
    Cleanup(ctx context.Context) error
    Logs(ctx context.Context, stdout, stderr io.Writer) error
    Status(ctx context.Context) (ContainerStatus, error)
}
```

### ContainerStatus Interface

Status objects returned by `Container.Status()` must implement:

```go
type ContainerStatus interface {
    IsDone() bool
    ExitCode() int
}
```

### Volume Interface

Volumes returned by `CreateVolume()` must implement:

```go
type Volume interface {
    Cleanup(ctx context.Context) error
    Name() string
}
```

## Task Structure

Your driver will receive tasks with the following structure:

```go
type Task struct {
    Command         []string           // Command to execute in container
    ContainerLimits ContainerLimits    // CPU/Memory limits
    Env             map[string]string  // Environment variables
    ID              string             // Unique task identifier
    Image           string             // Container image to use
    Mounts          Mounts             // Volume mounts
    Privileged      bool               // Run with elevated privileges
    Stdin           io.Reader          // Standard input stream (optional)
    User            string             // User to run as (optional)
}

type Mount struct {
    Name string  // Volume name
    Path string  // Mount path in container
}

type ContainerLimits struct {
    CPU    int64  // CPU shares (0 means unlimited)
    Memory int64  // Memory in bytes (0 means unlimited)
}
```

## Implementation Requirements

### 1. Driver Registration

Register your driver using the `init()` function pattern:

```go
package yourdriver

import (
    "log/slog"
    "github.com/jtarchie/ci/orchestra"
)

func init() {
    orchestra.Add("yourdriver", NewYourDriver)
}

func NewYourDriver(namespace string, logger *slog.Logger) (orchestra.Driver, error) {
    // Initialize your driver
    return &YourDriver{
        namespace: namespace,
        logger:    logger,
    }, nil
}
```

### 2. Namespace Isolation

The `namespace` parameter is critical for resource isolation. Use it to:

- Tag/label all created resources (containers, volumes)
- Filter resources during cleanup operations
- Prevent conflicts between test runs or different pipeline executions

Example naming pattern: `{namespace}-{task.ID}`

### 3. RunContainer() Behavior

**Idempotency**: Running the same task (same `task.ID`) multiple times should:

- Return the existing container if it already exists
- Not create duplicate containers
- Return consistent results

**Expected flow**:

1. Check if container with this task ID already exists
2. If exists, return existing container
3. If not exists:
   - Pull/prepare the image
   - Create any required volumes from `task.Mounts`
   - Start the container with specified command
   - Return container handle immediately (don't wait for completion)

### 4. Container.Status() Behavior

Must be **pollable** - tests will call this repeatedly until `IsDone()` returns
true.

**Requirements**:

- Return current execution state
- `IsDone()` should return `true` when container has finished (success or
  failure)
- `ExitCode()` should return the actual exit code (0 for success, non-zero for
  errors)
- Should work correctly even after container has stopped
- Be mindful of rate limits - implement caching or throttling if needed

### 5. Container.Logs() Behavior

**Requirements**:

- Write stdout to the `stdout` writer
- Write stderr to the `stderr` writer
- Should work even after container has stopped
- Return logs from the beginning of execution
- Can be called multiple times with consistent results

### 6. Container.Cleanup() Behavior

**Purpose**: Explicitly destroy/remove individual containers

**When called**:

- Not always called - tests may skip this
- Called when explicit cleanup is needed for a specific container

**Requirements**:

- Remove the container
- Should be idempotent (safe to call multiple times)
- Should not fail if container is already removed

### 7. Driver.Close() Behavior

**Purpose**: Clean up ALL resources in the namespace

**When called**:

- At the end of test runs
- When a driver instance is being disposed

**Requirements**:

- Destroy all containers tagged with the namespace
- Delete all volumes tagged with the namespace
- Should be idempotent
- Should handle partial failures gracefully (log warnings, continue cleanup)

### 8. CreateVolume() Behavior

**Requirements**:

- Create or get existing volume with the given name
- Tag/label volume with namespace for cleanup
- Return volume handle
- Should be idempotent (return existing volume if name matches)
- `size` parameter may be 0 (meaning use default/unlimited)

### 9. Volume.Cleanup() Behavior

**Requirements**:

- Delete the specific volume
- Should be idempotent
- May be called or may rely on Driver.Close() instead

## Testing

Your driver will be tested against the standard test suite in
`orchestra/drivers_test.go`. The tests verify:

1. **happy path**: Basic container execution and log retrieval
2. **exit code failed**: Proper handling of non-zero exit codes
3. **volume**: Persistent volumes across multiple containers
4. **environment variables**: Environment variable passing
5. **with stdin**: Standard input handling

All tests run with the same interface, ensuring consistent behavior across
drivers.

## File Structure

Recommended structure:

```
orchestra/
  yourdriver/
    yourdriver.go      # Main driver implementation
    container.go       # Container implementation
    volume.go          # Volume implementation
    README.md          # Driver-specific documentation
```

## Example: Minimal Driver Structure

```go
package yourdriver

import (
    "context"
    "io"
    "log/slog"
    "github.com/jtarchie/ci/orchestra"
)

type YourDriver struct {
    namespace string
    logger    *slog.Logger
    // Add platform-specific client/config here
}

type Container struct {
    driver *YourDriver
    task   orchestra.Task
    // Add platform-specific container handle here
}

type ContainerStatus struct {
    isDone   bool
    exitCode int
}

type Volume struct {
    driver *YourDriver
    name   string
    // Add platform-specific volume handle here
}

// Driver methods
func (d *YourDriver) Name() string { return "yourdriver" }
func (d *YourDriver) Close() error { /* cleanup all namespace resources */ }
func (d *YourDriver) CreateVolume(ctx context.Context, name string, size int) (orchestra.Volume, error) { /* ... */ }
func (d *YourDriver) RunContainer(ctx context.Context, task orchestra.Task) (orchestra.Container, error) { /* ... */ }

// Container methods
func (c *Container) Status(ctx context.Context) (orchestra.ContainerStatus, error) { /* ... */ }
func (c *Container) Logs(ctx context.Context, stdout, stderr io.Writer) error { /* ... */ }
func (c *Container) Cleanup(ctx context.Context) error { /* ... */ }

// ContainerStatus methods
func (s *ContainerStatus) IsDone() bool { return s.isDone }
func (s *ContainerStatus) ExitCode() int { return s.exitCode }

// Volume methods
func (v *Volume) Name() string { return v.name }
func (v *Volume) Cleanup(ctx context.Context) error { /* ... */ }
```

## Common Patterns

### Error Handling

- Wrap errors with context: `fmt.Errorf("failed to create container: %w", err)`
- Use structured logging:
  `logger.Error("operation failed", "detail", value, "err", err)`
- Handle "not found" errors gracefully (return nil or cached state, don't fail)

### Resource Naming

- Use consistent naming: `{namespace}-{task.ID}`
- Sanitize names if platform has restrictions (length, character set)
- Document any naming limitations

### State Management

- Cache container status when done to avoid repeated API calls
- Handle race conditions (container may finish between checks)
- Be defensive about resource lifecycle (may be destroyed externally)

## Platform-Specific Considerations

When implementing for a specific platform, consider:

1. **API Rate Limits**: Implement caching/throttling in Status() if needed
2. **Image Formats**: How to handle Docker images vs platform-native formats
3. **Networking**: Whether containers need network access
4. **Lifecycle**: How the platform handles stopped vs destroyed containers
5. **Logs**: Whether logs persist after container stops
6. **Volumes**: Volume mounting limitations (number, size, permissions)
7. **Cleanup**: Whether cleanup requires specific ordering (containers before
   volumes)

## Reference Implementation

See `orchestra/docker/` for a complete, production-ready implementation that can
serve as a reference.
