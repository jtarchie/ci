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

### DigitalOcean Driver

The DigitalOcean driver creates an on-demand droplet running Docker and
delegates container operations to it. When the driver is closed, the droplet is
automatically deleted.

| Parameter   | Description                       | Default        | Example                           |
| ----------- | --------------------------------- | -------------- | --------------------------------- |
| `token`     | DigitalOcean API token            | (required)     | `digitalocean:token=dop_v1_xxx`   |
| `image`     | Droplet image slug                | `docker-20-04` | `digitalocean:image=docker-24-04` |
| `size`      | Droplet size slug or "auto"       | `s-1vcpu-1gb`  | `digitalocean:size=s-2vcpu-4gb`   |
| `region`    | Droplet region                    | `nyc3`         | `digitalocean:region=sfo3`        |
| `disk_size` | Disk size for Docker volumes (GB) | `25`           | `digitalocean:disk_size=50`       |

**Auto-sizing**: When `size=auto`, the driver automatically selects an
appropriate droplet size based on the pipeline's `container_limits` (CPU and
memory):

- Memory > 8GB or CPU > 4 cores → `s-8vcpu-16gb`
- Memory > 4GB or CPU > 2 cores → `s-4vcpu-8gb`
- Memory > 2GB or CPU > 1 core → `s-2vcpu-4gb`
- Memory > 1GB → `s-2vcpu-2gb`
- Memory > 512MB → `s-1vcpu-2gb`
- Default → `s-1vcpu-1gb`

**Examples**:

```bash
# Basic usage with token
--driver=digitalocean:token=dop_v1_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Using environment variable for token
DIGITALOCEAN_TOKEN=dop_v1_xxx --driver=digitalocean

# Auto-size based on container limits
--driver=digitalocean:size=auto

# Full configuration
--driver=digitalocean://ci-namespace?token=dop_v1_xxx&image=docker-20-04&size=s-2vcpu-4gb&region=sfo3&disk_size=50

# Colon-separated format
--driver=digitalocean:token=dop_v1_xxx,size=auto,region=nyc1
```

**Environment Variables**:

| Variable                 | Description                    |
| ------------------------ | ------------------------------ |
| `DIGITALOCEAN_TOKEN`     | API token (alternative to DSN) |
| `DIGITALOCEAN_IMAGE`     | Default image slug             |
| `DIGITALOCEAN_SIZE`      | Default size slug              |
| `DIGITALOCEAN_REGION`    | Default region                 |
| `DIGITALOCEAN_DISK_SIZE` | Default disk size (GB)         |

**Note**: The driver generates an ephemeral SSH key pair for each session to
connect to the droplet. Both the key and droplet are cleaned up when the driver
is closed.

### Hetzner Driver

The Hetzner driver creates an on-demand cloud server running Docker and
delegates container operations to it. When the driver is closed, the server is
automatically deleted.

| Parameter        | Description                       | Default     | Example                      |
| ---------------- | --------------------------------- | ----------- | ---------------------------- |
| `token`          | Hetzner Cloud API token           | (required)  | `hetzner:token=xxx`          |
| `image`          | Server image name                 | `docker-ce` | `hetzner:image=ubuntu-22.04` |
| `server_type`    | Server type slug or "auto"        | `cx23`      | `hetzner:server_type=cx33`   |
| `location`       | Server location                   | `nbg1`      | `hetzner:location=fsn1`      |
| `disk_size`      | Disk size for Docker volumes (GB) | `10`        | `hetzner:disk_size=50`       |
| `ssh_timeout`    | Timeout for SSH availability      | `5m`        | `hetzner:ssh_timeout=10m`    |
| `docker_timeout` | Timeout for Docker availability   | `5m`        | `hetzner:docker_timeout=10m` |

**Auto-sizing**: When `server_type=auto`, the driver automatically selects an
appropriate server type based on the pipeline's `container_limits` (CPU and
memory):

- Memory > 16GB or CPU > 8 cores → `cx53` (16 vCPU, 32GB)
- Memory > 8GB or CPU > 4 cores → `cx43` (8 vCPU, 16GB)
- Memory > 4GB or CPU > 2 cores → `cx33` (4 vCPU, 8GB)
- Default → `cx23` (2 vCPU, 4GB)

**Examples**:

```bash
# Basic usage with token
--driver=hetzner:token=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Using environment variable for token
HETZNER_TOKEN=xxx --driver=hetzner

# Auto-size based on container limits
--driver=hetzner:server_type=auto

# Full configuration
--driver=hetzner://ci-namespace?token=xxx&image=docker-ce&server_type=cx33&location=nbg1&disk_size=50

# Colon-separated format
--driver=hetzner:token=xxx,server_type=auto,location=fsn1
```

**Environment Variables**:

| Variable                 | Description                    |
| ------------------------ | ------------------------------ |
| `HETZNER_TOKEN`          | API token (alternative to DSN) |
| `HETZNER_IMAGE`          | Default image name             |
| `HETZNER_SERVER_TYPE`    | Default server type slug       |
| `HETZNER_LOCATION`       | Default location               |
| `HETZNER_DISK_SIZE`      | Default disk size (GB)         |
| `HETZNER_SSH_TIMEOUT`    | Default SSH timeout            |
| `HETZNER_DOCKER_TIMEOUT` | Default Docker timeout         |

**Available Locations**:

| Location | City              |
| -------- | ----------------- |
| `fsn1`   | Falkenstein, DE   |
| `nbg1`   | Nuremberg, DE     |
| `hel1`   | Helsinki, FI      |
| `ash`    | Ashburn, VA, US   |
| `hil`    | Hillsboro, OR, US |

**Note**: The driver generates an ephemeral SSH key pair for each session to
connect to the server. Both the key and server are cleaned up when the driver is
closed.

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
