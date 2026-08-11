package ebpfmanager

import (
	"context"
	"net/netip"
	"os"
	"testing"

	"github/setera/internal/podnetwork"
	"github/setera/pkg/ebpf"
)

// TestIntegrationReconcileRemotePodsAfterRestart proves that startup remote
// reconciliation removes stale remote entries from the pinned map while
// preserving a recovered local endpoint.
func TestIntegrationReconcileRemotePodsAfterRestart(t *testing.T) {
	if os.Getenv(ebpfIntegrationEnv) != "1" {
		t.Skip(
			"set SETERA_EBPF_INTEGRATION=1 to run privileged eBPF integration tests",
		)
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root or equivalent eBPF/TC capabilities")
	}

	host, _ := addIntegrationVethPair(t)

	localIP := netip.MustParseAddr("198.18.0.248")
	staleIP := netip.MustParseAddr("198.18.0.249")
	desiredIP := netip.MustParseAddr("198.18.0.250")

	for _, ip := range []netip.Addr{
		localIP,
		staleIP,
		desiredIP,
	} {
		deletePinnedPodEndpointIfPresent(t, ip)
	}

	beforeRestart := New()

	local := podnetwork.LocalPod{
		IP:              localIP,
		PodUID:          "local-recovered",
		TenantID:        "tenant-a",
		HostVethName:    host.Attrs().Name,
		HostVethIfIndex: host.Attrs().Index,
	}
	if err := beforeRestart.AddLocalPod(
		context.Background(),
		local,
	); err != nil {
		t.Fatalf("AddLocalPod: %v", err)
	}

	stale := RemotePod{
		IP:       staleIP,
		PodUID:   "stale-remote",
		TenantID: "tenant-a",
	}
	if err := beforeRestart.UpsertRemotePod(
		context.Background(),
		stale,
	); err != nil {
		t.Fatalf("Upsert stale remote: %v", err)
	}

	// Fresh manager: recover local ownership first, then reconcile remote
	// desired state from the informer cache.
	restarted := New()
	if err := restarted.RecoverLocalPod(
		context.Background(),
		local,
	); err != nil {
		t.Fatalf("RecoverLocalPod: %v", err)
	}

	desired := RemotePod{
		IP:       desiredIP,
		PodUID:   "desired-remote",
		TenantID: "default",
	}
	if err := restarted.ReconcileRemotePods(
		context.Background(),
		[]RemotePod{desired},
	); err != nil {
		t.Fatalf("ReconcileRemotePods: %v", err)
	}

	requirePinnedPodEndpoint(
		t,
		localIP,
		local.TenantID,
		local.HostVethIfIndex,
	)
	requirePinnedPodEndpointMissing(t, staleIP)
	requirePinnedPodEndpoint(
		t,
		desiredIP,
		desired.TenantID,
		ebpf.RemotePodIfIndex,
	)

	if err := restarted.DeleteRemotePod(
		context.Background(),
		desired.IP,
		desired.PodUID,
	); err != nil {
		t.Fatalf("cleanup desired remote: %v", err)
	}
	if err := restarted.DeleteLocalPod(
		context.Background(),
		local.IP,
	); err != nil {
		t.Fatalf("cleanup local: %v", err)
	}

	t.Logf(
		"remote startup reconciliation verified: stale=%s removed; desired=%s installed; local=%s preserved",
		staleIP,
		desiredIP,
		localIP,
	)
}
