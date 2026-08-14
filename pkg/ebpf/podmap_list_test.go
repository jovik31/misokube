package ebpf

import (
	"net"
	"net/netip"
	"testing"

	"github/setera/pkg/ebpf/loader"
)

func TestConvertListedPodEndpoints(t *testing.T) {
	entries := []loader.PodTenantVeth{
		{
			IP:      net.ParseIP("10.244.2.20"),
			Tenant:  "tenant-b",
			IfIndex: RemotePodIfIndex,
		},
		{
			IP:      net.ParseIP("10.244.1.10"),
			Tenant:  "tenant-a",
			IfIndex: 42,
		},
	}

	got, err := podEndpointsFromLoader(entries)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	if got[0].IP != netip.MustParseAddr("10.244.1.10") ||
		got[0].TenantID != "tenant-a" ||
		got[0].IfIndex != 42 {
		t.Fatalf("first endpoint = %+v", got[0])
	}

	if got[1].IP != netip.MustParseAddr("10.244.2.20") ||
		got[1].TenantID != "tenant-b" ||
		got[1].IfIndex != RemotePodIfIndex {
		t.Fatalf("second endpoint = %+v", got[1])
	}
}

func TestConvertListedPodEndpointsRejectsInvalidIfIndex(t *testing.T) {
	_, err := podEndpointsFromLoader([]loader.PodTenantVeth{{
		IP:      net.ParseIP("10.244.1.10"),
		Tenant:  "tenant-a",
		IfIndex: 0,
	}})
	if err == nil {
		t.Fatal("expected error")
	}
}
