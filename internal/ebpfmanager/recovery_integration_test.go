package ebpfmanager

import (
	"context"
	"net/netip"
	"os"
	"testing"

	"github/setera/internal/podnetwork"
	"github/setera/pkg/network"
)

// TestIntegrationRecoverLocalPod simulates a daemon restart while a Pod and
// its host veth are still alive.
//
// The first Manager installs the datapath and is then intentionally abandoned
// without DeleteLocalPod, which leaves the kernel TC filters and shared map
// entry in the same state a crashed daemon would leave behind. A fresh Manager
// rediscovers the veth from the Pod /32 route and recovers the datapath.
func TestIntegrationRecoverLocalPod(t *testing.T) {
	if os.Getenv(ebpfIntegrationEnv) != "1" {
		t.Skip(
			"set SETERA_EBPF_INTEGRATION=1 to run privileged eBPF integration tests",
		)
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root or equivalent eBPF/TC capabilities")
	}

	host, _ := addIntegrationVethPair(t)

	podIP := netip.MustParseAddr("198.18.0.253")
	prefix := netip.PrefixFrom(podIP, 32)

	networkOps := network.NewLinux()
	if err := networkOps.ReplaceRoute(network.Route{
		Prefix:  prefix,
		IfIndex: host.Attrs().Index,
	}); err != nil {
		t.Fatalf("install Pod host route: %v", err)
	}
	t.Cleanup(func() {
		_ = networkOps.DeleteRoute(network.Route{
			Prefix:  prefix,
			IfIndex: host.Attrs().Index,
		})
	})

	deletePinnedPodEndpointIfPresent(t, podIP)

	original := New()
	pod := podnetwork.LocalPod{
		IP:              podIP,
		PodUID:          "recovery-pod-uid",
		TenantID:        "tenant-a",
		HostVethName:    host.Attrs().Name,
		HostVethIfIndex: host.Attrs().Index,
	}

	if err := original.AddLocalPod(
		context.Background(),
		pod,
	); err != nil {
		t.Fatalf("initial AddLocalPod: %v", err)
	}

	requireSeteraPodFilters(t, host)
	requirePinnedPodEndpoint(
		t,
		podIP,
		pod.TenantID,
		pod.HostVethIfIndex,
	)

	// Simulate a process restart. Do not close the original Manager. In the
	// real crash case its userspace state disappears while the TC attachment
	// and pinned map remain in the kernel.
	recoveredVeth, err := networkOps.FindPodVeth(podIP)
	if err != nil {
		t.Fatalf("rediscover Pod veth: %v", err)
	}

	if recoveredVeth.HostName != pod.HostVethName {
		t.Fatalf(
			"recovered host veth = %q, want %q",
			recoveredVeth.HostName,
			pod.HostVethName,
		)
	}
	if recoveredVeth.HostIfIndex != pod.HostVethIfIndex {
		t.Fatalf(
			"recovered ifindex = %d, want %d",
			recoveredVeth.HostIfIndex,
			pod.HostVethIfIndex,
		)
	}

	restarted := New()
	recoveredPod := podnetwork.LocalPod{
		IP:              podIP,
		PodUID:          pod.PodUID,
		TenantID:        pod.TenantID,
		HostVethName:    recoveredVeth.HostName,
		HostVethIfIndex: recoveredVeth.HostIfIndex,
	}

	if err := restarted.RecoverLocalPod(
		context.Background(),
		recoveredPod,
	); err != nil {
		t.Fatalf("RecoverLocalPod: %v", err)
	}

	requireLocalManagerRecord(t, restarted, recoveredPod)
	requireSeteraPodFilters(t, host)
	requirePinnedPodEndpoint(
		t,
		podIP,
		recoveredPod.TenantID,
		recoveredPod.HostVethIfIndex,
	)

	if err := restarted.DeleteLocalPod(
		context.Background(),
		podIP,
	); err != nil {
		t.Fatalf("DeleteLocalPod after recovery: %v", err)
	}

	requireNoLocalManagerRecord(t, restarted, podIP)
	requirePinnedPodEndpointMissing(t, podIP)
	requireNoSeteraPodFilters(t, host)
	requireClsact(t, host)

	t.Logf(
		"restart recovery verified: ip=%s tenant=%s hostVeth=%s ifindex=%d",
		podIP,
		recoveredPod.TenantID,
		recoveredPod.HostVethName,
		recoveredPod.HostVethIfIndex,
	)
}
