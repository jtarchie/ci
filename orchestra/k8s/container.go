package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jtarchie/ci/orchestra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// sanitizeName converts a string to a valid Kubernetes resource name (DNS-1123 subdomain)
// Must consist of lowercase alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character
func sanitizeName(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace underscores and other invalid characters with hyphens
	reg := regexp.MustCompile(`[^a-z0-9.-]+`)
	name = reg.ReplaceAllString(name, "-")

	// Ensure it starts with an alphanumeric character
	name = strings.TrimLeft(name, "-.")

	// Ensure it ends with an alphanumeric character
	name = strings.TrimRight(name, "-.")

	// Kubernetes resource names have a max length of 253 characters
	if len(name) > 253 {
		name = name[:253]
		// Re-trim end in case we cut in the middle of invalid characters
		name = strings.TrimRight(name, "-.")
	}

	return name
}

// sanitizeLabel converts a string to a valid Kubernetes label value
// Must be an empty string or consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character
func sanitizeLabel(label string) string {
	if label == "" {
		return label
	}

	// Replace invalid characters with hyphens
	reg := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	label = reg.ReplaceAllString(label, "-")

	// Ensure it starts with an alphanumeric character
	label = strings.TrimLeft(label, "-._")

	// Ensure it ends with an alphanumeric character
	label = strings.TrimRight(label, "-._")

	// Kubernetes labels have a max length of 63 characters
	if len(label) > 63 {
		label = label[:63]
		// Re-trim end in case we cut in the middle of invalid characters
		label = strings.TrimRight(label, "-._")
	}

	return label
}

type Container struct {
	clientset *kubernetes.Clientset
	config    *rest.Config
	podName   string
	task      orchestra.Task
	logger    *slog.Logger
}

type ContainerStatus struct {
	phase      corev1.PodPhase
	exitCode   int32
	terminated bool
}

func (c *Container) Status(ctx context.Context) (orchestra.ContainerStatus, error) {
	pod, err := c.clientset.CoreV1().Pods("default").Get(ctx, c.podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	status := &ContainerStatus{
		phase: pod.Status.Phase,
	}

	// Check container status
	if len(pod.Status.ContainerStatuses) > 0 {
		containerStatus := pod.Status.ContainerStatuses[0]
		if containerStatus.State.Terminated != nil {
			status.terminated = true
			status.exitCode = containerStatus.State.Terminated.ExitCode
		}
	}

	return status, nil
}

func (c *Container) Logs(ctx context.Context, stdout, stderr io.Writer) error {
	req := c.clientset.CoreV1().Pods("default").GetLogs(c.podName, &corev1.PodLogOptions{})

	podLogs, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to get pod logs: %w", err)
	}
	defer func() {
		if closeErr := podLogs.Close(); closeErr != nil {
			c.logger.Warn("failed to close pod logs stream", "err", closeErr)
		}
	}()

	// K8s doesn't separate stdout/stderr in logs by default, so we write everything to stdout
	_, err = io.Copy(stdout, podLogs)
	if err != nil {
		return fmt.Errorf("failed to copy logs: %w", err)
	}

	return nil
}

func (c *Container) Cleanup(ctx context.Context) error {
	deletePolicy := metav1.DeletePropagationForeground
	err := c.clientset.CoreV1().Pods("default").Delete(ctx, c.podName, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete pod: %w", err)
	}

	return nil
}

func (s *ContainerStatus) IsDone() bool {
	return s.terminated || s.phase == corev1.PodSucceeded || s.phase == corev1.PodFailed
}

func (s *ContainerStatus) ExitCode() int {
	return int(s.exitCode)
}

func (k *K8s) RunContainer(ctx context.Context, task orchestra.Task) (orchestra.Container, error) {
	logger := k.logger.With("taskID", task.ID)

	// Sanitize pod name to comply with k8s naming (lowercase alphanumeric + hyphens/dots)
	podName := sanitizeName(fmt.Sprintf("%s-%s", k.namespace, task.ID))

	// Check if pod already exists
	existingPod, err := k.clientset.CoreV1().Pods("default").Get(ctx, podName, metav1.GetOptions{})
	if err == nil {
		logger.Debug("pod.exists", "name", podName)
		return &Container{
			clientset: k.clientset,
			config:    k.config,
			podName:   existingPod.Name,
			task:      task,
			logger:    logger,
		}, nil
	} // Create volumes for the pod
	volumes := []corev1.Volume{}
	volumeMounts := []corev1.VolumeMount{}

	for _, taskMount := range task.Mounts {
		volume, err := k.CreateVolume(ctx, taskMount.Name, 0)
		if err != nil {
			logger.Error("volume.create", "name", taskMount.Name, "err", err)
			return nil, fmt.Errorf("failed to create volume: %w", err)
		}

		k8sVolume, _ := volume.(*Volume)

		// Sanitize volume name to comply with k8s naming requirements
		sanitizedVolumeName := sanitizeName(taskMount.Name)

		volumes = append(volumes, corev1.Volume{
			Name: sanitizedVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: k8sVolume.pvcName,
				},
			},
		})

		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      sanitizedVolumeName,
			MountPath: filepath.Join("/tmp", podName, taskMount.Path),
		})
	}

	// Convert environment variables
	env := []corev1.EnvVar{}
	for k, v := range task.Env {
		env = append(env, corev1.EnvVar{
			Name:  k,
			Value: v,
		})
	}

	// Set up resource requirements
	resources := corev1.ResourceRequirements{}
	if task.ContainerLimits.CPU > 0 || task.ContainerLimits.Memory > 0 {
		resources.Limits = corev1.ResourceList{}
		resources.Requests = corev1.ResourceList{}

		if task.ContainerLimits.CPU > 0 {
			// Convert CPU shares to millicores (rough approximation)
			// Docker CPU shares default is 1024, k8s uses millicores
			millicores := (task.ContainerLimits.CPU * 1000) / 1024
			resources.Limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(millicores, resource.DecimalSI)
			resources.Requests[corev1.ResourceCPU] = *resource.NewMilliQuantity(millicores/2, resource.DecimalSI)
		}

		if task.ContainerLimits.Memory > 0 {
			resources.Limits[corev1.ResourceMemory] = *resource.NewQuantity(task.ContainerLimits.Memory, resource.BinarySI)
			resources.Requests[corev1.ResourceMemory] = *resource.NewQuantity(task.ContainerLimits.Memory/2, resource.BinarySI)
		}
	}

	// Build the pod spec
	enabledStdin := task.Stdin != nil

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
			Labels: map[string]string{
				"orchestra.namespace": sanitizeLabel(k.namespace),
				"orchestra.task":      sanitizeLabel(task.ID),
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:         "task",
					Image:        task.Image,
					Command:      task.Command,
					Env:          env,
					VolumeMounts: volumeMounts,
					WorkingDir:   filepath.Join("/tmp", podName),
					Resources:    resources,
					Stdin:        enabledStdin,
					StdinOnce:    enabledStdin,
				},
			},
			Volumes: volumes,
		},
	}

	// Set security context if user is specified
	if task.User != "" {
		// Parse user as UID (k8s requires numeric UID)
		var uid int64

		// Handle common username to UID mappings
		switch task.User {
		case "root":
			uid = 0
		default:
			_, err := fmt.Sscanf(task.User, "%d", &uid)
			if err != nil {
				logger.Warn("user.parse", "user", task.User, "err", err, "msg", "using default user")
				// Skip setting user if we can't parse it
				goto skipUser
			}
		}

		pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			RunAsUser: &uid,
		}
	}
skipUser:

	// Set privileged mode if needed
	if task.Privileged {
		if pod.Spec.Containers[0].SecurityContext == nil {
			pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{}
		}
		privileged := true
		pod.Spec.Containers[0].SecurityContext.Privileged = &privileged
	}

	// Create the pod
	createdPod, err := k.clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		logger.Error("pod.create", "name", podName, "err", err)
		return nil, fmt.Errorf("failed to create pod: %w", err)
	}

	// Handle stdin if provided
	if enabledStdin && task.Stdin != nil {
		logger.Debug("pod.stdin", "name", podName)

		// Wait for pod to be in Running state with a timeout
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		watcher, err := k.clientset.CoreV1().Pods("default").Watch(waitCtx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", podName),
		})
		if err != nil {
			logger.Error("pod.watch", "name", podName, "err", err)
			return nil, fmt.Errorf("failed to watch pod: %w", err)
		}
		defer watcher.Stop()

		// Wait for the pod to reach Running state and have containers ready
		podRunning := false
		for event := range watcher.ResultChan() {
			if event.Type == watch.Modified || event.Type == watch.Added {
				p, ok := event.Object.(*corev1.Pod)
				if !ok {
					continue
				}

				logger.Debug("pod.status", "name", podName, "phase", p.Status.Phase, "containers", len(p.Status.ContainerStatuses))

				// Check if pod is running and at least one container is ready
				if p.Status.Phase == corev1.PodRunning {
					// Check if any container is running (not just created)
					for _, cs := range p.Status.ContainerStatuses {
						if cs.State.Running != nil {
							podRunning = true
							break
						}
					}
					if podRunning {
						break
					}
				}

				// If the pod completed very quickly (before we could see it running),
				// that's okay - we can still attach (though stdin may not work)
				if p.Status.Phase == corev1.PodSucceeded {
					logger.Debug("pod.completed.quickly", "name", podName)
					podRunning = true
					break
				}

				// Also check for failed states
				if p.Status.Phase == corev1.PodFailed {
					return nil, fmt.Errorf("pod failed to start: %s", p.Status.Message)
				}
			}

			// Check if context was cancelled
			select {
			case <-waitCtx.Done():
				return nil, fmt.Errorf("timeout waiting for pod to reach running state")
			default:
			}
		}

		if !podRunning {
			return nil, fmt.Errorf("pod did not reach running state")
		}

		logger.Debug("pod.running", "name", podName)

		// Now attach stdin to the running pod
		req := k.clientset.CoreV1().RESTClient().Post().
			Resource("pods").
			Name(podName).
			Namespace("default").
			SubResource("attach").
			VersionedParams(&corev1.PodAttachOptions{
				Stdin:     true,
				Container: "task",
			}, scheme.ParameterCodec)

		exec, err := remotecommand.NewSPDYExecutor(k.config, "POST", req.URL())
		if err != nil {
			logger.Error("pod.attach.executor", "name", podName, "err", err)
			return nil, fmt.Errorf("failed to create attach executor: %w", err)
		}

		// Stream stdin to the pod
		err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin: task.Stdin,
		})
		if err != nil {
			logger.Error("pod.attach.stream", "name", podName, "err", err)
			return nil, fmt.Errorf("failed to stream stdin: %w", err)
		}

		logger.Debug("pod.stdin.complete", "name", podName)
	}

	return &Container{
		clientset: k.clientset,
		config:    k.config,
		podName:   createdPod.Name,
		task:      task,
		logger:    logger,
	}, nil
}
