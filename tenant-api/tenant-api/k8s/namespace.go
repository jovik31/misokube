package k8s

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func NewNamespace(client *kubernetes.Clientset, name string) error {

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "setera-" + name,
		},
	}

	_, err := client.CoreV1().
		Namespaces().
		Create(context.TODO(), ns, metav1.CreateOptions{})

	return err
}