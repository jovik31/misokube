package tenantcontroller

import (
	"context"
	"fmt"

	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// podReader provides the only Pod information required by the tenant controller.
// The orchestrator does not keep a Pod informer. It queries Pods only when a
// Tenant assignment must be reduced or removed.
type podReader interface {
	ActiveTenantPodNodes(ctx context.Context, tenant string) (map[string]struct{}, error)
}

type kubePodReader struct {
	kube kubernetes.Interface
}

func newKubePodReader(kube kubernetes.Interface) *kubePodReader {
	return &kubePodReader{kube: kube}
}

func (r *kubePodReader) ActiveTenantPodNodes(ctx context.Context, tenant string) (map[string]struct{}, error) {
	selector := labels.SelectorFromSet(labels.Set{
		tenantmeta.PodTenantLabel: tenant,
	}).String()

	pods, err := r.kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("list pods for tenant %q: %w", tenant, err)
	}

	nodes := make(map[string]struct{})
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Spec.NodeName == "" || podTerminal(pod) {
			continue
		}
		nodes[pod.Spec.NodeName] = struct{}{}
	}

	return nodes, nil
}

func podTerminal(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}
