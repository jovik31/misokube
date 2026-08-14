package ebpfmanager

import (
	"context"
	"net/netip"
	"os"
	"testing"

	"github/setera/pkg/ebpf"
)

// TestIntegrationRemotePodLifecycle validates the remote-only manager path
// against the real pinned tc_podIDs map. No TC program is attached because the
// Pod lives on another Node.
func TestIntegrationRemotePodLifecycle(t *testing.T) {
	if os.Getenv(ebpfIntegrationEnv) != "1" {
		t.Skip(
			"set SETERA_EBPF_INTEGRATION=1 to run privileged eBPF integration tests",
		)
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root or equivalent eBPF capabilities")
	}

	pod := RemotePod{
		IP:       netip.MustParseAddr("198.18.0.252"),
		PodUID:   "remote-integration-pod",
		TenantID: "tenant-a",
	}

	deletePinnedPodEndpointIfPresent(t, pod.IP)

	manager := New()

	if err := manager.UpsertRemotePod(
		context.Background(),
		pod,
	); err != nil {
		t.Fatalf("UpsertRemotePod: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.DeleteRemotePod(
			context.Background(),
			pod.IP,
			pod.PodUID,
		)
	})

	requirePinnedPodEndpoint(
		t,
		pod.IP,
		pod.TenantID,
		ebpf.RemotePodIfIndex,
	)

	record, ok := manager.remote[pod.IP]
	if !ok {
		t.Fatalf("manager does not track remote Pod %s", pod.IP)
	}
	if record.PodUID != pod.PodUID ||
		record.TenantID != pod.TenantID {
		t.Fatalf("unexpected remote record: %+v", record)
	}

	if err := manager.DeleteRemotePod(
		context.Background(),
		pod.IP,
		pod.PodUID,
	); err != nil {
		t.Fatalf("DeleteRemotePod: %v", err)
	}

	requirePinnedPodEndpointMissing(t, pod.IP)

	if _, ok := manager.remote[pod.IP]; ok {
		t.Fatalf("manager still tracks remote Pod %s", pod.IP)
	}

	t.Logf(
		"remote Pod lifecycle verified: ip=%s tenant=%s ifindex=%d",
		pod.IP,
		pod.TenantID,
		ebpf.RemotePodIfIndex,
	)
}
