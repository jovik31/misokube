package ebpfmanager

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github/setera/pkg/ebpf"
)

func TestReconcileRemotePodsRebuildsAndSweepsRemoteState(t *testing.T) {
	fake := newSyncFake()
	manager := newWithDependencies(fake.dependencies())

	staleIP := netip.MustParseAddr("10.244.2.99")
	localIP := netip.MustParseAddr("10.244.1.10")

	fake.endpoints = []ebpf.PodEndpoint{
		{
			IP:       staleIP,
			TenantID: "tenant-old",
			IfIndex:  ebpf.RemotePodIfIndex,
		},
		{
			IP:       localIP,
			TenantID: "tenant-a",
			IfIndex:  42,
		},
	}

	desired := RemotePod{
		IP:       netip.MustParseAddr("10.244.2.20"),
		PodUID:   "remote-new",
		TenantID: "tenant-a",
	}

	if err := manager.ReconcileRemotePods(
		context.Background(),
		[]RemotePod{desired},
	); err != nil {
		t.Fatal(err)
	}

	if len(fake.upserts) != 1 ||
		fake.upserts[0].IP != desired.IP ||
		fake.upserts[0].IfIndex != ebpf.RemotePodIfIndex {
		t.Fatalf("upserts = %+v", fake.upserts)
	}

	if len(fake.deletes) != 1 || fake.deletes[0] != staleIP {
		t.Fatalf("deletes = %v, want [%s]", fake.deletes, staleIP)
	}

	if _, ok := manager.remote[desired.IP]; !ok {
		t.Fatal("desired remote Pod not tracked")
	}
	if _, ok := manager.remote[staleIP]; ok {
		t.Fatal("stale remote Pod still tracked")
	}
}

func TestReconcileRemotePodsNeverDeletesLocalEndpoint(t *testing.T) {
	fake := newSyncFake()
	manager := newWithDependencies(fake.dependencies())

	localIP := netip.MustParseAddr("10.244.1.10")
	fake.endpoints = []ebpf.PodEndpoint{{
		IP:       localIP,
		TenantID: "tenant-a",
		IfIndex:  42,
	}}

	if err := manager.ReconcileRemotePods(
		context.Background(),
		nil,
	); err != nil {
		t.Fatal(err)
	}

	if len(fake.deletes) != 0 {
		t.Fatalf("local endpoint was deleted: %v", fake.deletes)
	}
}

func TestReconcileRemotePodsRejectsActualLocalCollision(t *testing.T) {
	fake := newSyncFake()
	manager := newWithDependencies(fake.dependencies())

	ip := netip.MustParseAddr("10.244.1.10")
	fake.endpoints = []ebpf.PodEndpoint{{
		IP:       ip,
		TenantID: "tenant-a",
		IfIndex:  42,
	}}

	err := manager.ReconcileRemotePods(
		context.Background(),
		[]RemotePod{{
			IP:       ip,
			PodUID:   "remote-a",
			TenantID: "tenant-a",
		}},
	)
	if !errors.Is(err, ErrRemotePodConflict) {
		t.Fatalf("error = %v, want ErrRemotePodConflict", err)
	}
	if len(fake.upserts) != 0 {
		t.Fatal("collision changed endpoint map")
	}
}

func TestReconcileRemotePodsRejectsDuplicateIPOwnership(t *testing.T) {
	fake := newSyncFake()
	manager := newWithDependencies(fake.dependencies())

	ip := netip.MustParseAddr("10.244.2.20")
	err := manager.ReconcileRemotePods(
		context.Background(),
		[]RemotePod{
			{
				IP:       ip,
				PodUID:   "pod-a",
				TenantID: "tenant-a",
			},
			{
				IP:       ip,
				PodUID:   "pod-b",
				TenantID: "tenant-b",
			},
		},
	)
	if !errors.Is(err, ErrRemotePodConflict) {
		t.Fatalf("error = %v, want ErrRemotePodConflict", err)
	}
}

type syncWrite struct {
	IP       netip.Addr
	TenantID string
	IfIndex  int
}

type syncFake struct {
	endpoints []ebpf.PodEndpoint
	upserts   []syncWrite
	deletes   []netip.Addr
}

func newSyncFake() *syncFake {
	return &syncFake{}
}

func (f *syncFake) dependencies() dependencies {
	return dependencies{
		attachPodProgram: func(string, string) (podProgram, error) {
			panic("unexpected attach")
		},
		upsertEndpoint: func(
			ip netip.Addr,
			tenant string,
			ifIndex int,
		) error {
			f.upserts = append(f.upserts, syncWrite{
				IP:       ip,
				TenantID: tenant,
				IfIndex:  ifIndex,
			})
			return nil
		},
		deleteEndpoint: func(ip netip.Addr) error {
			f.deletes = append(f.deletes, ip)
			return nil
		},
		listEndpoints: func() ([]ebpf.PodEndpoint, error) {
			return append([]ebpf.PodEndpoint(nil), f.endpoints...), nil
		},
	}
}
