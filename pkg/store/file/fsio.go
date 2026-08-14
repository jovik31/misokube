//go:build linux

package file

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github/setera/pkg/store"
)

const tempFilePrefix = ".store-tmp-"

// readFile reads one value file. It never follows a symlink. It maps a missing
// file to store.ErrNotFound. It rejects a non-regular or oversized file with
// store.ErrCorrupt. os.OpenFile sets close-on-exec for us.
func (s *Store) readFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, store.ErrNotFound
		case errors.Is(err, syscall.ELOOP):
			return nil, store.ErrCorrupt
		default:
			return nil, err
		}
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, store.ErrCorrupt
	}
	if info.Size() > s.cfg.maxValueSize {
		return nil, fmt.Errorf(
			"%w: persisted value is %d bytes, maximum is %d",
			store.ErrCorrupt, info.Size(), s.cfg.maxValueSize,
		)
	}

	// Read one byte more than the maximum. If the file grew after the stat, the
	// extra byte triggers store.ErrCorrupt. This defends against a change during
	// the read.
	data, err := io.ReadAll(io.LimitReader(f, s.cfg.maxValueSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.cfg.maxValueSize {
		return nil, store.ErrCorrupt
	}
	return data, nil
}

// iterate walks the root directory in sorted name order. os.ReadDir returns the
// entries in sorted order. iterate skips the lock file, the temporary files, and
// any foreign file that is not a key file. It reads each key file whose key
// starts with prefix and calls fn.
func (s *Store) iterate(ctx context.Context, prefix []byte, fn func([]byte, []byte) error) error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}

		name := entry.Name()
		if name == lockFileName || strings.HasPrefix(name, tempFilePrefix) {
			continue
		}
		if !strings.HasPrefix(name, keyFilePrefix) {
			continue // a foreign file; not ours, so skip it
		}
		// A key file must be regular. A symlink or a non-regular file with the
		// key prefix is a damaged key file.
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("iterate entry %q: %w", name, store.ErrCorrupt)
		}

		key, err := decodeKey(name)
		if err != nil {
			return fmt.Errorf("iterate entry %q: %w", name, err)
		}
		if !bytes.HasPrefix(key, prefix) {
			continue
		}

		value, err := s.readFile(filepath.Join(s.root, name))
		if err != nil {
			return fmt.Errorf("iterate key %x: %w", key, err)
		}
		if err := fn(bytes.Clone(key), value); err != nil {
			return err
		}
	}
	return nil
}

// writeValue writes one value with crash-atomicity. writeValue does not fsync
// the directory. The caller fsyncs the directory one time after the batch.
// writeValue writes a temporary file, fsyncs it, then renames it over the
// destination. The temporary file is a sibling, so the rename stays on one
// filesystem.
func (s *Store) writeValue(path string, value []byte) error {
	tmp, err := os.CreateTemp(s.root, tempFilePrefix)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	if err := tmp.Chmod(s.cfg.fileMode); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(value); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// removeValue deletes one value file. removeValue does not fsync the directory.
// It never follows a symlink. A missing file is not an error.
func (s *Store) removeValue(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return store.ErrCorrupt
	}

	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

// syncDir fsyncs a directory. On Linux this makes a rename or an unlink durable.
func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// cleanupTemps removes leftover temporary files from an earlier crash. It runs
// one time at Open. It fsyncs the directory one time, only if it removed a file.
func (s *Store) cleanupTemps() error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}

	removed := false
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), tempFilePrefix) {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("temporary entry %q: %w", entry.Name(), store.ErrCorrupt)
		}
		if err := os.Remove(filepath.Join(s.root, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		removed = true
	}

	if removed {
		return syncDir(s.root)
	}
	return nil
}
