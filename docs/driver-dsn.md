# Driver DSN (Data Source Name) Format

The CI system uses a DSN-style configuration format for specifying orchestration
drivers and their parameters.

## Format Options

### 1. Simple Driver Name

```bash
--driver=native
--driver=docker
--driver=k8s
```

Uses default configuration for the specified driver.

### 2. URL-Style Format

```bash
--driver=<driver>://<namespace>?<param1>=<value1>&<param2>=<value2>
```

**Examples**:

```bash
# K8s with custom namespace
--driver=k8s://production

# K8s with namespace and parameters
--driver=k8s://staging?region=us-east&timeout=300
```

**Components**:

- `driver`: The driver name (e.g., `k8s`, `docker`, `native`)
- `namespace`: Orchestra namespace for resource labeling/grouping
- `param=value`: Driver-specific configuration parameters

### 3. Colon-Separated Format

```bash
--driver=<driver>:<param1>=<value1>,<param2>=<value2>
```

**Examples**:

```bash
# K8s with namespace parameter
--driver=k8s:namespace=production

# Multiple parameters
--driver=k8s:namespace=staging,region=us-west,timeout=600
```

## How It Works

1. **DSN Parsing**: The system parses the driver string to extract:
   - Driver name
   - Orchestra namespace (from URL host or generated)
   - Configuration parameters

2. **Driver Initialization**: The driver receives configuration directly via
   parameters:
   - Parameters are passed as a map to the driver initialization function
   - Each driver reads its specific configuration from this parameter map
   - Driver defaults are used for any unspecified parameters

## Driver-Specific Parameters

### K8s Driver

| Parameter    | Description                        | Default                 | Example                          |
| ------------ | ---------------------------------- | ----------------------- | -------------------------------- |
| `namespace`  | Kubernetes namespace for resources | `default`               | `k8s:namespace=production`       |
| `kubeconfig` | Path to kubeconfig file            | `~/.kube/config` or env | `k8s:kubeconfig=/path/to/config` |

**Examples**:

```bash
--driver=k8s://my-namespace
--driver=k8s:namespace=staging
--driver=k8s:namespace=prod,kubeconfig=/etc/k8s/config
--driver=k8s://production?kubeconfig=/path/to/config
```

**Note**: If not specified, falls back to `KUBECONFIG` environment variable or
default kubeconfig location.

### Docker Driver

| Parameter | Description        | Default                | Example                            |
| --------- | ------------------ | ---------------------- | ---------------------------------- |
| `host`    | Docker daemon host | `DOCKER_HOST` or local | `docker:host=ssh://user@remote:22` |

**Examples**:

```bash
--driver=docker
--driver=docker:host=unix:///var/run/docker.sock
--driver=docker:host=ssh://user@host:22
```

**Note**: If not specified, falls back to `DOCKER_HOST` environment variable or
local Docker daemon.

### Native Driver

Currently no specific parameters. Uses host process execution.

```bash
--driver=native
```

## Examples

### Development

```bash
# Local development with native driver
go run main.go runner --driver=native examples/both/hello-world.ts

# Local Kubernetes (minikube) with default namespace
go run main.go runner --driver=k8s examples/both/hello-world.ts
```

### Staging

```bash
# K8s staging environment
go run main.go runner --driver=k8s://staging examples/both/hello-world.ts

# With additional parameters
go run main.go runner --driver=k8s://staging?region=us-east examples/both/hello-world.ts
```

### Production

```bash
# K8s production with explicit namespace
go run main.go runner --driver=k8s://production examples/both/hello-world.ts

# With additional parameters
go run main.go runner --driver=k8s://production?region=us-west examples/both/hello-world.ts
```

## Priority Order

Configuration values are resolved in this order (highest to lowest priority):

1. DSN parameters (e.g., `--driver=k8s:namespace=prod`)
2. Driver defaults

## Validation

The system validates:

- Driver name exists
- DSN syntax is correct
- Required parameters are provided (driver-specific)

Invalid configurations will fail early with clear error messages.

## Future Enhancements

Potential additions:

- Storage class selection for K8s PVCs
- Resource quotas and limits
- Authentication credentials
- TLS/SSL configuration
- Custom labels and annotations
