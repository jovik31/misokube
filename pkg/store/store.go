// Package store defines a small transactional key-value store.
//
// A store keeps opaque byte values under byte keys. All access happens inside a
// transaction. A read transaction observes the last committed state. A write
// transaction stages changes and makes them durable as one atomic unit.
//
// # Transaction model
//
// The store controls the transaction lifetime. View runs a read-only function.
// Update runs a read-write function. The store commits an Update when the
// function returns nil. The store rolls back an Update when the function returns
// an error or panics. A caller cannot commit, roll back, or leak a transaction
// by mistake.
//
// A transaction value is valid only during its function. Do not keep it. A
// method call on a transaction after its function returns gives ErrTxClosed.
//
// # Concurrency
//
// The store runs at most one Update at a time. If an Update is active, a new
// Update waits until the active one completes or the context is done. The store
// can run several View calls at the same time. A View observes committed data
// only. A View never sees the changes of an Update that is not yet committed.
//
// # Keys and values
//
// A key that a caller passes to Get, Iterate, Put, or Delete is borrowed for the
// call only. Do not change a key while a store method uses it.
//
// Put copies the key and the value. The caller can reuse or change its slices
// after Put returns.
//
// A value that Get or Iterate returns is borrowed. It is valid only until the
// transaction function returns. Do not change it. Copy the bytes to keep them
// longer.
//
// An empty value is a valid stored value. It is different from an absent key.
// Get reports an absent key with ErrNotFound. A caller must test the error, not
// the length of the slice, to decide if a key exists.
package store

import (
	"context"
)

// Store reads values and runs write transactions.
type Store interface {
	// View runs fn inside a read-only transaction. The store gives fn a ReadTx
	// that observes the last committed state. View returns the error from fn.
	// View returns ErrClosed if the store is closed. View returns the context
	// error if the context is done.
	//
	// The store does not change any data during View.
	View(ctx context.Context, fn func(ReadTx) error) error

	// Update runs fn inside a read-write transaction. The store gives fn a
	// WriteTx. The store commits the staged changes if fn returns nil. The store
	// rolls back the staged changes if fn returns an error, and Update returns
	// that error.
	//
	// The store runs at most one Update at a time. Update waits until the active
	// Update completes or the context is done. Update returns the context error
	// if the context is done before the transaction starts. Update returns
	// ErrClosed if the store is closed. Update returns ErrLocked if another
	// process or store instance owns the data.
	//
	// If fn panics, the store rolls back the staged changes and raises the panic
	// again.
	Update(ctx context.Context, fn func(WriteTx) error) error

	// Close releases the resources of the store. Close makes the exclusive lock
	// free for another process. After Close, View and Update return ErrClosed.
	// Close is idempotent. A second call returns nil.
	Close() error
}

// ReadTx is a read-only view of the store.
//
// A ReadTx from View observes the last committed state. A WriteTx embeds ReadTx,
// so a read inside a write transaction also observes the pending changes of that
// transaction. This behavior is read-your-writes.
type ReadTx interface {
	// Get returns the value under key.
	//
	// Get returns ErrNotFound if the key does not exist. Get returns
	// ErrInvalidKey if the key is empty or invalid.
	//
	// The returned value is borrowed. It is valid only until the transaction
	// function returns. Do not change it. Copy the bytes to keep them longer.
	Get(key []byte) ([]byte, error)

	// Iterate calls fn for each pair whose key starts with prefix. A nil or
	// empty prefix selects every pair.
	//
	// The order of the pairs is not defined. The key and the value that fn
	// receives are borrowed. They are valid only during the call to fn. Copy the
	// bytes to keep them longer.
	//
	// If fn returns an error, Iterate stops and returns that error. Use a
	// private sentinel error to stop early without a failure. fn must not call a
	// method of the same transaction.
	Iterate(prefix []byte, fn func(key, value []byte) error) error
}

// WriteTx is a read-write view of the store. A WriteTx embeds ReadTx.
type WriteTx interface {
	ReadTx

	// Put stores value under key. The store copies key and value. The caller can
	// reuse or change its slices after Put returns.
	//
	// A nil value and an empty value are the same. Both store a present value
	// with zero length. Put returns ErrInvalidKey if the key is empty or
	// invalid.
	Put(key, value []byte) error

	// Delete removes key. A Delete of an absent key is not an error. It returns
	// nil. Delete returns ErrInvalidKey if the key is empty or invalid.
	Delete(key []byte) error
}
