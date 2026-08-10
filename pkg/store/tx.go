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
