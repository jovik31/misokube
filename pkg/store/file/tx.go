//go:build linux

package file

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync/atomic"

	"github/setera/pkg/store"
)

// txState holds the shared state of a transaction. The closed flag is atomic, so
// a use of a finished transaction is safe to detect.
type txState struct {
	ctx    context.Context
	s      *Store
	closed atomic.Bool
}

func (tx *txState) check() error {
	if tx.closed.Load() {
		return store.ErrTxClosed
	}
	return tx.ctx.Err()
}

func (tx *txState) close() {
	tx.closed.Store(true)
}

type readTx struct {
	state *txState
}

func newReadTx(ctx context.Context, s *Store) *readTx {
	return &readTx{state: &txState{ctx: ctx, s: s}}
}

func (tx *readTx) close() {
	tx.state.close()
}

func (tx *readTx) Get(key []byte) ([]byte, error) {
	if err := tx.state.check(); err != nil {
		return nil, err
	}

	name, err := encodeKey(key)
	if err != nil {
		return nil, fmt.Errorf("get key: %w", err)
	}

	value, err := tx.state.s.readFile(filepath.Join(tx.state.s.root, name))
	if err != nil {
		return nil, fmt.Errorf("get key %x: %w", key, err)
	}
	return value, nil
}

func (tx *readTx) Iterate(prefix []byte, fn func(key, value []byte) error) error {
	if err := tx.state.check(); err != nil {
		return err
	}
	if fn == nil {
		return errors.New("file store: nil iterate callback")
	}
	return tx.state.s.iterate(tx.state.ctx, prefix, fn)
}

type pendingWrite struct {
	key   []byte
	value []byte
}

// writeTx stages writes and deletes in memory. It embeds readTx, so a read sees
// the committed state under the staged overlay. This gives read-your-writes.
type writeTx struct {
	*readTx
	writes  map[string]pendingWrite
	deletes map[string][]byte
}

func newWriteTx(ctx context.Context, s *Store) *writeTx {
	return &writeTx{
		readTx:  newReadTx(ctx, s),
		writes:  make(map[string]pendingWrite),
		deletes: make(map[string][]byte),
	}
}

func (tx *writeTx) Get(key []byte) ([]byte, error) {
	if err := tx.state.check(); err != nil {
		return nil, err
	}

	name, err := encodeKey(key)
	if err != nil {
		return nil, fmt.Errorf("get key: %w", err)
	}
	if _, deleted := tx.deletes[name]; deleted {
		return nil, store.ErrNotFound
	}
	if pending, ok := tx.writes[name]; ok {
		return bytes.Clone(pending.value), nil
	}
	return tx.readTx.Get(key)
}

func (tx *writeTx) Put(key, value []byte) error {
	if err := tx.state.check(); err != nil {
		return err
	}

	name, err := encodeKey(key)
	if err != nil {
		return fmt.Errorf("put key: %w", err)
	}
	if int64(len(value)) > tx.state.s.cfg.maxValueSize {
		return fmt.Errorf(
			"put key %x: %w: value is %d bytes, maximum is %d",
			key, store.ErrValueTooLarge, len(value), tx.state.s.cfg.maxValueSize,
		)
	}

	tx.writes[name] = pendingWrite{
		key:   bytes.Clone(key),
		value: bytes.Clone(value),
	}
	delete(tx.deletes, name)
	return nil
}

func (tx *writeTx) Delete(key []byte) error {
	if err := tx.state.check(); err != nil {
		return err
	}

	name, err := encodeKey(key)
	if err != nil {
		return fmt.Errorf("delete key: %w", err)
	}

	delete(tx.writes, name)
	tx.deletes[name] = bytes.Clone(key)
	return nil
}

func (tx *writeTx) Iterate(prefix []byte, fn func(key, value []byte) error) error {
	if err := tx.state.check(); err != nil {
		return err
	}
	if fn == nil {
		return errors.New("file store: nil iterate callback")
	}

	// Read the committed pairs first. Then apply the overlay: drop deleted keys
	// and add or replace the staged writes.
	values := make(map[string]pendingWrite)
	if err := tx.state.s.iterate(tx.state.ctx, prefix, func(key, value []byte) error {
		name, err := encodeKey(key)
		if err != nil {
			return err
		}
		values[name] = pendingWrite{key: bytes.Clone(key), value: bytes.Clone(value)}
		return nil
	}); err != nil {
		return err
	}

	for name := range tx.deletes {
		delete(values, name)
	}
	for name, pending := range tx.writes {
		if bytes.HasPrefix(pending.key, prefix) {
			values[name] = pendingWrite{
				key:   bytes.Clone(pending.key),
				value: bytes.Clone(pending.value),
			}
		}
	}

	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := tx.state.check(); err != nil {
			return err
		}
		pending := values[name]
		if err := fn(bytes.Clone(pending.key), bytes.Clone(pending.value)); err != nil {
			return err
		}
	}
	return nil
}

// commit applies the staged changes. The caller holds the store write lock and
// has confirmed the store is open. commit does not check the context, so a
// cancellation cannot apply one key and skip its sibling in the same batch.
// commit fsyncs the root directory one time, after all writes and deletes.
func (tx *writeTx) commit() error {
	writeNames := make([]string, 0, len(tx.writes))
	for name := range tx.writes {
		writeNames = append(writeNames, name)
	}
	sort.Strings(writeNames)

	for _, name := range writeNames {
		path := filepath.Join(tx.state.s.root, name)
		if err := tx.state.s.writeValue(path, tx.writes[name].value); err != nil {
			return fmt.Errorf("put key %x: %w", tx.writes[name].key, err)
		}
	}

	deleteNames := make([]string, 0, len(tx.deletes))
	for name := range tx.deletes {
		deleteNames = append(deleteNames, name)
	}
	sort.Strings(deleteNames)

	for _, name := range deleteNames {
		path := filepath.Join(tx.state.s.root, name)
		if err := tx.state.s.removeValue(path); err != nil {
			return fmt.Errorf("delete key %x: %w", tx.deletes[name], err)
		}
	}

	if len(writeNames) == 0 && len(deleteNames) == 0 {
		return nil
	}
	return syncDir(tx.state.s.root)
}
