package k8s

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func EnsureNamespace(
	ctx context.Context,
	client *kubernetes.Clientset,
	name string,
	tenant string,
) error {

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"misokube.com/tenant": tenant,
			},
		},
	}

	_, err := client.CoreV1().
		Namespaces().
		Create(ctx, ns, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}

	return err
}
