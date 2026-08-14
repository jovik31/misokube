package nodeipam

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
)

// Restore rebuilds the allocator and the in-memory indexes from durable state.
// Restore is idempotent after it succeeds.
func (m *Manager) Restore(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.restored {
		return nil
	}

	allocations, err := m.repo.list(ctx)
	if err != nil {
		return fmt.Errorf("restore allocations: %w", err)
	}

	byOwner := make(map[Owner]Allocation, len(allocations))

	for _, allocation := range allocations {
		if err := allocation.validate(); err != nil {
			return err
		}
		if previous, ok := byOwner[allocation.Owner]; ok {
			return fmt.Errorf(
				"%w: owner %q/%q has addresses %s and %s",
				ErrCorruptState,
				allocation.Owner.ContainerID,
				allocation.Owner.IfName,
				previous.IP,
				allocation.IP,
			)
		}
		byOwner[allocation.Owner] = allocation
	}

	restored := make([]netip.Addr, 0, len(allocations))
	for _, allocation := range allocations {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, m.rollbackRestore(restored))
		}
		if err := m.allocator.AllocateSpecific(allocation.IP); err != nil {
			return errors.Join(
				fmt.Errorf("restore address %s: %w", allocation.IP, err),
				m.rollbackRestore(restored),
			)
		}
		restored = append(restored, allocation.IP)
	}

	m.byOwner = byOwner
	m.restored = true
	return nil
}

func (m *Manager) rollbackRestore(addrs []netip.Addr) error {
	var rollbackErr error
	for i := len(addrs) - 1; i >= 0; i-- {
		if err := m.allocator.Release(addrs[i]); err != nil {
			rollbackErr = errors.Join(
				rollbackErr,
				fmt.Errorf("rollback restore %s: %w", addrs[i], err),
			)
		}
	}
	return rollbackErr
}
