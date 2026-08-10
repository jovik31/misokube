package tenantcontroller

import (
	"context"
	"testing"

	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKubePodReaderActiveTenantPodNodes(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tenant-a-running",
				Namespace: "default",
				Labels: map[string]string{
					tenantmeta.PodTenantLabel: "tenant-a",
				},
			},
			Spec: corev1.PodSpec{NodeName: "node-a"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tenant-a-succeeded",
				Namespace: "default",
				Labels: map[string]string{
					tenantmeta.PodTenantLabel: "tenant-a",
				},
			},
			Spec: corev1.PodSpec{NodeName: "node-b"},
			Status: corev1.PodStatus{
				Phase: corev1.PodSucceeded,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tenant-b-running",
				Namespace: "default",
				Labels: map[string]string{
					tenantmeta.PodTenantLabel: "tenant-b",
				},
			},
			Spec: corev1.PodSpec{NodeName: "node-c"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
	)

	reader := newKubePodReader(client)

	nodes, err := reader.ActiveTenantPodNodes(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}

	if len(nodes) != 1 {
		t.Fatalf("got %d active nodes, want 1", len(nodes))
	}
	if _, ok := nodes["node-a"]; !ok {
		t.Fatal("node-a missing from active tenant pod nodes")
	}
	if _, ok := nodes["node-b"]; ok {
		t.Fatal("terminal pod node must not be active")
	}
	if _, ok := nodes["node-c"]; ok {
		t.Fatal("different tenant node must not be returned")
	}
}

func TestKubePodReaderIgnoresUnscheduledPods(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pending",
				Namespace: "default",
				Labels: map[string]string{
					tenantmeta.PodTenantLabel: "tenant-a",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		},
	)

	reader := newKubePodReader(client)
	nodes, err := reader.ActiveTenantPodNodes(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}

	if len(nodes) != 0 {
		t.Fatalf("got %d active nodes, want 0", len(nodes))
	}
}
