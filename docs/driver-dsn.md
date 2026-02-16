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
| `tags`      | Comma-separated custom tags       | (none)         | `digitalocean:tags=prod,myapp`    |

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
| `DIGITALOCEAN_TAGS`      | Default custom tags            |

**Resource Tagging**: All droplets are automatically tagged with `ci` and
`namespace-<namespace>`. Custom tags can be added via the `tags` parameter for
more granular resource management and targeted cleanup.

**Note**: The driver generates an ephemeral SSH key pair for each session to
connect to the droplet. Both the key and droplet are cleaned up when the driver
is closed.

### Hetzner Driver

The Hetzner driver creates an on-demand cloud server running Docker and
delegates container operations to it. When the driver is closed, the server is
automatically deleted.

| Parameter        | Description                       | Default     | Example                         |
| ---------------- | --------------------------------- | ----------- | ------------------------------- |
| `token`          | Hetzner Cloud API token           | (required)  | `hetzner:token=xxx`             |
| `image`          | Server image name                 | `docker-ce` | `hetzner:image=ubuntu-22.04`    |
| `server_type`    | Server type slug or "auto"        | `cx23`      | `hetzner:server_type=cx33`      |
| `location`       | Server location                   | `nbg1`      | `hetzner:location=fsn1`         |
| `disk_size`      | Disk size for Docker volumes (GB) | `10`        | `hetzner:disk_size=50`          |
| `ssh_timeout`    | Timeout for SSH availability      | `5m`        | `hetzner:ssh_timeout=10m`       |
| `docker_timeout` | Timeout for Docker availability   | `5m`        | `hetzner:docker_timeout=10m`    |
| `labels`         | Comma-separated key=value labels  | (none)      | `hetzner:labels=env=prod,app=x` |

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
| `HETZNER_LABELS`         | Default custom labels          |

**Available Locations**:

| Location | City              |
| -------- | ----------------- |
| `fsn1`   | Falkenstein, DE   |
| `nbg1`   | Nuremberg, DE     |
| `hel1`   | Helsinki, FI      |
| `ash`    | Ashburn, VA, US   |
| `hil`    | Hillsboro, OR, US |

**Resource Labeling**: All servers are automatically labeled with `ci=true` and
`namespace=<namespace>`. Custom labels can be added via the `labels` parameter
for more granular resource management and targeted cleanup.

**Note**: The driver generates an ephemeral SSH key pair for each session to
connect to the server. Both the key and server are cleaned up when the driver is
closed.

### QEMU Driver

The QEMU driver runs tasks inside a local QEMU virtual machine. Commands are
executed inside the guest via the QEMU Guest Agent (QGA), and volumes are shared
between host and guest via 9p virtfs. The VM is lazily booted on first use and
automatically destroyed when the driver is closed.

| Parameter     | Description                       | Default                             | Example                                        |
| ------------- | --------------------------------- | ----------------------------------- | ---------------------------------------------- |
| `memory`      | VM memory in MB                   | `2048`                              | `qemu:memory=4096`                             |
| `cpus`        | Number of vCPUs                   | `2`                                 | `qemu:cpus=4`                                  |
| `accel`       | Acceleration backend              | `hvf` (macOS), `kvm` (Linux), `tcg` | `qemu:accel=tcg`                               |
| `qemu_binary` | Path to QEMU binary               | `qemu-system-x86_64` or `aarch64`   | `qemu:qemu_binary=/usr/bin/qemu-system-x86_64` |
| `cache_dir`   | Directory for cached cloud images | `~/.cache/ci/qemu`                  | `qemu:cache_dir=/tmp/qemu-cache`               |
| `image`       | Path to a custom qcow2 base image | Auto-downloads Ubuntu cloud image   | `qemu:image=/path/to/image.qcow2`              |

**Acceleration**:

- **macOS**: Defaults to `hvf` (Hypervisor.framework) for near-native
  performance
- **Linux**: Defaults to `kvm` if `/dev/kvm` is available, otherwise `tcg`
  (software emulation)
- **Other**: Defaults to `tcg`

**Architecture**: The driver auto-detects the host architecture and selects the
appropriate QEMU binary (`qemu-system-x86_64` or `qemu-system-aarch64`) and
machine type.

**Examples**:

```bash
# Basic usage with defaults
--driver=qemu

# Custom memory and CPU
--driver=qemu:memory=4096,cpus=4

# URL-style with namespace
--driver=qemu://my-namespace?memory=4096&cpus=4

# Custom base image
--driver=qemu:image=/path/to/custom.qcow2

# Software emulation (no hardware acceleration)
--driver=qemu:accel=tcg
```

**Environment Variables**:

| Variable         | Description                   |
| ---------------- | ----------------------------- |
| `QEMU_MEMORY`    | Default VM memory in MB       |
| `QEMU_CPUS`      | Default number of vCPUs       |
| `QEMU_ACCEL`     | Default acceleration backend  |
| `QEMU_BINARY`    | Default QEMU binary path      |
| `QEMU_CACHE_DIR` | Default image cache directory |
| `QEMU_IMAGE`     | Default base image path       |

**How it works**:

1. Downloads an Ubuntu cloud image (cached locally) or uses a provided image
2. Creates a copy-on-write overlay so the base image is never modified
3. Generates a cloud-init seed ISO to configure the guest (SSH keys, QGA
   install)
4. Boots the VM with QMP monitor, QGA channel (TCP), and 9p volume sharing
5. Waits for cloud-init to complete and QGA to become responsive
6. Executes task commands via QGA `guest-exec` / `guest-exec-status`
7. Volumes are shared via 9p virtfs, mounted at `/mnt/volumes/<name>` in the
   guest

**Prerequisites**:

- QEMU installed (`brew install qemu` on macOS, `apt install qemu-system` on
  Linux)
- `qemu-img` and `genisoimage`/`mkisofs` available on PATH
- For hardware acceleration: KVM support on Linux, or Hypervisor.framework on
  macOS

**Note**: The VM and all temporary files (overlay disk, seed ISO, volumes) are
cleaned up when the driver is closed.

### Apple Virtualization (VZ) Driver

The VZ driver runs tasks inside a local virtual machine using Apple's
Virtualization.framework (macOS only). Commands are executed inside the guest
via a custom vsock-based agent, and volumes are shared between host and guest
via virtiofs. The VM is lazily booted on first use and automatically destroyed
when the driver is closed.

| Parameter   | Description                       | Default               | Example                       |
| ----------- | --------------------------------- | --------------------- | ----------------------------- |
| `memory`    | VM memory in MB                   | `2048`                | `vz:memory=4096`              |
| `cpus`      | Number of vCPUs                   | `2`                   | `vz:cpus=4`                   |
| `cache_dir` | Directory for cached cloud images | `~/.cache/ci/vz`      | `vz:cache_dir=/tmp/vz-cache`  |
| `image`     | Path to a custom raw base image   | Auto-downloads Ubuntu | `vz:image=/path/to/image.raw` |

**Examples**:

```bash
# Basic usage with defaults
--driver=vz

# Custom memory and CPU
--driver=vz:memory=4096,cpus=4

# URL-style with namespace
--driver=vz://my-namespace?memory=4096&cpus=4

# Custom base image
--driver=vz:image=/path/to/custom.raw
```

**Environment Variables**:

| Variable       | Description                   |
| -------------- | ----------------------------- |
| `VZ_MEMORY`    | Default VM memory in MB       |
| `VZ_CPUS`      | Default number of vCPUs       |
| `VZ_CACHE_DIR` | Default image cache directory |
| `VZ_IMAGE`     | Default base image path       |

**How it works**:

1. Downloads an Ubuntu cloud image (cached locally) or uses a provided image
2. Converts the image to raw format (Apple VZ requires raw disk images)
3. Creates a writable copy so the base image is never modified
4. Generates a cloud-init seed ISO to configure the guest (vsock agent,
   virtiofs)
5. Boots the VM with EFI boot loader, virtiofs sharing, and vsock communication
6. Waits for cloud-init to complete and the vsock agent to become responsive
7. Executes task commands via the vsock agent protocol
8. Volumes are shared via virtiofs, mounted at `/mnt/volumes/<name>` in the
   guest

**Prerequisites**:

- macOS 13 (Ventura) or later
- Binary must be codesigned with `com.apple.security.virtualization` entitlement
- `qemu-img` available on PATH (for qcow2 → raw image conversion)
- Go toolchain available for cross-compiling the guest agent

**Entitlements Setup**:

Apple's Virtualization.framework requires executables to be codesigned with the
`com.apple.security.virtualization` entitlement before VMs can be created.
Without this, the driver will fail with error:
`"The process doesn't have the
'com.apple.security.virtualization' entitlement."`

To enable VZ driver support:

1. Create an `entitlements.plist` file:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>com.apple.security.virtualization</key>
    <true/>
</dict>
</plist>
```

2. Codesign your binary:

```bash
# After building
go build -o ci .
codesign -s - -f --entitlements entitlements.plist ./ci

# For test binaries
go test -c ./orchestra/vz
codesign -s - -f --entitlements entitlements.plist ./vz.test
./vz.test -test.v
```

**Note**: The `-s -` flag uses ad-hoc signing (no certificate required). For
distribution, use a valid Developer ID certificate:
`-s "Developer ID
Application: Your Name (TEAM_ID)"`.

**Architecture**: The driver always uses hardware-accelerated virtualization via
Apple's Hypervisor.framework. On Apple Silicon Macs, the guest runs arm64 Linux.

**Note**: The `task.Image` field (e.g., "busybox") is ignored — commands run
directly in the guest OS, similar to the QEMU and native drivers.

**Note**: The VM and all temporary files (disk copy, seed ISO, volumes) are
cleaned up when the driver is closed.

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
