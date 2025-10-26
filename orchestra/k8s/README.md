# Kubernetes (K8s) Orchestra Driver

This driver implements the Orchestra container orchestration interface for
Kubernetes clusters.

## Overview

The K8s driver allows Orchestra to run tasks as Kubernetes Pods, providing
container orchestration through the Kubernetes API. It's designed to work with
any Kubernetes cluster, including local development clusters like minikube.

## Configuration

The driver uses standard Kubernetes client configuration:

1. **In-cluster configuration**: When running inside a Kubernetes cluster, it
   automatically uses the service account credentials
2. **Kubeconfig file**: When running outside a cluster, it uses the default
   kubeconfig file (`~/.kube/config`)

### Environment Variables

The following standard Kubernetes environment variables are respected:

- `KUBECONFIG` - Path to kubeconfig file
- `KUBERNETES_SERVICE_HOST` - Kubernetes API server host (when running
  in-cluster)
- `KUBERNETES_SERVICE_PORT` - Kubernetes API server port (when running
  in-cluster)

## Features

### Supported

- ✅ Container execution (Pods)
- ✅ Volume mounts (PersistentVolumeClaims)
- ✅ Environment variables
- ✅ Resource limits (CPU and memory)
- ✅ Exit code detection
- ✅ Log retrieval
- ✅ Container cleanup
- ✅ Privileged containers
- ✅ Custom user (via security context)
- ✅ Idempotent operations (running same task ID returns existing pod)

### Known Limitations

- ❌ **Stdin input**: Kubernetes stdin support requires complex Pod attach/exec
  API usage with SPDY/WebSocket protocols. This is not currently implemented.
- ⚠️ **Stderr separation**: Kubernetes logs don't separate stdout and stderr by
  default. All logs are written to the stdout writer.
- ⚠️ **Namespace**: All resources are created in the `default` Kubernetes
  namespace. Multi-namespace support is not implemented.
- ⚠️ **Storage classes**: PVCs use the default storage class. Custom storage
  class selection is not supported.

## Resource Naming

The driver sanitizes resource names to comply with Kubernetes naming
requirements:

- **Pod names**: Converted to lowercase, invalid characters replaced with
  hyphens, max 253 characters (DNS-1123 subdomain format)
- **PVC names**: Same as pod names
- **Labels**: Alphanumeric characters, hyphens, underscores, and dots allowed;
  must start/end with alphanumeric; max 63 characters

## Volume Support

Volumes are implemented as PersistentVolumeClaims (PVCs):

- Default access mode: `ReadWriteOnce`
- Default size: 1Gi (if not specified)
- Storage class: Uses cluster default
- Volumes are mounted at `/tmp/{pod-name}/{mount-path}`

## Resource Limits

CPU and memory limits are translated from Docker format to Kubernetes format:

- **CPU**: Docker CPU shares are converted to Kubernetes millicores (1024 shares
  ≈ 1000 millicores)
- **Memory**: Direct byte-to-byte mapping
- Kubernetes requests are set to 50% of limits

## Testing with Minikube

The driver is tested with minikube. To run tests locally:

```bash
# Start minikube
minikube start

# Run k8s driver tests
go test -v -race -count=1 -run 'TestDrivers/k8s' ./orchestra

# Cleanup minikube
minikube delete
```

## Implementation Notes

### Cleanup Strategy

The `Close()` method deletes all Pods and PVCs with the `orchestra.namespace`
label matching the driver's namespace. This ensures proper cleanup even if
individual `Cleanup()` calls were skipped.

### Idempotency

Running the same task (by task ID) multiple times returns the existing Pod/PVC
rather than creating duplicates. This matches the behavior of other Orchestra
drivers.

### Pod Lifecycle

- Pods use `RestartPolicy: Never`
- Pods are created and started immediately
- Status polling detects when pods complete
- Pods remain after completion for log retrieval
- Explicit cleanup or driver `Close()` removes pods

## Future Enhancements

Potential improvements for future versions:

1. **Stdin support**: Implement using remotecommand.Executor and SPDY protocol
2. **Multi-namespace**: Support custom Kubernetes namespaces
3. **Storage classes**: Allow configurable storage class for PVCs
4. **Volume modes**: Support ReadWriteMany and other access modes
5. **Node selection**: Support node selectors and affinity rules
6. **Image pull secrets**: Support private container registries
7. **Health checks**: Add liveness/readiness probes
8. **Job resources**: Option to use Kubernetes Jobs instead of bare Pods

## Dependencies

- `k8s.io/client-go` - Kubernetes Go client library
- `k8s.io/api` - Kubernetes API types
- `k8s.io/apimachinery` - Kubernetes API machinery

## See Also

- [Kubernetes API Documentation](https://kubernetes.io/docs/reference/kubernetes-api/)
- [client-go Documentation](https://github.com/kubernetes/client-go)
- [Orchestra Driver Implementation Guide](../docs/implementing-new-driver.md)
