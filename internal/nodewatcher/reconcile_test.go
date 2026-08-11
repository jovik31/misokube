package nodewatcher

import (
	"net"
	"net/netip"
	"testing"

	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
)

func TestRemoteNodeFor(t *testing.T) {
	mac, err := net.ParseMAC("02:00:00:00:00:02")
	if err != nil {
		t.Fatal(err)
	}
	labels, annotations, err := tenantmeta.VTEPNodeMetadata(tenantmeta.VTEP{
		IP:  netip.MustParseAddr("10.244.2.1"),
		MAC: mac,
	})
	if err != nil {
		t.Fatal(err)
	}

	node := &corev1.Node{}
	node.Name = "node-b"
	node.Labels = labels
	node.Annotations = annotations
	node.Spec.PodCIDR = "10.244.2.0/24"
	node.Status.Addresses = []corev1.NodeAddress{{
		Type:    corev1.NodeInternalIP,
		Address: "172.18.0.3",
	}}

	got, ready, err := remoteNodeFor(node)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("expected remote Node to be routing-ready")
	}
	if got.Name != "node-b" ||
		got.PodCIDR != netip.MustParsePrefix("10.244.2.0/24") ||
		got.UnderlayIP != netip.MustParseAddr("172.18.0.3") ||
		got.VTEPIP != netip.MustParseAddr("10.244.2.1") ||
		got.VTEPMAC.String() != mac.String() {
		t.Fatalf("got %+v", got)
	}
}

func TestRemoteNodeForSkipsNodeWithoutVTEP(t *testing.T) {
	node := &corev1.Node{}
	node.Name = "node-b"
	node.Spec.PodCIDR = "10.244.2.0/24"

	_, ready, err := remoteNodeFor(node)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("expected Node without VTEP metadata to be skipped")
	}
}