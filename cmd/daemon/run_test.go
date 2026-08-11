package main

import (
	"net/netip"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestNodeIPv4PodCIDR(t *testing.T) {
	node := &corev1.Node{}
	node.Name = "node-a"
	node.Spec.PodCIDRs = []string{
		"fd00:10:244::/64",
		"10.244.1.0/24",
	}

	got, err := nodeIPv4PodCIDR(node)
	if err != nil {
		t.Fatal(err)
	}
	if want := netip.MustParsePrefix("10.244.1.0/24"); got != want {
		t.Fatalf("PodCIDR = %s, want %s", got, want)
	}
}

func TestNodeIPv4PodCIDRFallsBackToPodCIDR(t *testing.T) {
	node := &corev1.Node{}
	node.Name = "node-a"
	node.Spec.PodCIDR = "10.244.2.0/24"

	got, err := nodeIPv4PodCIDR(node)
	if err != nil {
		t.Fatal(err)
	}
	if want := netip.MustParsePrefix("10.244.2.0/24"); got != want {
		t.Fatalf("PodCIDR = %s, want %s", got, want)
	}
}

func TestReservedNodeAddresses(t *testing.T) {
	prefix := netip.MustParsePrefix("10.244.1.0/24")

	got := reservedNodeAddresses(prefix)
	set := make(map[netip.Addr]struct{}, len(got))
	for _, addr := range got {
		set[addr] = struct{}{}
	}

	for _, want := range []netip.Addr{
		netip.MustParseAddr("10.244.1.0"),
		netip.MustParseAddr("10.244.1.1"),
		netip.MustParseAddr("10.244.1.255"),
	} {
		if _, ok := set[want]; !ok {
			t.Fatalf("reserved addresses %v do not contain %s", got, want)
		}
	}
}

func TestReservedNodeAddressesAlwaysReservesVTEP(t *testing.T) {
	got := reservedNodeAddresses(
		netip.MustParsePrefix("10.244.1.0/24"),
	)

	want := netip.MustParseAddr("10.244.1.1")
	for _, addr := range got {
		if addr == want {
			return
		}
	}
	t.Fatalf("reserved addresses %v do not contain VTEP %s", got, want)
}