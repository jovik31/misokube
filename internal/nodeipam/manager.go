package nodeipam

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"sync"

	"github/setera/pkg/store"
)

// allocator is the part of an IP allocator that NodeIPAM needs.
// Concrete allocators satisfy this interface without importing nodeipam.
type allocator interface {
	Allocate() (netip.Addr, error)
	AllocateSpecific(netip.Addr) error
	Release(netip.Addr) error
}

// storage is the part of a transactional store that NodeIPAM needs.
// NodeIPAM does not own the store lifecycle, so Close is not part of this
// interface.
type storage interface {
	View(context.Context, func(store.ReadTx) error) error
	Update(context.Context, func(store.WriteTx) error) error
}

// Manager owns node-local IP allocations and their durable records.
//
// The manager must be the only owner of its allocator. Call Restore before
// Allocate or Release.
type Manager struct {
	mu sync.RWMutex

	allocator allocator
	repo      repository

	restored bool
	byOwner  map[Owner]Allocation
}

// New creates a node-local IPAM manager.
func New(a allocator, s storage) (*Manager, error) {
	if a == nil {
		return nil, fmt.Errorf("%w: allocator is nil", ErrInvalidDependency)
	}
	if s == nil {
		return nil, fmt.Errorf("%w: store is nil", ErrInvalidDependency)
	}

	return &Manager{
		allocator: a,
		repo:      newRepository(s),
		byOwner:   make(map[Owner]Allocation),
	}, nil
}

// Allocate gives an address to req and saves the allocation before it returns.
// A repeated request with the same owner and metadata returns the first
// allocation.
func (m *Manager) Allocate(ctx context.Context, req Request) (Allocation, error) {
	if err := req.validate(); err != nil {
		return Allocation{}, err
	}
	if err := ctx.Err(); err != nil {
		return Allocation{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.restored {
		return Allocation{}, ErrNotRestored
	}

	if existing, ok := m.byOwner[req.Owner]; ok {
		if existing.matches(req) {
			return existing, nil
		}
		return Allocation{}, fmt.Errorf(
			"%w: container %q interface %q already owns %s",
			ErrOwnerConflict,
			req.Owner.ContainerID,
			req.Owner.IfName,
			existing.IP,
		)
	}

	addr, err := m.allocator.Allocate()
	if err != nil {
		return Allocation{}, fmt.Errorf("allocate address: %w", err)
	}
	addr = addr.Unmap()

	allocation := Allocation{
		IP:       addr,
		Owner:    req.Owner,
		PodUID:   req.PodUID,
		TenantID: req.TenantID,
	}

	if err := m.repo.put(ctx, allocation); err != nil {
		rollbackErr := m.allocator.Release(addr)
		if rollbackErr != nil {
			return Allocation{}, errors.Join(
				fmt.Errorf("persist allocation %s: %w", addr, err),
				fmt.Errorf("rollback allocation %s: %w", addr, rollbackErr),
			)
		}
		return Allocation{}, fmt.Errorf("persist allocation %s: %w", addr, err)
	}

	m.byOwner[allocation.Owner] = allocation
	return allocation, nil
}

// Release gives an address back. Release is idempotent for an unknown owner.
// The caller must remove the pod network and datapath state before it calls
// Release.
func (m *Manager) Release(ctx context.Context, owner Owner) error {
	if err := owner.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.restored {
		return ErrNotRestored
	}

	allocation, ok := m.byOwner[owner]
	if !ok {
		return nil
	}

	// Free the in-memory address first. The manager lock prevents another ADD
	// from using it until the durable delete completes. If the delete fails, put
	// the address back into the allocator.
	if err := m.allocator.Release(allocation.IP); err != nil {
		return fmt.Errorf("release address %s: %w", allocation.IP, err)
	}

	if err := m.repo.delete(ctx, allocation.IP); err != nil {
		rollbackErr := m.allocator.AllocateSpecific(allocation.IP)
		if rollbackErr != nil {
			return errors.Join(
				fmt.Errorf("delete allocation %s: %w", allocation.IP, err),
				fmt.Errorf("rollback release %s: %w", allocation.IP, rollbackErr),
			)
		}
		return fmt.Errorf("delete allocation %s: %w", allocation.IP, err)
	}

	delete(m.byOwner, owner)
	return nil
}

// Get returns the allocation for owner.
func (m *Manager) Get(owner Owner) (Allocation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allocation, ok := m.byOwner[owner]
	return allocation, ok
}

// List returns a snapshot of all live allocations.
func (m *Manager) List() []Allocation {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Allocation, 0, len(m.byOwner))
	for _, allocation := range m.byOwner {
		out = append(out, allocation)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].IP.Compare(out[j].IP) < 0
	})
	return out
}
