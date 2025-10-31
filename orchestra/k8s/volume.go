package k8s

import (
	"context"
	"fmt"

	"github.com/jtarchie/ci/orchestra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Volume struct {
	clientset  *kubernetes.Clientset
	pvcName    string
	volumeName string
}

// Cleanup implements orchestra.Volume.
func (v *Volume) Cleanup(ctx context.Context) error {
	deletePolicy := metav1.DeletePropagationForeground
	err := v.clientset.CoreV1().PersistentVolumeClaims("default").Delete(
		ctx,
		v.pvcName,
		metav1.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		},
	)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("could not destroy volume: %w", err)
	}

	return nil
}

func (k *K8s) CreateVolume(ctx context.Context, name string, size int) (orchestra.Volume, error) {
	pvcName := sanitizeName(fmt.Sprintf("%s-%s", k.namespace, name))

	// Check if PVC already exists
	existingPVC, err := k.clientset.CoreV1().PersistentVolumeClaims("default").Get(ctx, pvcName, metav1.GetOptions{})
	if err == nil {
		return &Volume{
			clientset:  k.clientset,
			pvcName:    existingPVC.Name,
			volumeName: name,
		}, nil
	}

	// Determine storage size
	storageSize := "1Gi" // default
	if size > 0 {
		storageSize = fmt.Sprintf("%dMi", size/(1024*1024))
	}

	// Create PVC with ReadWriteOnce (RWO) access mode
	// The project scheduler ensures serial access (one pod at a time),
	// so RWO is sufficient and provides better performance than RWX
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvcName,
			Labels: map[string]string{
				"orchestra.namespace": sanitizeLabel(k.namespace),
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}

	createdPVC, err := k.clientset.CoreV1().PersistentVolumeClaims("default").Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not create volume: %w", err)
	}

	return &Volume{
		clientset:  k.clientset,
		pvcName:    createdPVC.Name,
		volumeName: name,
	}, nil
}

func (v *Volume) Name() string {
	return v.volumeName
}
