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
	clientset *kubernetes.Clientset
	logger    *slog.Logger
	namespace string
}

// Close implements orchestra.Driver.
func (k *K8s) Close() error {
	ctx := context.Background()

	// Delete all pods in the namespace with our label
	labelSelector := fmt.Sprintf("orchestra.namespace=%s", sanitizeLabel(k.namespace))

	deletePolicy := metav1.DeletePropagationForeground
	err := k.clientset.CoreV1().Pods("default").DeleteCollection(
		ctx,
		metav1.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		},
		metav1.ListOptions{
			LabelSelector: labelSelector,
		},
	)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete pods: %w", err)
	}

	// Delete all PVCs in the namespace with our label
	err = k.clientset.CoreV1().PersistentVolumeClaims("default").DeleteCollection(
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

func NewK8s(namespace string, logger *slog.Logger) (orchestra.Driver, error) {
	// Try to get in-cluster config first (for running inside k8s)
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig (for local development)
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
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

	return &K8s{
		clientset: clientset,
		logger:    logger,
		namespace: namespace,
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
