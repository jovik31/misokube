package store

import (
	"errors"
)

// The sentinel errors let callers classify the errors of the store package with
// errors.Is.
//
// Usage:
//
//	if errors.Is(err, store.ErrNotFound) {
//		return nil
//	}
//
// A backend can wrap a sentinel with %w or with an *OpError. errors.Is still
// finds the sentinel through the wrap.
var (
	// ErrNotFound reports that a key does not exist.
	ErrNotFound = errors.New("store: not found")

	// ErrInvalidKey reports that a key is empty, malformed, or unsupported by
	// the backend.
	ErrInvalidKey = errors.New("store: invalid key")

	// ErrClosed reports an operation on a closed store.
	ErrClosed = errors.New("store: closed")

	// ErrTxClosed reports use of a transaction after its function returns.
	ErrTxClosed = errors.New("store: transaction closed")

	// ErrLocked reports that another process or store instance owns the data
	// exclusively.
	ErrLocked = errors.New("store: locked")

	// ErrCorrupt reports that the backend cannot read the persisted state
	// safely.
	ErrCorrupt = errors.New("store: corrupt data")

	// ErrValueTooLarge reports that the value is too large for the backend.
	ErrValueTooLarge = errors.New("store: value too large")
)
