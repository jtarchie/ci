package k8s

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jtarchie/ci/orchestra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type K8s struct {
	clientset    *kubernetes.Clientset
	config       *rest.Config
	logger       *slog.Logger
	namespace    string // Orchestra namespace (for labeling)
	k8sNamespace string // Kubernetes namespace (for resource placement)
}

// Close implements orchestra.Driver.
func (k *K8s) Close() error {
	ctx := context.Background()

	// Delete all jobs in the namespace with our label (pods will be cascade deleted)
	labelSelector := fmt.Sprintf("orchestra.namespace=%s", sanitizeLabel(k.namespace))

	deletePolicy := metav1.DeletePropagationForeground
	err := k.clientset.BatchV1().Jobs(k.k8sNamespace).DeleteCollection(
		ctx,
		metav1.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		},
		metav1.ListOptions{
			LabelSelector: labelSelector,
		},
	)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete jobs: %w", err)
	}

	// Delete all PVCs in the namespace with our label
	err = k.clientset.CoreV1().PersistentVolumeClaims(k.k8sNamespace).DeleteCollection(
		ctx,
		metav1.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		},
		metav1.ListOptions{
			LabelSelector: labelSelector,
		},
	)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete PVCs: %w", err)
	}

	return nil
}

func NewK8s(namespace string, logger *slog.Logger, params map[string]string) (orchestra.Driver, error) {
	// Try to get in-cluster config first (for running inside k8s)
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig (for local development)
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

		// Check DSN parameter for kubeconfig path
		if kubeconfigPath := params["kubeconfig"]; kubeconfigPath != "" {
			loadingRules.ExplicitPath = kubeconfigPath
		}

		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		config, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Determine the K8s namespace to use for resources from DSN parameters
	k8sNamespace := params["namespace"]
	if k8sNamespace == "" {
		k8sNamespace = "default"
	}

	logger.Info("k8s.config", "k8sNamespace", k8sNamespace, "orchestraNamespace", namespace)

	return &K8s{
		clientset:    clientset,
		config:       config,
		logger:       logger,
		namespace:    namespace,
		k8sNamespace: k8sNamespace,
	}, nil
}

func (k *K8s) Name() string {
	return "k8s"
}

func init() {
	orchestra.Add("k8s", NewK8s)
}

var (
	_ orchestra.Driver          = &K8s{}
	_ orchestra.Container       = &Container{}
	_ orchestra.ContainerStatus = &ContainerStatus{}
	_ orchestra.Volume          = &Volume{}
)
