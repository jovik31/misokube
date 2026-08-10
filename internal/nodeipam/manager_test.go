package nodeipam

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"sync"
	"testing"

	"github/setera/pkg/ipam/bitmap"
	"github/setera/pkg/store"
)

func TestNewRejectsNilDependencies(t *testing.T) {
	st := newMemoryStore()

	if _, err := New(nil, st); !errors.Is(err, ErrInvalidDependency) {
		t.Fatalf("expected ErrInvalidDependency for allocator, got %v", err)
	}

	allocator, err := bitmap.New(netip.MustParsePrefix("10.244.0.0/29"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := New(allocator, nil); !errors.Is(err, ErrInvalidDependency) {
		t.Fatalf("expected ErrInvalidDependency for store, got %v", err)
	}
}

func TestRequestValidation(t *testing.T) {
	tests := []Request{
		{
			Owner: Owner{
				IfName: "eth0",
			},
			PodUID:   "pod-a",
			TenantID: "tenant-a",
		},
		{
			Owner: Owner{
				ContainerID: "container-a",
			},
			PodUID:   "pod-a",
			TenantID: "tenant-a",
		},
		{
			Owner: Owner{
				ContainerID: "container-a",
				IfName:      "eth0",
			},
			TenantID: "tenant-a",
		},
		{
			Owner: Owner{
				ContainerID: "container-a",
				IfName:      "eth0",
			},
			PodUID: "pod-a",
		},
	}

	for i, req := range tests {
		if err := req.validate(); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestManagerRequiresRestore(t *testing.T) {
	manager := newTestManager(t, newMemoryStore())

	_, err := manager.Allocate(
		context.Background(),
		testRequest("container-a"),
	)
	if !errors.Is(err, ErrNotRestored) {
		t.Fatalf("expected ErrNotRestored, got %v", err)
	}
}

func TestAllocateIsIdempotent(t *testing.T) {
	manager := newRestoredTestManager(t, newMemoryStore())
	req := testRequest("container-a")

	first, err := manager.Allocate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	second, err := manager.Allocate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatalf(
			"second ADD changed allocation: first=%+v second=%+v",
			first,
			second,
		)
	}

	if len(manager.List()) != 1 {
		t.Fatalf("got %d allocations, want 1", len(manager.List()))
	}
}

func TestAllocateRejectsOwnerConflict(t *testing.T) {
	manager := newRestoredTestManager(t, newMemoryStore())
	req := testRequest("container-a")

	if _, err := manager.Allocate(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	req.TenantID = "tenant-b"

	_, err := manager.Allocate(context.Background(), req)
	if !errors.Is(err, ErrOwnerConflict) {
		t.Fatalf("expected ErrOwnerConflict, got %v", err)
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	manager := newRestoredTestManager(t, newMemoryStore())
	req := testRequest("container-a")

	allocation, err := manager.Allocate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Release(context.Background(), req.Owner); err != nil {
		t.Fatal(err)
	}

	if err := manager.Release(context.Background(), req.Owner); err != nil {
		t.Fatalf("second release failed: %v", err)
	}

	if _, ok := manager.Get(req.Owner); ok {
		t.Fatal("released owner is still present")
	}

	second, err := manager.Allocate(
		context.Background(),
		testRequest("container-b"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if second.IP != allocation.IP {
		t.Fatalf(
			"got %s after release, want reused %s",
			second.IP,
			allocation.IP,
		)
	}
}

func TestAllocateRollsBackWhenStoreFails(t *testing.T) {
	st := newMemoryStore()
	manager := newRestoredTestManager(t, st)

	st.failUpdate = true

	_, err := manager.Allocate(
		context.Background(),
		testRequest("container-a"),
	)
	if err == nil {
		t.Fatal("expected allocation error")
	}

	if len(manager.List()) != 0 {
		t.Fatal("failed allocation remained in memory")
	}

	st.failUpdate = false

	allocation, err := manager.Allocate(
		context.Background(),
		testRequest("container-b"),
	)
	if err != nil {
		t.Fatal(err)
	}

	want := netip.MustParseAddr("10.244.0.0")

	if allocation.IP != want {
		t.Fatalf("got %s, want %s", allocation.IP, want)
	}
}

func TestReleaseRollsBackWhenStoreFails(t *testing.T) {
	st := newMemoryStore()
	manager := newRestoredTestManager(t, st)
	req := testRequest("container-a")

	allocation, err := manager.Allocate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	st.failUpdate = true

	if err := manager.Release(context.Background(), req.Owner); err == nil {
		t.Fatal("expected release error")
	}

	got, ok := manager.Get(req.Owner)
	if !ok {
		t.Fatal("allocation was not restored")
	}

	if got != allocation {
		t.Fatalf(
			"allocation changed after rollback: got=%+v want=%+v",
			got,
			allocation,
		)
	}
}

func TestRestoreIsIdempotent(t *testing.T) {
	manager := newTestManager(t, newMemoryStore())

	if err := manager.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := manager.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRollsBackOnAllocatorConflict(t *testing.T) {
	st := newMemoryStore()
	ctx := context.Background()

	first := Allocation{
		IP: netip.MustParseAddr("10.244.0.0"),
		Owner: Owner{
			ContainerID: "container-a",
			IfName:      "eth0",
		},
		PodUID:   "pod-a",
		TenantID: "tenant-a",
	}

	second := Allocation{
		IP: netip.MustParseAddr("10.244.0.1"),
		Owner: Owner{
			ContainerID: "container-b",
			IfName:      "eth0",
		},
		PodUID:   "pod-b",
		TenantID: "tenant-a",
	}

	repo := newRepository(st)

	if err := repo.put(ctx, first); err != nil {
		t.Fatal(err)
	}

	if err := repo.put(ctx, second); err != nil {
		t.Fatal(err)
	}

	allocator, err := bitmap.New(
		netip.MustParsePrefix("10.244.0.0/29"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := allocator.AllocateSpecific(second.IP); err != nil {
		t.Fatal(err)
	}

	manager, err := New(allocator, st)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Restore(ctx); err == nil {
		t.Fatal("expected restore error")
	}

	allocated, err := allocator.IsAllocated(first.IP)
	if err != nil {
		t.Fatal(err)
	}

	if allocated {
		t.Fatalf("address %s was not rolled back", first.IP)
	}
}

func TestRestoreRejectsCorruptRecord(t *testing.T) {
	st := newMemoryStore()

	key, err := allocationKey(
		netip.MustParseAddr("10.244.0.2"),
	)
	if err != nil {
		t.Fatal(err)
	}

	st.data[string(key)] = []byte(
		`{"version":99,"container_id":"c","if_name":"eth0","pod_uid":"p","tenant_id":"t"}`,
	)

	manager := newTestManager(t, st)

	if err := manager.Restore(context.Background()); !errors.Is(err, ErrCorruptState) {
		t.Fatalf("expected ErrCorruptState, got %v", err)
	}
}

func TestRestoreRebuildsState(t *testing.T) {
	st := newMemoryStore()

	first := newRestoredTestManager(t, st)
	req := testRequest("container-a")

	want, err := first.Allocate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	second := newTestManager(t, st)

	if err := second.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}

	got, ok := second.Get(req.Owner)
	if !ok {
		t.Fatal("restored allocation not found")
	}

	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	next, err := second.Allocate(
		context.Background(),
		testRequest("container-b"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if next.IP == want.IP {
		t.Fatalf("restore reused live address %s", want.IP)
	}
}

func TestConcurrentAllocateReturnsUniqueAddresses(t *testing.T) {
	manager := newRestoredTestManagerWithPrefix(
		t,
		newMemoryStore(),
		"10.244.0.0/24",
	)

	const workers = 64

	results := make(chan Allocation, workers)
	errs := make(chan error, workers)

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()

			allocation, err := manager.Allocate(
				context.Background(),
				testRequest(fmt.Sprintf("container-%d", i)),
			)
			if err != nil {
				errs <- err
				return
			}

			results <- allocation
		}(i)
	}

	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("allocate failed: %v", err)
	}

	seen := make(map[netip.Addr]struct{}, workers)

	for allocation := range results {
		if _, ok := seen[allocation.IP]; ok {
			t.Fatalf("duplicate address %s", allocation.IP)
		}

		seen[allocation.IP] = struct{}{}
	}

	if len(seen) != workers {
		t.Fatalf(
			"got %d addresses, want %d",
			len(seen),
			workers,
		)
	}
}

func newTestManager(
	t *testing.T,
	st storage,
) *Manager {
	t.Helper()

	return newTestManagerWithPrefix(
		t,
		st,
		"10.244.0.0/29",
	)
}

func newRestoredTestManager(
	t *testing.T,
	st storage,
) *Manager {
	t.Helper()

	return newRestoredTestManagerWithPrefix(
		t,
		st,
		"10.244.0.0/29",
	)
}

func newTestManagerWithPrefix(
	t *testing.T,
	st storage,
	prefix string,
) *Manager {
	t.Helper()

	allocator, err := bitmap.New(
		netip.MustParsePrefix(prefix),
	)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := New(allocator, st)
	if err != nil {
		t.Fatal(err)
	}

	return manager
}

func newRestoredTestManagerWithPrefix(
	t *testing.T,
	st storage,
	prefix string,
) *Manager {
	t.Helper()

	manager := newTestManagerWithPrefix(
		t,
		st,
		prefix,
	)

	if err := manager.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}

	return manager
}

func testRequest(containerID string) Request {
	return Request{
		Owner: Owner{
			ContainerID: containerID,
			IfName:      "eth0",
		},
		PodUID:   "pod-" + containerID,
		TenantID: "tenant-a",
	}
}

type memoryStore struct {
	mu sync.RWMutex

	data map[string][]byte

	failUpdate bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		data: make(map[string][]byte),
	}
}

func (s *memoryStore) View(
	ctx context.Context,
	fn func(store.ReadTx) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return fn(memoryReadTx{
		data: s.data,
	})
}

func (s *memoryStore) Update(
	ctx context.Context,
	fn func(store.WriteTx) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	staged := cloneData(s.data)

	tx := memoryWriteTx{
		memoryReadTx: memoryReadTx{
			data: staged,
		},
	}

	if err := fn(&tx); err != nil {
		return err
	}

	if s.failUpdate {
		return errors.New("memory store: update failed")
	}

	s.data = staged

	return nil
}

type memoryReadTx struct {
	data map[string][]byte
}

func (tx memoryReadTx) Get(
	key []byte,
) ([]byte, error) {
	value, ok := tx.data[string(key)]
	if !ok {
		return nil, store.ErrNotFound
	}

	return bytes.Clone(value), nil
}

func (tx memoryReadTx) Iterate(
	prefix []byte,
	fn func(key, value []byte) error,
) error {
	keys := make([]string, 0, len(tx.data))

	for key := range tx.data {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		keyBytes := []byte(key)

		if !bytes.HasPrefix(keyBytes, prefix) {
			continue
		}

		if err := fn(
			bytes.Clone(keyBytes),
			bytes.Clone(tx.data[key]),
		); err != nil {
			return err
		}
	}

	return nil
}

type memoryWriteTx struct {
	memoryReadTx
}

func (tx *memoryWriteTx) Put(
	key,
	value []byte,
) error {
	if len(key) == 0 {
		return store.ErrInvalidKey
	}

	tx.data[string(key)] = bytes.Clone(value)

	return nil
}

func (tx *memoryWriteTx) Delete(
	key []byte,
) error {
	if len(key) == 0 {
		return store.ErrInvalidKey
	}

	delete(tx.data, string(key))

	return nil
}

func cloneData(
	in map[string][]byte,
) map[string][]byte {
	out := make(map[string][]byte, len(in))

	for key, value := range in {
		out[key] = bytes.Clone(value)
	}

	return out
}
