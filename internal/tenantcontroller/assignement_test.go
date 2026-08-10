package tenantcontroller

import (
	"net"
	"net/netip"
	"testing"

	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScaleUpCandidatesPreferFewerTenants(t *testing.T) {
	n1 := readyNode("node-a")
	n2 := readyNode("node-b")
	n3 := readyNode("node-c")

	label, err := tenantmeta.NodeTenantLabel("other")
	if err != nil {
		t.Fatal(err)
	}
	n1.Labels[label] = "true"

	got := scaleUpCandidates([]*corev1.Node{n1, n2, n3}, "tenant-a")
	if len(got) != 3 {
		t.Fatalf("got %d candidates, want 3", len(got))
	}
	if got[0].Name != "node-b" || got[1].Name != "node-c" || got[2].Name != "node-a" {
		t.Fatalf("unexpected candidate order: %s, %s, %s", got[0].Name, got[1].Name, got[2].Name)
	}
}

func TestScaleUpCandidatesSkipAssignedAndUnavailableNodes(t *testing.T) {
	assigned := readyNode("assigned")
	unschedulable := readyNode("unschedulable")
	unschedulable.Spec.Unschedulable = true
	notReady := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "not-ready", Labels: map[string]string{}}}
	available := readyNode("available")

	label, err := tenantmeta.NodeTenantLabel("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	assigned.Labels[label] = "true"

	got := scaleUpCandidates(
		[]*corev1.Node{assigned, unschedulable, notReady, available},
		"tenant-a",
	)
	if len(got) != 1 || got[0].Name != "available" {
		t.Fatalf("got %+v, want only available", nodeNames(got))
	}
}

func TestScaleDownCandidatesSkipNodesWithActiveTenantPods(t *testing.T) {
	n1 := readyNode("node-a")
	n2 := readyNode("node-b")

	got := scaleDownCandidates(
		[]*corev1.Node{n1, n2},
		map[string]struct{}{"node-b": {}},
	)
	if len(got) != 1 || got[0].Name != "node-a" {
		t.Fatalf("got %+v, want node-a", nodeNames(got))
	}
}

func TestNodeInfosReadVTEPMetadata(t *testing.T) {
	node := readyNode("node-a")
	node.Status.Addresses = []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "192.0.2.10"},
	}

	labels, annotations, err := tenantmeta.VTEPNodeMetadata(tenantmeta.VTEP{
		IP:  netip.MustParseAddr("10.0.0.10"),
		MAC: mustMAC(t, "02:42:ac:11:00:0a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range labels {
		node.Labels[key] = value
	}
	node.Annotations = annotations

	got := nodeInfos([]*corev1.Node{node})
	if len(got) != 1 {
		t.Fatalf("got %d node infos, want 1", len(got))
	}
	if got[0].NodeIP != "192.0.2.10" || got[0].VtepIP != "10.0.0.10" || got[0].VtepMAC != "02:42:ac:11:00:0a" {
		t.Fatalf("unexpected NodeInfo: %+v", got[0])
	}
}

func readyNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

func nodeNames(nodes []*corev1.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.Name)
	}
	return out
}

func mustMAC(t *testing.T, value string) net.HardwareAddr {
	t.Helper()
	mac, err := net.ParseMAC(value)
	if err != nil {
		t.Fatal(err)
	}
	return mac
}
