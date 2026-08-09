package ipam

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github/setera/pkg/network/ipam/store"
	filestore "github/setera/pkg/network/ipam/store/file"
)

var testTime = time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)

func TestNewBitmapNodeIPAM_Errors(t *testing.T) {
	cases := []struct {
		label  string
		subnet *net.IPNet
		store  store.Store
		want   error
	}{
		{"nil subnet", nil, newFileStore(t, t.TempDir()), ErrNilSubnet{}},
		{"nil store", mustCIDR(t, "10.0.0.0/24"), nil, ErrNilStore{}},
		{"ipv6 subnet", mustCIDR(t, "2001:db8::/64"), newFileStore(t, t.TempDir()), ErrIPv4OnlySupported{MaskSize: 128}},
		{"/32", mustCIDR(t, "10.0.0.0/32"), newFileStore(t, t.TempDir()), ErrSubnetTooSmall{Subnet: "10.0.0.0/32"}},
		{"/31", mustCIDR(t, "10.0.0.0/31"), newFileStore(t, t.TempDir()), ErrSubnetTooSmall{Subnet: "10.0.0.0/31"}},
		{"/8 is too large", mustCIDR(t, "10.0.0.0/8"), newFileStore(t, t.TempDir()), ErrSubnetTooLarge{Subnet: "10.0.0.0/8", Max: maxAddresses}},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, err := NewBitmapNodeIPAM(context.Background(), tc.subnet, tc.store)
			if !errors.Is(err, tc.want) {
				t.Errorf("want error %v, got %v", tc.want, err)
			}
		})
	}
}

// The node IPAM keeps back the network address, the gateway address, and the
// broadcast address.
func TestCapacity(t *testing.T) {
	cases := []struct {
		cidr string
		want int
	}{
		{"10.0.0.0/30", 1},
		{"10.0.0.0/28", 13},
		{"10.0.0.0/24", 253},
		{"10.244.1.0/24", 253},
	}

	for _, tc := range cases {
		t.Run(tc.cidr, func(t *testing.T) {
			ipam := newIPAM(t, tc.cidr, t.TempDir())
			if got := ipam.Capacity(); got != tc.want {
				t.Errorf("Capacity: want %d, got %d", tc.want, got)
			}
			if got := ipam.Remaining(); got != tc.want {
				t.Errorf("Remaining on a new IPAM: want %d, got %d", tc.want, got)
			}
		})
	}
}

// The first address must sit after the network address and the gateway.
func TestAllocate_StartsAfterTheReservedAddresses(t *testing.T) {
	ipam := newIPAM(t, "10.244.1.0/24", t.TempDir())

	alloc := mustAllocate(t, ipam, request("container-a", "tenant-a", "ns-a", "pod-a"))

	if !alloc.IP.Equal(net.ParseIP("10.244.1.2")) {
		t.Errorf("first address: want 10.244.1.2, got %s", alloc.IP)
	}
	if alloc.Ifindex != -1 {
		t.Errorf("Ifindex on a new allocation: want -1, got %d", alloc.Ifindex)
	}
	if !alloc.AllocatedAt.Equal(testTime) {
		t.Errorf("AllocatedAt: want %s, got %s", testTime, alloc.AllocatedAt)
	}
	if got := ipam.Remaining(); got != 252 {
		t.Errorf("Remaining after one allocation: want 252, got %d", got)
	}
}

// The CNI repeats an ADD after a timeout. A repeat must not take a second
// address.
func TestAllocate_IsIdempotent(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/24", t.TempDir())
	req := request("container-a", "tenant-a", "ns-a", "pod-a")

	first := mustAllocate(t, ipam, req)
	second := mustAllocate(t, ipam, req)

	if !first.IP.Equal(second.IP) {
		t.Errorf("a repeated ADD changed the address: %s then %s", first.IP, second.IP)
	}
	if got := ipam.Remaining(); got != 252 {
		t.Errorf("a repeated ADD took a second address, Remaining is %d", got)
	}
}

func TestAllocate_RejectsIncompleteRequests(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/24", t.TempDir())

	cases := []struct {
		label string
		req   AllocationRequest
		want  error
	}{
		{"no container ID", AllocationRequest{TenantID: "t", PodName: "p"}, ErrEmptyContainerID{}},
		{"no tenant ID", AllocationRequest{ContainerID: "c", PodName: "p"}, ErrEmptyTenantID{ContainerID: "c"}},
		{"no pod name", AllocationRequest{ContainerID: "c", TenantID: "t"}, ErrEmptyPodName{ContainerID: "c"}},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			if _, err := ipam.Allocate(context.Background(), tc.req); !errors.Is(err, tc.want) {
				t.Errorf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestAllocate_ReportsExhaustion(t *testing.T) {
	// A /30 holds one pod address.
	ipam := newIPAM(t, "10.0.0.0/30", t.TempDir())

	mustAllocate(t, ipam, request("container-a", "tenant-a", "ns-a", "pod-a"))

	_, err := ipam.Allocate(context.Background(), request("container-b", "tenant-a", "ns-a", "pod-b"))
	var exhausted ErrNoAvailableIPs
	if !errors.As(err, &exhausted) {
		t.Errorf("want ErrNoAvailableIPs, got %v", err)
	}
	if got := ipam.Remaining(); got != 0 {
		t.Errorf("Remaining on a full range: want 0, got %d", got)
	}
}

// A freed address must not go straight back out. An immediate reuse can meet a
// stale ARP or conntrack entry for the pod that just left.
func TestAllocate_DoesNotReuseAFreedAddressImmediately(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/29", t.TempDir()) // five pod addresses: .2 to .6
	ctx := context.Background()

	a := mustAllocate(t, ipam, request("container-a", "tenant-a", "ns", "pod-a"))
	b := mustAllocate(t, ipam, request("container-b", "tenant-a", "ns", "pod-b"))
	mustAllocate(t, ipam, request("container-c", "tenant-a", "ns", "pod-c"))

	if err := ipam.Release(ctx, "container-b"); err != nil {
		t.Fatalf("release container-b: %v", err)
	}

	d := mustAllocate(t, ipam, request("container-d", "tenant-a", "ns", "pod-d"))
	if d.IP.Equal(b.IP) {
		t.Errorf("the address %s of container-b went straight back out to container-d", b.IP)
	}

	// The freed address must come back once the search wraps, so that nothing
	// is lost.
	mustAllocate(t, ipam, request("container-e", "tenant-a", "ns", "pod-e"))
	f := mustAllocate(t, ipam, request("container-f", "tenant-a", "ns", "pod-f"))
	if !f.IP.Equal(b.IP) {
		t.Errorf("after a wrap the freed address %s must come back, got %s", b.IP, f.IP)
	}
	if a.IP.Equal(f.IP) {
		t.Errorf("container-f took the address of the live container-a")
	}
}

func TestRelease_FreesTheAddress(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/24", t.TempDir())
	ctx := context.Background()

	mustAllocate(t, ipam, request("container-a", "tenant-a", "ns-a", "pod-a"))
	if err := ipam.Release(ctx, "container-a"); err != nil {
		t.Fatalf("release: %v", err)
	}

	if got := ipam.Remaining(); got != 253 {
		t.Errorf("Remaining after a release: want 253, got %d", got)
	}
	if _, ok := ipam.Get("container-a"); ok {
		t.Error("Get still returns a released allocation")
	}
	if _, ok := ipam.GetByPod("ns-a", "pod-a"); ok {
		t.Error("GetByPod still returns a released allocation")
	}
}

// The CNI calls DEL more than one time, and for pods that never finished ADD.
func TestRelease_IsIdempotent(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/24", t.TempDir())
	ctx := context.Background()

	mustAllocate(t, ipam, request("container-a", "tenant-a", "ns-a", "pod-a"))

	if err := ipam.Release(ctx, "container-a"); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := ipam.Release(ctx, "container-a"); err != nil {
		t.Errorf("second release must succeed, got %v", err)
	}
	if err := ipam.Release(ctx, "container-never-seen"); err != nil {
		t.Errorf("release of an unknown container must succeed, got %v", err)
	}
	if err := ipam.Release(ctx, ""); !errors.Is(err, ErrEmptyContainerID{}) {
		t.Errorf("want ErrEmptyContainerID, got %v", err)
	}
}

// A pod sandbox restart gives the pod a new container ID. A late DEL for the
// old sandbox must not take the entry of the new one.
func TestRelease_LateDeleteDoesNotTakeTheNewSandbox(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/24", t.TempDir())
	ctx := context.Background()

	mustAllocate(t, ipam, request("container-old", "tenant-a", "ns-a", "pod-a"))
	fresh := mustAllocate(t, ipam, request("container-new", "tenant-a", "ns-a", "pod-a"))

	// The pod now points at the new sandbox.
	byPod, ok := ipam.GetByPod("ns-a", "pod-a")
	if !ok {
		t.Fatal("GetByPod lost the pod after a sandbox restart")
	}
	if !byPod.IP.Equal(fresh.IP) {
		t.Errorf("GetByPod: want the new address %s, got %s", fresh.IP, byPod.IP)
	}

	// The late DEL for the old sandbox arrives.
	if err := ipam.Release(ctx, "container-old"); err != nil {
		t.Fatalf("release the old sandbox: %v", err)
	}

	byPod, ok = ipam.GetByPod("ns-a", "pod-a")
	if !ok {
		t.Fatal("the late delete of the old sandbox took the pod index entry of the new one")
	}
	if !byPod.IP.Equal(fresh.IP) {
		t.Errorf("GetByPod after the late delete: want %s, got %s", fresh.IP, byPod.IP)
	}
	if _, ok := ipam.Get("container-new"); !ok {
		t.Error("the late delete removed the allocation of the new sandbox")
	}
}

func TestSetHostVeth(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/24", t.TempDir())
	ctx := context.Background()

	mustAllocate(t, ipam, request("container-a", "tenant-a", "ns-a", "pod-a"))
	if err := ipam.SetHostVeth(ctx, "container-a", "veth1a2b3c4d", 42); err != nil {
		t.Fatalf("SetHostVeth: %v", err)
	}

	got, ok := ipam.Get("container-a")
	if !ok {
		t.Fatal("allocation is missing")
	}
	if got.HostVethName != "veth1a2b3c4d" {
		t.Errorf("HostVethName: want veth1a2b3c4d, got %q", got.HostVethName)
	}
	if got.Ifindex != 42 {
		t.Errorf("Ifindex: want 42, got %d", got.Ifindex)
	}
}

func TestSetHostVeth_Errors(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/24", t.TempDir())
	ctx := context.Background()

	err := ipam.SetHostVeth(ctx, "container-never-seen", "veth0", 1)
	if !errors.Is(err, ErrAllocationNotFound{ContainerID: "container-never-seen"}) {
		t.Errorf("want ErrAllocationNotFound, got %v", err)
	}
	if err := ipam.SetHostVeth(ctx, "", "veth0", 1); !errors.Is(err, ErrEmptyContainerID{}) {
		t.Errorf("want ErrEmptyContainerID, got %v", err)
	}
}

func TestListByTenant(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/24", t.TempDir())

	mustAllocate(t, ipam, request("container-a", "tenant-a", "ns", "pod-a"))
	mustAllocate(t, ipam, request("container-b", "tenant-a", "ns", "pod-b"))
	mustAllocate(t, ipam, request("container-c", "tenant-b", "ns", "pod-c"))

	if got := len(ipam.List()); got != 3 {
		t.Errorf("List: want 3 allocations, got %d", got)
	}

	tenantA := ipam.ListByTenant("tenant-a")
	if len(tenantA) != 2 {
		t.Errorf("ListByTenant(tenant-a): want 2, got %d", len(tenantA))
	}
	for containerID, alloc := range tenantA {
		if alloc.TenantID != "tenant-a" {
			t.Errorf("%s belongs to tenant %q, not tenant-a", containerID, alloc.TenantID)
		}
	}
	if got := len(ipam.ListByTenant("tenant-c")); got != 0 {
		t.Errorf("ListByTenant on an unknown tenant: want 0, got %d", got)
	}
}

// A caller must not be able to change the IPAM state by writing to a result.
func TestReadsReturnCopies(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/24", t.TempDir())
	mustAllocate(t, ipam, request("container-a", "tenant-a", "ns-a", "pod-a"))

	got, _ := ipam.Get("container-a")
	original := append(net.IP(nil), got.IP...)

	// The address is a slice. A shallow copy would let this write reach the
	// IPAM state.
	got.IP[3] = 99
	got.TenantID = "attacker"
	got.HostVethName = "changed"

	again, _ := ipam.Get("container-a")
	if !again.IP.Equal(original) {
		t.Errorf("writing to a result changed the stored address: want %s, got %s", original, again.IP)
	}
	if again.TenantID != "tenant-a" {
		t.Errorf("writing to a result changed the tenant: got %q", again.TenantID)
	}
	if again.HostVethName != "" {
		t.Errorf("writing to a result changed the veth name: got %q", again.HostVethName)
	}

	for _, alloc := range ipam.List() {
		alloc.TenantID = "attacker"
	}
	if listed, _ := ipam.Get("container-a"); listed.TenantID != "tenant-a" {
		t.Errorf("writing to a List result changed the stored tenant: got %q", listed.TenantID)
	}
}

func TestSubnet_ReturnsACopy(t *testing.T) {
	ipam := newIPAM(t, "10.244.1.0/24", t.TempDir())

	got := ipam.Subnet()
	if got.String() != "10.244.1.0/24" {
		t.Fatalf("Subnet: want 10.244.1.0/24, got %s", got)
	}
	got.IP[0] = 99

	if again := ipam.Subnet(); again.String() != "10.244.1.0/24" {
		t.Errorf("writing to a Subnet result changed the range: got %s", again)
	}
}

// The whole point of the store. A restart must not lose an address that a live
// pod holds.
func TestRestart_RecoversAllocations(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	first := newIPAM(t, "10.0.0.0/24", dir)
	a := mustAllocate(t, first, request("container-a", "tenant-a", "ns-a", "pod-a"))
	b := mustAllocate(t, first, request("container-b", "tenant-b", "ns-b", "pod-b"))
	if err := first.SetHostVeth(ctx, "container-a", "vethaaaa", 7); err != nil {
		t.Fatalf("SetHostVeth: %v", err)
	}
	if err := first.Release(ctx, "container-b"); err != nil {
		t.Fatalf("release container-b: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := newIPAM(t, "10.0.0.0/24", dir)

	got, ok := second.Get("container-a")
	if !ok {
		t.Fatal("container-a lost its allocation across a restart")
	}
	if !got.IP.Equal(a.IP) {
		t.Errorf("address after a restart: want %s, got %s", a.IP, got.IP)
	}
	if got.HostVethName != "vethaaaa" || got.Ifindex != 7 {
		t.Errorf("veth details did not survive the restart: %q index %d", got.HostVethName, got.Ifindex)
	}
	if got.TenantID != "tenant-a" {
		t.Errorf("tenant did not survive the restart: got %q", got.TenantID)
	}
	if _, ok := second.Get("container-b"); ok {
		t.Error("the released container-b came back after a restart")
	}
	if _, ok := second.GetByPod("ns-a", "pod-a"); !ok {
		t.Error("the pod index was not rebuilt after a restart")
	}
	if got := second.Remaining(); got != 252 {
		t.Errorf("Remaining after a restart: want 252, got %d", got)
	}

	// The recovered address must stay taken.
	next := mustAllocate(t, second, request("container-c", "tenant-a", "ns-a", "pod-c"))
	if next.IP.Equal(a.IP) {
		t.Errorf("a new pod took the address %s of the recovered container-a", a.IP)
	}
	if !next.IP.Equal(b.IP) {
		t.Logf("note: the new pod took %s, the freed address of container-b", next.IP)
	}
}

func TestRestart_RejectsStateFromAnotherRange(t *testing.T) {
	dir := t.TempDir()

	first := newIPAM(t, "10.0.0.0/24", dir)
	mustAllocate(t, first, request("container-a", "tenant-a", "ns-a", "pod-a"))
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The node PodCIDR changed under a running node.
	_, err := NewBitmapNodeIPAM(context.Background(), mustCIDR(t, "10.99.0.0/24"), newFileStore(t, dir))
	var outOfRange ErrStoredIPOutOfRange
	if !errors.As(err, &outOfRange) {
		t.Fatalf("want ErrStoredIPOutOfRange, got %v", err)
	}
}

func TestRestart_RejectsTwoAllocationsOnOneAddress(t *testing.T) {
	dir := t.TempDir()
	st := newFileStore(t, dir)
	ctx := context.Background()

	alloc := store.Allocation{
		TenantID: "tenant-a", Namespace: "ns", PodName: "pod-a",
		IP: net.ParseIP("10.0.0.5"), Ifindex: -1, AllocatedAt: testTime,
	}
	for _, key := range []string{"container-a", "container-b"} {
		copied := alloc.Clone()
		copied.PodName = key
		if err := st.Apply(ctx, store.Record{Seq: 1, Op: store.OpPut, Key: key, Alloc: &copied}); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	_, err := NewBitmapNodeIPAM(ctx, mustCIDR(t, "10.0.0.0/24"), newFileStore(t, dir))
	var conflict ErrStoredIPConflict
	if !errors.As(err, &conflict) {
		t.Errorf("want ErrStoredIPConflict, got %v", err)
	}
}

// A store outage must not leak an address.
func TestAllocate_RollsBackWhenTheStoreFails(t *testing.T) {
	failing := &flakyStore{}
	ipam, err := NewBitmapNodeIPAM(context.Background(), mustCIDR(t, "10.0.0.0/24"), failing)
	if err != nil {
		t.Fatalf("new ipam: %v", err)
	}
	ipam.now = func() time.Time { return testTime }

	before := ipam.Remaining()
	failing.fail = true

	if _, err := ipam.Allocate(context.Background(), request("container-a", "tenant-a", "ns", "pod-a")); err == nil {
		t.Fatal("expected the allocation to fail while the store is down")
	}
	if got := ipam.Remaining(); got != before {
		t.Errorf("a failed write leaked an address: Remaining went from %d to %d", before, got)
	}
	if _, ok := ipam.Get("container-a"); ok {
		t.Error("a failed write left an allocation in memory")
	}

	// The address must still be available once the store recovers.
	failing.fail = false
	if _, err := ipam.Allocate(context.Background(), request("container-a", "tenant-a", "ns", "pod-a")); err != nil {
		t.Errorf("allocation after the store recovered: %v", err)
	}
}

// A failed delete must keep the address. Losing it here would hand a live
// address to a second pod.
func TestRelease_KeepsTheAddressWhenTheStoreFails(t *testing.T) {
	failing := &flakyStore{}
	ipam, err := NewBitmapNodeIPAM(context.Background(), mustCIDR(t, "10.0.0.0/24"), failing)
	if err != nil {
		t.Fatalf("new ipam: %v", err)
	}
	ipam.now = func() time.Time { return testTime }
	ctx := context.Background()

	mustAllocate(t, ipam, request("container-a", "tenant-a", "ns", "pod-a"))
	after := ipam.Remaining()

	failing.fail = true
	if err := ipam.Release(ctx, "container-a"); err == nil {
		t.Fatal("expected the release to fail while the store is down")
	}
	if got := ipam.Remaining(); got != after {
		t.Errorf("a failed delete freed the address anyway: Remaining went from %d to %d", after, got)
	}
	if _, ok := ipam.Get("container-a"); !ok {
		t.Error("a failed delete dropped the allocation from memory")
	}
}

// Parallel CNI ADDs must never share an address.
func TestAllocate_IsSafeUnderConcurrency(t *testing.T) {
	ipam := newIPAM(t, "10.0.0.0/24", t.TempDir())
	const workers = 100

	var wg sync.WaitGroup
	results := make([]*Allocation, workers)
	errs := make([]error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := request(
				fmt.Sprintf("container-%03d", i),
				"tenant-a",
				"ns",
				fmt.Sprintf("pod-%03d", i),
			)
			results[i], errs[i] = ipam.Allocate(context.Background(), req)
		}(i)
	}
	wg.Wait()

	seen := make(map[string]int, workers)
	for i, alloc := range results {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		key := alloc.IP.String()
		if first, clash := seen[key]; clash {
			t.Fatalf("workers %d and %d both got the address %s", first, i, key)
		}
		seen[key] = i
	}
	if got := ipam.Remaining(); got != 253-workers {
		t.Errorf("Remaining: want %d, got %d", 253-workers, got)
	}
}

// helpers

// flakyStore is an in-memory store that can be told to fail every write, so
// that the tests can check the rollback paths.
type flakyStore struct {
	fail    bool
	records map[string]store.Allocation
}

func (f *flakyStore) Load(context.Context) (store.State, error) {
	out := store.State{Allocations: make(map[string]store.Allocation, len(f.records))}
	for k, v := range f.records {
		out.Allocations[k] = v
	}
	return out, nil
}

func (f *flakyStore) Apply(_ context.Context, rec store.Record) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	if f.fail {
		return errors.New("flaky store: disk is unavailable")
	}
	if f.records == nil {
		f.records = make(map[string]store.Allocation)
	}
	switch rec.Op {
	case store.OpPut:
		f.records[rec.Key] = rec.Alloc.Clone()
	case store.OpDelete:
		delete(f.records, rec.Key)
	}
	return nil
}

func (f *flakyStore) Close() error { return nil }

func newFileStore(t *testing.T, dir string) store.Store {
	t.Helper()
	s, err := filestore.New(dir)
	if err != nil {
		t.Fatalf("open file store at %s: %v", dir, err)
	}
	return s
}

func newIPAM(t *testing.T, cidr, dir string) *BitmapNodeIPAM {
	t.Helper()
	ipam, err := NewBitmapNodeIPAM(context.Background(), mustCIDR(t, cidr), newFileStore(t, dir))
	if err != nil {
		t.Fatalf("new ipam over %s: %v", cidr, err)
	}
	ipam.now = func() time.Time { return testTime }
	t.Cleanup(func() { _ = ipam.Close() })
	return ipam
}

func request(containerID, tenantID, namespace, podName string) AllocationRequest {
	return AllocationRequest{
		ContainerID: containerID,
		TenantID:    tenantID,
		Namespace:   namespace,
		PodName:     podName,
		IFName:      "eth0",
		NetNS:       "/var/run/netns/" + podName,
	}
}

func mustAllocate(t *testing.T, ipam *BitmapNodeIPAM, req AllocationRequest) *Allocation {
	t.Helper()
	alloc, err := ipam.Allocate(context.Background(), req)
	if err != nil {
		t.Fatalf("allocate %s: %v", req.ContainerID, err)
	}
	return alloc
}
