package nodeipam

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"

	"github/setera/pkg/store"
)

const (
	allocationKeyPrefix     byte = 0x01
	allocationRecordVersion int  = 1
)

var allocationPrefix = []byte{allocationKeyPrefix}

type allocationRecord struct {
	Version     int    `json:"version"`
	ContainerID string `json:"container_id"`
	IfName      string `json:"if_name"`
	PodUID      string `json:"pod_uid"`
	TenantID    string `json:"tenant_id"`
}

type repository struct {
	store storage
}

func newRepository(s storage) repository {
	return repository{store: s}
}

func (r repository) put(ctx context.Context, allocation Allocation) error {
	key, err := allocationKey(allocation.IP)
	if err != nil {
		return err
	}
	value, err := encodeAllocation(allocation)
	if err != nil {
		return err
	}

	return r.store.Update(ctx, func(tx store.WriteTx) error {
		return tx.Put(key, value)
	})
}

func (r repository) delete(ctx context.Context, addr netip.Addr) error {
	key, err := allocationKey(addr)
	if err != nil {
		return err
	}

	return r.store.Update(ctx, func(tx store.WriteTx) error {
		return tx.Delete(key)
	})
}

func (r repository) list(ctx context.Context) ([]Allocation, error) {
	var allocations []Allocation

	err := r.store.View(ctx, func(tx store.ReadTx) error {
		return tx.Iterate(allocationPrefix, func(key, value []byte) error {
			addr, err := decodeAllocationKey(key)
			if err != nil {
				return err
			}
			allocation, err := decodeAllocation(addr, value)
			if err != nil {
				return err
			}
			allocations = append(allocations, allocation)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return allocations, nil
}

func allocationKey(addr netip.Addr) ([]byte, error) {
	if !addr.IsValid() {
		return nil, fmt.Errorf("allocation key: %w: invalid IP address", ErrCorruptState)
	}
	addr = addr.Unmap()

	if addr.Is4() {
		value := addr.As4()
		key := make([]byte, 1+len(value))
		key[0] = allocationKeyPrefix
		copy(key[1:], value[:])
		return key, nil
	}

	value := addr.As16()
	key := make([]byte, 1+len(value))
	key[0] = allocationKeyPrefix
	copy(key[1:], value[:])
	return key, nil
}

func decodeAllocationKey(key []byte) (netip.Addr, error) {
	if len(key) == 0 || key[0] != allocationKeyPrefix {
		return netip.Addr{}, fmt.Errorf("%w: invalid allocation key prefix", ErrCorruptState)
	}

	switch len(key) {
	case 5:
		var value [4]byte
		copy(value[:], key[1:])
		return netip.AddrFrom4(value), nil
	case 17:
		var value [16]byte
		copy(value[:], key[1:])
		return netip.AddrFrom16(value), nil
	default:
		return netip.Addr{}, fmt.Errorf("%w: invalid allocation key length %d", ErrCorruptState, len(key))
	}
}

func encodeAllocation(allocation Allocation) ([]byte, error) {
	if err := allocation.validate(); err != nil {
		return nil, err
	}

	record := allocationRecord{
		Version:     allocationRecordVersion,
		ContainerID: allocation.Owner.ContainerID,
		IfName:      allocation.Owner.IfName,
		PodUID:      allocation.PodUID,
		TenantID:    allocation.TenantID,
	}
	value, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode allocation %s: %w", allocation.IP, err)
	}
	return value, nil
}

func decodeAllocation(addr netip.Addr, value []byte) (Allocation, error) {
	var record allocationRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return Allocation{}, fmt.Errorf("%w: decode allocation %s: %v", ErrCorruptState, addr, err)
	}
	if record.Version != allocationRecordVersion {
		return Allocation{}, fmt.Errorf(
			"%w: allocation %s has version %d, want %d",
			ErrCorruptState,
			addr,
			record.Version,
			allocationRecordVersion,
		)
	}

	allocation := Allocation{
		IP: addr.Unmap(),
		Owner: Owner{
			ContainerID: record.ContainerID,
			IfName:      record.IfName,
		},
		PodUID:   record.PodUID,
		TenantID: record.TenantID,
	}
	if err := allocation.validate(); err != nil {
		return Allocation{}, err
	}
	return allocation, nil
}
