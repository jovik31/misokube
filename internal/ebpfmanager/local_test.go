package ebpfmanager

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github/setera/internal/podnetwork"
)

func TestAddLocalPod(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testLocalPod()

	if err := manager.AddLocalPod(context.Background(), pod); err != nil {
		t.Fatal(err)
	}

	if fake.attachCalls != 1 {
		t.Fatalf("attach calls = %d, want 1", fake.attachCalls)
	}
	if fake.upsertCalls != 1 {
		t.Fatalf("upsert calls = %d, want 1", fake.upsertCalls)
	}
	if fake.lastIfName != pod.HostVethName {
		t.Fatalf("attached interface = %q, want %q", fake.lastIfName, pod.HostVethName)
	}
	if fake.lastTenant != pod.TenantID {
		t.Fatalf("attached tenant = %q, want %q", fake.lastTenant, pod.TenantID)
	}
	if fake.lastIP != pod.IP {
		t.Fatalf("endpoint IP = %s, want %s", fake.lastIP, pod.IP)
	}
	if fake.lastIfIndex != pod.HostVethIfIndex {
		t.Fatalf("endpoint ifindex = %d, want %d", fake.lastIfIndex, pod.HostVethIfIndex)
	}

	state, ok := manager.local[pod.IP]
	if !ok {
		t.Fatal("local Pod state was not stored")
	}
	if state.pod.PodUID != pod.PodUID {
		t.Fatalf("stored pod UID = %q, want %q", state.pod.PodUID, pod.PodUID)
	}
}

func TestAddLocalPodDuplicateIsIdempotent(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testLocalPod()

	if err := manager.AddLocalPod(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	if err := manager.AddLocalPod(context.Background(), pod); err != nil {
		t.Fatal(err)
	}

	if fake.attachCalls != 1 {
		t.Fatalf("attach calls = %d, want 1", fake.attachCalls)
	}
	if fake.upsertCalls != 2 {
		t.Fatalf("upsert calls = %d, want 2", fake.upsertCalls)
	}
	if fake.program.closeCalls != 0 {
		t.Fatalf("program close calls = %d, want 0", fake.program.closeCalls)
	}
}

func TestAddLocalPodRejectsConflictingIP(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	first := testLocalPod()

	if err := manager.AddLocalPod(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	second := first
	second.PodUID = "pod-b"
	second.HostVethName = "veth5678"
	second.HostVethIfIndex = 77

	err := manager.AddLocalPod(context.Background(), second)
	if !errors.Is(err, ErrLocalPodConflict) {
		t.Fatalf("error = %v, want ErrLocalPodConflict", err)
	}
	if fake.attachCalls != 1 {
		t.Fatalf("attach calls = %d, want 1", fake.attachCalls)
	}
	if fake.upsertCalls != 1 {
		t.Fatalf("upsert calls = %d, want 1", fake.upsertCalls)
	}
}

func TestAddLocalPodRollsBackProgramWhenEndpointUpdateFails(t *testing.T) {
	fake := newFakeDependencies()
	fake.upsertErr = errors.New("map update failed")
	manager := newWithDependencies(fake.dependencies())
	pod := testLocalPod()

	err := manager.AddLocalPod(context.Background(), pod)
	if !errors.Is(err, fake.upsertErr) {
		t.Fatalf("error = %v, want wrapped %v", err, fake.upsertErr)
	}
	if fake.program.closeCalls != 1 {
		t.Fatalf("program close calls = %d, want 1", fake.program.closeCalls)
	}
	if _, ok := manager.local[pod.IP]; ok {
		t.Fatal("local Pod state stored after failed endpoint update")
	}
}

func TestAddLocalPodReportsRollbackFailure(t *testing.T) {
	fake := newFakeDependencies()
	fake.upsertErr = errors.New("map update failed")
	fake.program.closeErr = errors.New("detach failed")
	manager := newWithDependencies(fake.dependencies())

	err := manager.AddLocalPod(context.Background(), testLocalPod())
	if !errors.Is(err, fake.upsertErr) {
		t.Fatalf("error = %v, want map error", err)
	}
	if !errors.Is(err, fake.program.closeErr) {
		t.Fatalf("error = %v, want rollback error", err)
	}
}

func TestAddLocalPodDoesNotPublishWhenAttachFails(t *testing.T) {
	fake := newFakeDependencies()
	fake.attachErr = errors.New("attach failed")
	manager := newWithDependencies(fake.dependencies())

	err := manager.AddLocalPod(context.Background(), testLocalPod())
	if !errors.Is(err, fake.attachErr) {
		t.Fatalf("error = %v, want wrapped %v", err, fake.attachErr)
	}
	if fake.upsertCalls != 0 {
		t.Fatalf("upsert calls = %d, want 0", fake.upsertCalls)
	}
}

func TestDeleteLocalPod(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testLocalPod()

	if err := manager.AddLocalPod(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	fake.operations = nil

	if err := manager.DeleteLocalPod(context.Background(), pod.IP); err != nil {
		t.Fatal(err)
	}

	if fake.deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", fake.deleteCalls)
	}
	if fake.program.closeCalls != 1 {
		t.Fatalf("program close calls = %d, want 1", fake.program.closeCalls)
	}
	if len(fake.operations) != 2 || fake.operations[0] != "delete" || fake.operations[1] != "close" {
		t.Fatalf("operation order = %v, want [delete close]", fake.operations)
	}
	if _, ok := manager.local[pod.IP]; ok {
		t.Fatal("local Pod state still present after delete")
	}
}

func TestDeleteLocalPodWithoutRecordStillDeletesEndpoint(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	ip := netip.MustParseAddr("10.244.1.10")

	if err := manager.DeleteLocalPod(context.Background(), ip); err != nil {
		t.Fatal(err)
	}

	if fake.deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", fake.deleteCalls)
	}
	if fake.program.closeCalls != 0 {
		t.Fatalf("program close calls = %d, want 0", fake.program.closeCalls)
	}
}

func TestDeleteLocalPodKeepsStateWhenEndpointDeleteFails(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testLocalPod()

	if err := manager.AddLocalPod(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	fake.deleteErr = errors.New("map delete failed")

	err := manager.DeleteLocalPod(context.Background(), pod.IP)
	if !errors.Is(err, fake.deleteErr) {
		t.Fatalf("error = %v, want wrapped %v", err, fake.deleteErr)
	}
	if fake.program.closeCalls != 0 {
		t.Fatalf("program close calls = %d, want 0", fake.program.closeCalls)
	}
	if _, ok := manager.local[pod.IP]; !ok {
		t.Fatal("local Pod state removed after failed endpoint delete")
	}
}

func TestDeleteLocalPodRemovesStateWhenProgramCloseFails(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testLocalPod()

	if err := manager.AddLocalPod(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	fake.program.closeErr = errors.New("detach failed")

	err := manager.DeleteLocalPod(context.Background(), pod.IP)
	if !errors.Is(err, fake.program.closeErr) {
		t.Fatalf("error = %v, want wrapped %v", err, fake.program.closeErr)
	}
	if _, ok := manager.local[pod.IP]; ok {
		t.Fatal("local Pod state still present after failed program close")
	}

	// A later CNI DEL is allowed to converge after podnetwork has removed the
	// veth as part of the first failed cleanup attempt.
	fake.program.closeErr = nil
	if err := manager.DeleteLocalPod(context.Background(), pod.IP); err != nil {
		t.Fatal(err)
	}
}

func TestAddLocalPodRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		edit func(*podnetwork.LocalPod)
	}{
		{
			name: "invalid IP",
			edit: func(p *podnetwork.LocalPod) { p.IP = netip.Addr{} },
		},
		{
			name: "IPv6",
			edit: func(p *podnetwork.LocalPod) { p.IP = netip.MustParseAddr("2001:db8::1") },
		},
		{
			name: "missing pod UID",
			edit: func(p *podnetwork.LocalPod) { p.PodUID = "" },
		},
		{
			name: "missing tenant",
			edit: func(p *podnetwork.LocalPod) { p.TenantID = "" },
		},
		{
			name: "missing host veth",
			edit: func(p *podnetwork.LocalPod) { p.HostVethName = "" },
		},
		{
			name: "invalid host ifindex",
			edit: func(p *podnetwork.LocalPod) { p.HostVethIfIndex = 0 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeDependencies()
			manager := newWithDependencies(fake.dependencies())
			pod := testLocalPod()
			tt.edit(&pod)

			err := manager.AddLocalPod(context.Background(), pod)
			if !errors.Is(err, ErrInvalidLocalPod) {
				t.Fatalf("error = %v, want ErrInvalidLocalPod", err)
			}
			if fake.attachCalls != 0 || fake.upsertCalls != 0 {
				t.Fatalf("invalid input changed datapath: attach=%d upsert=%d", fake.attachCalls, fake.upsertCalls)
			}
		})
	}
}

func TestAddLocalPodHonorsCanceledContext(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := manager.AddLocalPod(ctx, testLocalPod())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if fake.attachCalls != 0 || fake.upsertCalls != 0 {
		t.Fatal("canceled ADD changed datapath")
	}
}

func testLocalPod() podnetwork.LocalPod {
	return podnetwork.LocalPod{
		IP:              netip.MustParseAddr("10.244.1.10"),
		PodUID:          "pod-a",
		TenantID:        "tenant-a",
		HostVethName:    "veth1234",
		HostVethIfIndex: 42,
	}
}

type fakeDependencies struct {
	program *fakePodProgram

	attachCalls int
	upsertCalls int
	deleteCalls int

	attachErr error
	upsertErr error
	deleteErr error

	lastIfName  string
	lastTenant  string
	lastIP      netip.Addr
	lastIfIndex int

	operations []string
}

func newFakeDependencies() *fakeDependencies {
	fake := &fakeDependencies{}
	fake.program = &fakePodProgram{owner: fake}
	return fake
}

func (f *fakeDependencies) dependencies() dependencies {
	return dependencies{
		attachPodProgram: func(ifName, tenant string) (podProgram, error) {
			f.attachCalls++
			f.lastIfName = ifName
			f.lastTenant = tenant
			f.operations = append(f.operations, "attach")
			if f.attachErr != nil {
				return nil, f.attachErr
			}
			return f.program, nil
		},
		upsertEndpoint: func(ip netip.Addr, tenant string, ifIndex int) error {
			f.upsertCalls++
			f.lastIP = ip
			f.lastTenant = tenant
			f.lastIfIndex = ifIndex
			f.operations = append(f.operations, "upsert")
			return f.upsertErr
		},
		deleteEndpoint: func(ip netip.Addr) error {
			f.deleteCalls++
			f.lastIP = ip
			f.operations = append(f.operations, "delete")
			return f.deleteErr
		},
	}
}

type fakePodProgram struct {
	owner      *fakeDependencies
	closeCalls int
	closeErr   error
}

func (f *fakePodProgram) Close() error {
	f.closeCalls++
	if f.owner != nil {
		f.owner.operations = append(f.owner.operations, "close")
	}
	return f.closeErr
}
