package ipam

import "errors"

var (
	// ErrExhausted indicates that no allocatable address remains in the pool.
	ErrExhausted = errors.New("ipam: pool exhausted")

	// ErrInvalidPrefix indicates that an allocator was created with an invalid
	// or unsupported network prefix.
	ErrInvalidPrefix = errors.New("ipam: invalid prefix")

	// ErrInvalidAddress indicates that a caller supplied an invalid IP address.
	ErrInvalidAddress = errors.New("ipam: invalid address")

	// ErrOutOfRange indicates that an address does not belong to the allocator's
	// configured pool.
	ErrOutOfRange = errors.New("ipam: address outside pool")

	// ErrAllocated indicates that AllocateSpecific was called for an address
	// that is already allocated.
	ErrAllocated = errors.New("ipam: address already allocated")

	// ErrReserved indicates that an address belongs to the pool but has been
	// reserved by the concrete allocator and cannot be allocated.
	ErrReserved = errors.New("ipam: address reserved")

	// ErrPoolTooLarge indicates that a concrete allocator cannot represent the
	// requested pool within its configured capacity limit.
	ErrPoolTooLarge = errors.New("ipam: pool too large")
)
