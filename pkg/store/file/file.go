//go:build linux

// Package file implements a Linux-local filesystem-backed transactional
// key-value store.
//
// The backend stores one value in one regular file under a root directory. It
// hex-encodes each key into the filename. So a caller key never acts as a
// filesystem path. This design blocks path traversal and symlink attacks.
//
// A single key write is crash-atomic. The backend writes a temporary file,
// fsyncs it, renames it over the target, then fsyncs the root directory. A
// delete unlinks the file. A commit fsyncs the root directory one time, after
// all writes and all deletes.
//
// Update serializes writers with a single-writer slot. Update honors the
// context while it waits for the slot. Update stages changes in memory and
// applies them only when the callback returns nil. During the apply step Update
// holds a write lock, so a reader never sees a half-applied commit.
//
// The backend does not provide crash-atomicity across multiple keys. A crash
// during a multi-key commit can leave some keys applied and others unapplied.
//
// This package is Linux-only. It uses flock for the process lock and O_NOFOLLOW
// to reject symlinks.
package file

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github/setera/pkg/store"
)

const (
	defaultDirMode      = 0o700
	defaultFileMode     = 0o600
	defaultMaxValueSize = int64(1 << 20) // 1 MiB
)

// Option configures a Store.
type Option func(*config)

type config struct {
	dirMode      fs.FileMode
	fileMode     fs.FileMode
	maxValueSize int64
}

// WithMaxValueSize sets the maximum value size that the store accepts. A
// non-positive value is ignored.
func WithMaxValueSize(n int64) Option {
	return func(c *config) {
		if n > 0 {
			c.maxValueSize = n
		}
	}
}

// WithDirMode sets the permission mode of the root directory. The store applies
// the mode only when it creates the directory. A zero mode is ignored.
func WithDirMode(mode fs.FileMode) Option {
	return func(c *config) {
		if mode.Perm() != 0 {
			c.dirMode = mode.Perm()
		}
	}
}

// WithFileMode sets the permission mode of each value file. A zero mode is
// ignored.
func WithFileMode(mode fs.FileMode) Option {
	return func(c *config) {
		if mode.Perm() != 0 {
			c.fileMode = mode.Perm()
		}
	}
}

// Store is a filesystem-backed transactional key-value store.
type Store struct {
	root string
	cfg  config

	// writeCh is the single-writer slot. Update takes it. Update honors the
	// context while it waits.
	writeCh chan struct{}

	// mu guards closed. mu also excludes readers during the commit of an
	// Update, so a reader never sees a half-applied commit.
	mu       sync.RWMutex
	closed   bool
	lockFile *os.File
}

// Open opens or creates a filesystem store rooted at root.
//
// Open creates a missing root. Open returns ErrInvalidRoot when root is empty,
// a symlink, or an existing non-directory. Open returns store.ErrLocked when
// another process owns the root. Native filesystem errors pass through the
// error wrap.
//
// The maximum key length is near 119 bytes. The backend hex-encodes the key and
// adds a short prefix, and a Linux filename component has a 255-byte limit.
func Open(root string, opts ...Option) (*Store, error) {
	if root == "" {
		return nil, fmt.Errorf("open file store: %w: root is empty", ErrInvalidRoot)
	}

	cfg := config{
		dirMode:      defaultDirMode,
		fileMode:     defaultFileMode,
		maxValueSize: defaultMaxValueSize,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root %q: %w", root, err)
	}
	if err := ensureRoot(absRoot, cfg.dirMode); err != nil {
		return nil, fmt.Errorf("open root %q: %w", absRoot, err)
	}

	lockFile, err := openAndLock(filepath.Join(absRoot, lockFileName), cfg.fileMode)
	if err != nil {
		return nil, fmt.Errorf("lock root %q: %w", absRoot, err)
	}

	s := &Store{
		root:     absRoot,
		cfg:      cfg,
		writeCh:  make(chan struct{}, 1),
		lockFile: lockFile,
	}
	if err := s.cleanupTemps(); err != nil {
		_ = unlockAndClose(lockFile)
		return nil, fmt.Errorf("clean root %q: %w", absRoot, err)
	}

	return s, nil
}

// View executes fn inside a read-only transaction. Many View calls run at once.
func (s *Store) View(ctx context.Context, fn func(store.ReadTx) error) error {
	if fn == nil {
		return errors.New("file store: nil view callback")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return store.ErrClosed
	}

	tx := newReadTx(ctx, s)
	defer tx.close()
	return fn(tx)
}

// Update executes fn inside an exclusive read/write transaction. Update takes
// the single-writer slot and honors the context while it waits. Update stages
// changes and applies them only when fn returns nil.
func (s *Store) Update(ctx context.Context, fn func(store.WriteTx) error) error {
	if fn == nil {
		return errors.New("file store: nil update callback")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Take the writer slot. Honor the context during the wait.
	select {
	case s.writeCh <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.writeCh }()

	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return store.ErrClosed
	}

	tx := newWriteTx(ctx, s)
	defer tx.close()

	if err := fn(tx); err != nil {
		return err
	}

	// This is the last context check. After this point commit runs to the end.
	// So a cancellation cannot apply one key and skip its sibling.
	if err := ctx.Err(); err != nil {
		return err
	}

	// Commit under the write lock. The lock excludes readers during the disk
	// mutation, so a reader never sees a half-applied commit.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return store.ErrClosed
	}
	return tx.commit()
}

// Close releases the process lock. Close is idempotent.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.lockFile == nil {
		return nil
	}
	if err := unlockAndClose(s.lockFile); err != nil {
		return fmt.Errorf("close file store: %w", err)
	}
	s.lockFile = nil
	return nil
}
