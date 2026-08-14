package bitmap

import (
	"errors"
	"net/netip"
	"sync"
	"testing"

	"github/setera/pkg/ipam"
)

func mustPrefix(t *testing.T, value string) netip.Prefix {
	t.Helper()
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		t.Fatal(err)
	}
	return prefix
}

func mustAddr(t *testing.T, value string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestNewRejectsInvalidPrefix(t *testing.T) {
	_, err := New(netip.Prefix{})
	if !errors.Is(err, ipam.ErrInvalidPrefix) {
		t.Fatalf("expected ErrInvalidPrefix, got %v", err)
	}
}

func TestNewRejectsPoolAboveLimit(t *testing.T) {
	_, err := New(mustPrefix(t, "10.0.0.0/16"), WithMaxAddresses(256))
	if !errors.Is(err, ipam.ErrPoolTooLarge) {
		t.Fatalf("expected ErrPoolTooLarge, got %v", err)
	}
}

func TestAllocateAndRelease(t *testing.T) {
	a, err := New(mustPrefix(t, "10.0.0.0/30"))
	if err != nil {
		t.Fatal(err)
	}

	first, err := a.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if first != mustAddr(t, "10.0.0.0") {
		t.Fatalf("unexpected first address: %s", first)
	}

	allocated, err := a.IsAllocated(first)
	if err != nil {
		t.Fatal(err)
	}
	if !allocated {
		t.Fatal("expected address to be allocated")
	}

	if err := a.Release(first); err != nil {
		t.Fatal(err)
	}
	if err := a.Release(first); err != nil {
		t.Fatalf("release must be idempotent: %v", err)
	}

	allocated, err = a.IsAllocated(first)
	if err != nil {
		t.Fatal(err)
	}
	if allocated {
		t.Fatal("expected address to be free")
	}
}

func TestAllocateSpecific(t *testing.T) {
	a, err := New(mustPrefix(t, "10.0.0.0/29"))
	if err != nil {
		t.Fatal(err)
	}

	addr := mustAddr(t, "10.0.0.5")
	if err := a.AllocateSpecific(addr); err != nil {
		t.Fatal(err)
	}
	if err := a.AllocateSpecific(addr); !errors.Is(err, ipam.ErrAllocated) {
		t.Fatalf("expected ErrAllocated, got %v", err)
	}
}

func TestOutOfRange(t *testing.T) {
	a, err := New(mustPrefix(t, "10.0.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}

	addr := mustAddr(t, "10.0.1.1")
	if err := a.AllocateSpecific(addr); !errors.Is(err, ipam.ErrOutOfRange) {
		t.Fatalf("expected ErrOutOfRange, got %v", err)
	}
	if err := a.Release(addr); !errors.Is(err, ipam.ErrOutOfRange) {
		t.Fatalf("expected ErrOutOfRange, got %v", err)
	}
	if _, err := a.IsAllocated(addr); !errors.Is(err, ipam.ErrOutOfRange) {
		t.Fatalf("expected ErrOutOfRange, got %v", err)
	}
}

func TestReservedAddresses(t *testing.T) {
	reserved := mustAddr(t, "10.0.0.0")
	a, err := New(
		mustPrefix(t, "10.0.0.0/30"),
		WithReserved(reserved),
	)
	if err != nil {
		t.Fatal(err)
	}

	addr, err := a.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if addr == reserved {
		t.Fatalf("allocated reserved address %s", addr)
	}

	if err := a.AllocateSpecific(reserved); !errors.Is(err, ipam.ErrReserved) {
		t.Fatalf("expected ErrReserved, got %v", err)
	}
	if err := a.Release(reserved); !errors.Is(err, ipam.ErrReserved) {
		t.Fatalf("expected ErrReserved, got %v", err)
	}

	stats := a.Stats()
	if stats.Capacity != 3 || stats.Allocated != 1 || stats.Available != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestExhaustion(t *testing.T) {
	a, err := New(mustPrefix(t, "10.0.0.0/31"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Allocate(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Allocate(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Allocate(); !errors.Is(err, ipam.ErrExhausted) {
		t.Fatalf("expected ErrExhausted, got %v", err)
	}
}

func TestIPv6(t *testing.T) {
	a, err := New(mustPrefix(t, "fd00::/126"))
	if err != nil {
		t.Fatal(err)
	}

	addr := mustAddr(t, "fd00::2")
	if err := a.AllocateSpecific(addr); err != nil {
		t.Fatal(err)
	}
	allocated, err := a.IsAllocated(addr)
	if err != nil {
		t.Fatal(err)
	}
	if !allocated {
		t.Fatal("expected IPv6 address to be allocated")
	}
}

func TestConcurrentAllocationProducesUniqueAddresses(t *testing.T) {
	a, err := New(mustPrefix(t, "10.0.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}

	const workers = 128
	results := make(chan netip.Addr, workers)
	errs := make(chan error, workers)

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			addr, err := a.Allocate()
			if err != nil {
				errs <- err
				return
			}
			results <- addr
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("allocation failed: %v", err)
	}

	seen := make(map[netip.Addr]struct{}, workers)
	for addr := range results {
		if _, exists := seen[addr]; exists {
			t.Fatalf("duplicate allocation: %s", addr)
		}
		seen[addr] = struct{}{}
	}
	if len(seen) != workers {
		t.Fatalf("got %d unique addresses, want %d", len(seen), workers)
	}
}

func TestStats(t *testing.T) {
	a, err := New(mustPrefix(t, "192.0.2.0/30"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Allocate(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Allocate(); err != nil {
		t.Fatal(err)
	}

	stats := a.Stats()
	if stats.Capacity != 4 || stats.Allocated != 2 || stats.Available != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}
