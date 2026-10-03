package tenantcontroller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

func TestCalculateNodeResourceScorePrefersAvailableResources(t *testing.T) {
	node := scoredReadyNode("node-a", "10.200.0.0/24")

	lowUsage := nodeMetrics("node-a", "100m", "256Mi")
	highUsage := nodeMetrics("node-a", "3500m", "7Gi")

	lowScore, err := calculateNodeResourceScore(node, lowUsage, nil)
	if err != nil {
		t.Fatal(err)
	}
	highScore, err := calculateNodeResourceScore(node, highUsage, nil)
	if err != nil {
		t.Fatal(err)
	}

	if lowScore.total <= highScore.total {
		t.Fatalf(
			"low usage score %f must exceed high usage score %f",
			lowScore.total,
			highScore.total,
		)
	}
}

func TestNodeAddressAvailabilityCountsUniquePodIPs(t *testing.T) {
	node := scoredReadyNode("node-a", "10.200.0.0/29")
	pods := []corev1.Pod{
		podOnNode("one", "node-a", "10.200.0.3"),
		podOnNode("two", "node-a", "10.200.0.4"),
		podOnNode("other-node", "node-b", "10.200.0.5"),
		podOnNode("outside", "node-a", "192.168.1.10"),
	}

	got, err := nodeAddressAvailability(node, pods)
	if err != nil {
		t.Fatal(err)
	}

	// A /29 has eight addresses. Network, broadcast and VTEP are reserved,
	// leaving five usable addresses. Two are allocated on node-a.
	want := float64(3) / float64(5)
	if got != want {
		t.Fatalf("availability %f, want %f", got, want)
	}
}

func scoredReadyNode(name, podCIDR string) *corev1.Node {
	node := readyNode(name)
	node.Spec.PodCIDR = podCIDR
	node.Spec.PodCIDRs = []string{podCIDR}
	node.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("4"),
		corev1.ResourceMemory: resource.MustParse("8Gi"),
	}
	return node
}

func nodeMetrics(name, cpu, memory string) *metricsv1beta1.NodeMetrics {
	return &metricsv1beta1.NodeMetrics{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Usage: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(memory),
		},
	}
}

func podOnNode(name, node, ip string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       corev1.PodSpec{NodeName: node},
		Status: corev1.PodStatus{
			PodIP:  ip,
			PodIPs: []corev1.PodIP{{IP: ip}},
		},
	}
}
