# Native Resources Architecture

## Overview

Native resources allow the CI system to perform resource operations (check, get,
put) without spawning separate container images. This reduces overhead
significantly, especially for common operations like git cloning.

## Current State

Currently, resources in Concourse-compatible pipelines work by:

1. Spawning a container with the resource type image (e.g.,
   `concourse/git-resource`)
2. Running `/opt/resource/check`, `/opt/resource/in`, or `/opt/resource/out`
3. Passing configuration via stdin as JSON
4. Reading results from stdout as JSON

This adds significant overhead for simple operations.

## Proposed Architecture

### Resource Interface

Native resources implement the standard Concourse resource protocol but in Go:

```go
// resources/resource.go
package resources

import "context"

// Version represents a resource version (arbitrary key-value pairs)
type Version map[string]string

// Metadata represents key-value metadata about a resource
type Metadata []MetadataField

type MetadataField struct {
    Name  string `json:"name"`
    Value string `json:"value"`
}

// CheckRequest is the input to a Check operation
type CheckRequest struct {
    Source  map[string]interface{} `json:"source"`
    Version Version                `json:"version,omitempty"`
}

// CheckResponse is the output of a Check operation
type CheckResponse []Version

// InRequest is the input to an In (get) operation
type InRequest struct {
    Source  map[string]interface{} `json:"source"`
    Version Version                `json:"version"`
    Params  map[string]interface{} `json:"params,omitempty"`
}

// InResponse is the output of an In operation
type InResponse struct {
    Version  Version  `json:"version"`
    Metadata Metadata `json:"metadata,omitempty"`
}

// OutRequest is the input to an Out (put) operation
type OutRequest struct {
    Source map[string]interface{} `json:"source"`
    Params map[string]interface{} `json:"params,omitempty"`
}

// OutResponse is the output of an Out operation
type OutResponse struct {
    Version  Version  `json:"version"`
    Metadata Metadata `json:"metadata,omitempty"`
}

// Resource is the interface that all native resources must implement
type Resource interface {
    // Name returns the resource type name (e.g., "git", "s3")
    Name() string

    // Check discovers new versions of the resource
    Check(ctx context.Context, req CheckRequest) (CheckResponse, error)

    // In fetches a specific version of the resource to the destination path
    In(ctx context.Context, destDir string, req InRequest) (InResponse, error)

    // Out pushes a new version of the resource from the source path
    Out(ctx context.Context, srcDir string, req OutRequest) (OutResponse, error)
}
```

### Registry Pattern

Similar to how orchestra drivers self-register, resources use `init()`:

```go
// resources/registry.go
package resources

import (
    "fmt"
    "sync"
)

type Factory func() Resource

var (
    registry   = make(map[string]Factory)
    registryMu sync.RWMutex
)

func Register(name string, factory Factory) {
    registryMu.Lock()
    defer registryMu.Unlock()
    registry[name] = factory
}

func Get(name string) (Resource, error) {
    registryMu.RLock()
    defer registryMu.RUnlock()
    factory, ok := registry[name]
    if !ok {
        return nil, fmt.Errorf("unknown resource type: %s", name)
    }
    return factory(), nil
}

func List() []string {
    registryMu.RLock()
    defer registryMu.RUnlock()
    names := make([]string, 0, len(registry))
    for name := range registry {
        names = append(names, name)
    }
    return names
}

func IsNative(name string) bool {
    registryMu.RLock()
    defer registryMu.RUnlock()
    _, ok := registry[name]
    return ok
}
```

### Git Resource Implementation

Using `go-git/go-git`:

```go
// resources/git/git.go
package git

import (
    "context"
    "fmt"
    "os"

    "github.com/go-git/go-git/v5"
    "github.com/go-git/go-git/v5/plumbing"
    "github.com/go-git/go-git/v5/plumbing/transport"
    "github.com/go-git/go-git/v5/plumbing/transport/http"
    "github.com/go-git/go-git/v5/plumbing/transport/ssh"
    "github.com/jtarchie/ci/resources"
)

type Git struct{}

func (g *Git) Name() string {
    return "git"
}

func (g *Git) Check(ctx context.Context, req resources.CheckRequest) (resources.CheckResponse, error) {
    // Implementation using go-git to fetch refs
}

func (g *Git) In(ctx context.Context, destDir string, req resources.InRequest) (resources.InResponse, error) {
    // Clone/checkout using go-git
}

func (g *Git) Out(ctx context.Context, srcDir string, req resources.OutRequest) (resources.OutResponse, error) {
    // Push using go-git
}

func init() {
    resources.Register("git", func() resources.Resource {
        return &Git{}
    })
}
```

## Deployment Strategies for Docker/K8s

The challenge: when running in Docker or K8s, the container doesn't have the
`ci` binary with native resources.

### Strategy 1: Mount the Binary (Recommended for Docker)

When running a native resource operation in a container environment:

1. Mount the `ci` binary into the container
2. Run `ci resource <type> <operation>` instead of `/opt/resource/<op>`
3. The binary handles the resource operation natively

```go
// When running in Docker/K8s with native resources:
container.Create(ctx, ContainerConfig{
    Mounts: []Mount{
        {
            Type:   "bind",
            Source: os.Executable(), // Path to ci binary
            Target: "/opt/ci/bin/ci",
            ReadOnly: true,
        },
    },
    Command: []string{"/opt/ci/bin/ci", "resource", "git", "in", "/workspace"},
})
```

### Strategy 2: Sidecar Container (K8s)

For Kubernetes, use a sidecar container with the `ci` binary:

```yaml
apiVersion: v1
kind: Pod
spec:
  containers:
    - name: main
      # Main task container
    - name: ci-resource
      image: ghcr.io/jtarchie/ci:latest
      command: ["sleep", "infinity"]
      volumeMounts:
        - name: workspace
          mountPath: /workspace
```

### Strategy 3: Init Container + Shared Volume

Copy the binary to a shared volume:

```yaml
initContainers:
  - name: ci-installer
    image: ghcr.io/jtarchie/ci:latest
    command: ["cp", "/usr/local/bin/ci", "/ci-bin/"]
    volumeMounts:
      - name: ci-bin
        mountPath: /ci-bin
```

### Strategy 4: Fallback to Container Resources

If native execution isn't possible, fall back to container-based resources:

```go
func ExecuteResource(ctx context.Context, rt ResourceType, op Operation, req Request) (Response, error) {
    // Check if native resource is available
    if resources.IsNative(rt.Name) && canExecuteNatively(ctx) {
        return executeNative(ctx, rt.Name, op, req)
    }
    
    // Fallback to container-based resource
    return executeContainer(ctx, rt, op, req)
}
```

## CLI Integration

Add a new `resource` subcommand for executing resource operations:

```bash
# For check
echo '{"source": {"uri": "..."}}' | ci resource git check

# For in (get)
echo '{"source": {...}, "version": {...}}' | ci resource git in /path/to/dest

# For out (put)
echo '{"source": {...}, "params": {...}}' | ci resource git out /path/to/src
```

## Runtime Integration

Modify the job runner to check for native resources:

```typescript
// In backwards/src/job_runner.ts
private async processGetStep(step: Get, pathContext: string): Promise<void> {
    const resource = this.findResource(step.get);
    const resourceType = this.findResourceType(resource?.type);

    // Check if this is a native resource
    if (runtime.isNativeResource(resourceType?.name)) {
        // Use native resource execution
        await runtime.executeNativeResource({
            type: resourceType?.name,
            operation: "check",
            source: resource?.source,
        });
        // ... rest of native flow
    } else {
        // Fallback to container-based resource (existing code)
    }
}
```

## File Structure

```
resources/
├── resource.go          # Interface definitions
├── registry.go          # Registration pattern
├── git/
│   ├── git.go           # Git resource implementation
│   └── git_test.go
├── s3/
│   ├── s3.go            # S3 resource implementation
│   └── s3_test.go
├── time/
│   ├── time.go          # Time resource implementation
│   └── time_test.go
└── mock/
    ├── mock.go          # Mock resource for testing
    └── mock_test.go
```

## Advantages

1. **Performance**: No container startup overhead for simple operations
2. **Simplicity**: Single binary contains all common resources
3. **Portability**: Works with native driver without Docker
4. **Fallback**: Can still use container resources when needed
5. **Extensibility**: Easy to add new native resources

## Migration Path

1. Start with `git` resource as proof of concept
2. Add `time` resource (simple, good for testing)
3. Add `mock` resource for testing
4. Gradually add more resources based on usage

## Configuration

Allow users to prefer or require native resources:

```yaml
# In pipeline config
resources:
  - name: my-repo
    type: git
    source:
      uri: https://github.com/...
    native: true  Prefer native implementation
```

Or globally via CLI:

```bash
ci runner --prefer-native-resources pipeline.ts
```

## Future Direction: Native-First Execution (No Containers for Resources)

The cleanest architecture is to **always execute native resources directly in
the `ci` process** - no containers at all for resource operations. This
eliminates all sidecar/binary-mounting complexity.

### Why Resources Don't Need Containers

1. **No isolation needed** - Resources read/write to a specific directory that
   becomes a volume mount for tasks
2. **The `ci` process already has filesystem access** - It creates volumes and
   manages the workspace
3. **Network access is fine** - Resources need to reach git repos, S3, etc.
   anyway
4. **No security boundary needed** - Unlike tasks, resources are trusted code
   (built into the binary)

### Execution Model

```
Pipeline Request
       │
       ▼
┌─────────────────┐
│  ci process     │
│  ┌───────────┐  │
│  │ Native    │  │  ← git clone happens here (in-process)
│  │ Resource  │  │
│  └───────────┘  │
│       │         │
│       ▼         │
│  ┌───────────┐  │
│  │ Volume/   │  │  ← files written to workspace
│  │ Workspace │  │
│  └───────────┘  │
└─────────────────┘
       │
       ▼
┌─────────────────┐
│  Container      │  ← Only tasks run in containers
│  (Task)         │
│  ┌───────────┐  │
│  │ Volume    │──┼── mounted from above
│  │ Mount     │  │
│  └───────────┘  │
└─────────────────┘
```

### Flow for a Get Step

1. `ci` receives get step request
2. `ci` creates a volume (directory for native, Docker volume for Docker)
3. `ci` executes native git resource **in-process**:
   - Clones repo directly to volume path
   - No container spawned
4. Volume is mounted into subsequent task containers

### Volume Path Resolution by Driver

| Driver   | Volume Path                        | How Resources Access It                                 |
| -------- | ---------------------------------- | ------------------------------------------------------- |
| `native` | `/tmp/ci-volumes/abc123`           | Direct filesystem access                                |
| `docker` | Host path mounted as Docker volume | Direct filesystem access (same host)                    |
| `k8s`    | PVC mount path on controller node  | Direct access if controller has PVC mounted (see below) |

### Kubernetes Consideration

For K8s, if the `ci` controller runs **outside** the cluster, you'd need the
binary-mounting approach from the strategies above. But if `ci` runs **inside**
the cluster (as a pod), it can mount the same PVCs and write directly:

```yaml
# ci controller running in-cluster
apiVersion: v1
kind: Pod
metadata:
  name: ci-controller
spec:
  containers:
    - name: ci
      image: ghcr.io/jtarchie/ci:latest
      volumeMounts:
        - name: workspace-pvc
          mountPath: /workspace
  volumes:
    - name: workspace-pvc
      persistentVolumeClaim:
        claimName: ci-workspace
```

### Benefits of Native-First

- **Zero container overhead** for resource operations
- **Faster pipeline execution** - git clone starts immediately
- **Simpler architecture** - no binary mounting or sidecars needed
- **Works identically** across native/docker drivers
- **Reduced complexity** - fewer moving parts to debug

### Implementation Priority

This is the recommended long-term direction. The current implementation supports
both approaches, allowing gradual migration:

1. Native resources execute in-process when possible
2. Fall back to container-based resources for custom/unknown types
3. Eventually, most common resources (git, s3, time, registry-image) will be
   native, making container-based resources rare
