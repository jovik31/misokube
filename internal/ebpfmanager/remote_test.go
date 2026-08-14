package ebpfmanager

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github/setera/pkg/ebpf"
)

func TestUpsertRemotePod(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testRemotePod()

	if err := manager.UpsertRemotePod(
		context.Background(),
		pod,
	); err != nil {
		t.Fatal(err)
	}

	if fake.attachCalls != 0 {
		t.Fatalf(
			"remote Pod attached %d programs, want 0",
			fake.attachCalls,
		)
	}
	if fake.upsertCalls != 1 {
		t.Fatalf(
			"upsert calls = %d, want 1",
			fake.upsertCalls,
		)
	}
	if fake.lastIP != pod.IP {
		t.Fatalf(
			"endpoint IP = %s, want %s",
			fake.lastIP,
			pod.IP,
		)
	}
	if fake.lastTenant != pod.TenantID {
		t.Fatalf(
			"endpoint tenant = %q, want %q",
			fake.lastTenant,
			pod.TenantID,
		)
	}
	if fake.lastIfIndex != ebpf.RemotePodIfIndex {
		t.Fatalf(
			"endpoint ifindex = %d, want %d",
			fake.lastIfIndex,
			ebpf.RemotePodIfIndex,
		)
	}

	record, ok := manager.remote[pod.IP]
	if !ok {
		t.Fatal("remote Pod state was not stored")
	}
	if record.PodUID != pod.PodUID {
		t.Fatalf(
			"stored PodUID = %q, want %q",
			record.PodUID,
			pod.PodUID,
		)
	}
	if record.TenantID != pod.TenantID {
		t.Fatalf(
			"stored tenant = %q, want %q",
			record.TenantID,
			pod.TenantID,
		)
	}
}

func TestUpsertRemotePodDuplicateRefreshesEndpoint(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testRemotePod()

	if err := manager.UpsertRemotePod(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	if err := manager.UpsertRemotePod(context.Background(), pod); err != nil {
		t.Fatal(err)
	}

	if fake.upsertCalls != 2 {
		t.Fatalf(
			"upsert calls = %d, want 2",
			fake.upsertCalls,
		)
	}
	if fake.attachCalls != 0 {
		t.Fatalf(
			"attach calls = %d, want 0",
			fake.attachCalls,
		)
	}
}

func TestUpsertRemotePodAllowsIPReuseByNewPod(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	first := testRemotePod()

	if err := manager.UpsertRemotePod(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	second := first
	second.PodUID = "remote-pod-b"
	second.TenantID = "tenant-b"

	if err := manager.UpsertRemotePod(context.Background(), second); err != nil {
		t.Fatal(err)
	}

	record := manager.remote[first.IP]
	if record.PodUID != second.PodUID {
		t.Fatalf(
			"stored PodUID = %q, want %q",
			record.PodUID,
			second.PodUID,
		)
	}
	if record.TenantID != second.TenantID {
		t.Fatalf(
			"stored tenant = %q, want %q",
			record.TenantID,
			second.TenantID,
		)
	}
}

func TestUpsertRemotePodKeepsOldStateWhenEndpointUpdateFails(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	first := testRemotePod()

	if err := manager.UpsertRemotePod(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	second := first
	second.PodUID = "remote-pod-b"
	second.TenantID = "tenant-b"

	fake.upsertErr = errors.New("map update failed")
	err := manager.UpsertRemotePod(context.Background(), second)
	if !errors.Is(err, fake.upsertErr) {
		t.Fatalf(
			"error = %v, want wrapped %v",
			err,
			fake.upsertErr,
		)
	}

	record := manager.remote[first.IP]
	if record.PodUID != first.PodUID ||
		record.TenantID != first.TenantID {
		t.Fatalf(
			"state changed after failed update: %+v",
			record,
		)
	}
}

func TestUpsertRemotePodRejectsLocalCollision(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())

	local := testLocalPod()
	if err := manager.AddLocalPod(context.Background(), local); err != nil {
		t.Fatal(err)
	}

	upsertsBefore := fake.upsertCalls

	remote := testRemotePod()
	remote.IP = local.IP

	err := manager.UpsertRemotePod(context.Background(), remote)
	if !errors.Is(err, ErrRemotePodConflict) {
		t.Fatalf(
			"error = %v, want ErrRemotePodConflict",
			err,
		)
	}
	if fake.upsertCalls != upsertsBefore {
		t.Fatal("remote collision modified endpoint map")
	}
}

func TestAddLocalPodRejectsRemoteCollision(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())

	remote := testRemotePod()
	if err := manager.UpsertRemotePod(context.Background(), remote); err != nil {
		t.Fatal(err)
	}

	attachBefore := fake.attachCalls
	upsertBefore := fake.upsertCalls

	local := testLocalPod()
	local.IP = remote.IP

	err := manager.AddLocalPod(context.Background(), local)
	if !errors.Is(err, ErrLocalPodConflict) {
		t.Fatalf(
			"error = %v, want ErrLocalPodConflict",
			err,
		)
	}
	if fake.attachCalls != attachBefore ||
		fake.upsertCalls != upsertBefore {
		t.Fatal("local collision modified datapath")
	}
}

func TestDeleteRemotePod(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testRemotePod()

	if err := manager.UpsertRemotePod(context.Background(), pod); err != nil {
		t.Fatal(err)
	}

	if err := manager.DeleteRemotePod(
		context.Background(),
		pod.IP,
		pod.PodUID,
	); err != nil {
		t.Fatal(err)
	}

	if fake.deleteCalls != 1 {
		t.Fatalf(
			"delete calls = %d, want 1",
			fake.deleteCalls,
		)
	}
	if _, ok := manager.remote[pod.IP]; ok {
		t.Fatal("remote Pod state still exists after delete")
	}
}

func TestDeleteRemotePodIgnoresStaleDeleteAfterIPReuse(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	first := testRemotePod()

	if err := manager.UpsertRemotePod(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	second := first
	second.PodUID = "remote-pod-b"
	second.TenantID = "tenant-b"
	if err := manager.UpsertRemotePod(context.Background(), second); err != nil {
		t.Fatal(err)
	}

	deletesBefore := fake.deleteCalls

	if err := manager.DeleteRemotePod(
		context.Background(),
		first.IP,
		first.PodUID,
	); err != nil {
		t.Fatal(err)
	}

	if fake.deleteCalls != deletesBefore {
		t.Fatal("stale delete removed endpoint")
	}

	record := manager.remote[first.IP]
	if record.PodUID != second.PodUID ||
		record.TenantID != second.TenantID {
		t.Fatalf(
			"stale delete changed current remote state: %+v",
			record,
		)
	}
}

func TestDeleteRemotePodWithoutRecordStillDeletesEndpoint(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testRemotePod()

	if err := manager.DeleteRemotePod(
		context.Background(),
		pod.IP,
		pod.PodUID,
	); err != nil {
		t.Fatal(err)
	}

	if fake.deleteCalls != 1 {
		t.Fatalf(
			"delete calls = %d, want 1",
			fake.deleteCalls,
		)
	}
}

func TestDeleteRemotePodRejectsLocalOwner(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())

	local := testLocalPod()
	if err := manager.AddLocalPod(context.Background(), local); err != nil {
		t.Fatal(err)
	}

	deletesBefore := fake.deleteCalls

	err := manager.DeleteRemotePod(
		context.Background(),
		local.IP,
		"old-remote-pod",
	)
	if !errors.Is(err, ErrRemotePodConflict) {
		t.Fatalf(
			"error = %v, want ErrRemotePodConflict",
			err,
		)
	}
	if fake.deleteCalls != deletesBefore {
		t.Fatal("remote delete removed local endpoint")
	}
}

func TestDeleteLocalPodRejectsRemoteOwner(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())

	remote := testRemotePod()
	if err := manager.UpsertRemotePod(context.Background(), remote); err != nil {
		t.Fatal(err)
	}

	deletesBefore := fake.deleteCalls

	err := manager.DeleteLocalPod(
		context.Background(),
		remote.IP,
	)
	if !errors.Is(err, ErrLocalPodConflict) {
		t.Fatalf(
			"error = %v, want ErrLocalPodConflict",
			err,
		)
	}
	if fake.deleteCalls != deletesBefore {
		t.Fatal("local delete removed remote endpoint")
	}
}

func TestDeleteRemotePodKeepsStateWhenEndpointDeleteFails(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testRemotePod()

	if err := manager.UpsertRemotePod(context.Background(), pod); err != nil {
		t.Fatal(err)
	}

	fake.deleteErr = errors.New("map delete failed")

	err := manager.DeleteRemotePod(
		context.Background(),
		pod.IP,
		pod.PodUID,
	)
	if !errors.Is(err, fake.deleteErr) {
		t.Fatalf(
			"error = %v, want wrapped %v",
			err,
			fake.deleteErr,
		)
	}
	if _, ok := manager.remote[pod.IP]; !ok {
		t.Fatal("remote state removed after failed map delete")
	}
}

func TestUpsertRemotePodRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		edit func(*RemotePod)
	}{
		{
			name: "invalid IP",
			edit: func(p *RemotePod) {
				p.IP = netip.Addr{}
			},
		},
		{
			name: "IPv6",
			edit: func(p *RemotePod) {
				p.IP = netip.MustParseAddr("2001:db8::1")
			},
		},
		{
			name: "missing Pod UID",
			edit: func(p *RemotePod) {
				p.PodUID = ""
			},
		},
		{
			name: "missing tenant",
			edit: func(p *RemotePod) {
				p.TenantID = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeDependencies()
			manager := newWithDependencies(fake.dependencies())
			pod := testRemotePod()
			tt.edit(&pod)

			err := manager.UpsertRemotePod(
				context.Background(),
				pod,
			)
			if !errors.Is(err, ErrInvalidRemotePod) {
				t.Fatalf(
					"error = %v, want ErrInvalidRemotePod",
					err,
				)
			}
			if fake.upsertCalls != 0 {
				t.Fatal("invalid remote Pod modified endpoint map")
			}
		})
	}
}

func TestDeleteRemotePodRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		ip     netip.Addr
		podUID string
	}{
		{
			name:   "invalid IP",
			ip:     netip.Addr{},
			podUID: "pod-a",
		},
		{
			name:   "IPv6",
			ip:     netip.MustParseAddr("2001:db8::1"),
			podUID: "pod-a",
		},
		{
			name:   "missing Pod UID",
			ip:     netip.MustParseAddr("10.244.2.10"),
			podUID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeDependencies()
			manager := newWithDependencies(fake.dependencies())

			err := manager.DeleteRemotePod(
				context.Background(),
				tt.ip,
				tt.podUID,
			)
			if !errors.Is(err, ErrInvalidRemotePod) {
				t.Fatalf(
					"error = %v, want ErrInvalidRemotePod",
					err,
				)
			}
			if fake.deleteCalls != 0 {
				t.Fatal("invalid remote delete modified endpoint map")
			}
		})
	}
}

func TestRemotePodOperationsHonorCanceledContext(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := manager.UpsertRemotePod(ctx, testRemotePod())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"upsert error = %v, want context.Canceled",
			err,
		)
	}

	pod := testRemotePod()
	err = manager.DeleteRemotePod(ctx, pod.IP, pod.PodUID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"delete error = %v, want context.Canceled",
			err,
		)
	}

	if fake.upsertCalls != 0 ||
		fake.deleteCalls != 0 {
		t.Fatal("canceled remote operations modified datapath")
	}
}

func testRemotePod() RemotePod {
	return RemotePod{
		IP:       netip.MustParseAddr("10.244.2.10"),
		PodUID:   "remote-pod-a",
		TenantID: "tenant-a",
	}
}
