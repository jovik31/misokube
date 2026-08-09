//go:build linux

package file

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"github/setera/pkg/store"
)

const lockFileName = ".store-lock"

// ensureRoot validates or creates the root directory. It never follows a
// symlink. It applies the mode only when it creates the directory. So it does
// not change the permission that a user set on an existing directory.
func ensureRoot(root string, mode fs.FileMode) error {
	info, err := os.Lstat(root)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: root is a symlink", ErrInvalidRoot)
		}
		if !info.IsDir() {
			return fmt.Errorf("%w: root is not a directory", ErrInvalidRoot)
		}
		return nil
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(root, mode); err != nil {
			return err
		}
		// MkdirAll applies the mode through the umask. Chmod sets the exact mode
		// on the new directory.
		return os.Chmod(root, mode)
	default:
		return err
	}
}

// openAndLock opens the lock file and takes a non-blocking exclusive flock. Go
// has no os wrapper for flock, so this function must call syscall.Flock. A busy
// lock maps to store.ErrLocked. os.OpenFile sets close-on-exec for us. The
// O_NOFOLLOW flag rejects a symlink at the lock path.
func openAndLock(path string, mode fs.FileMode) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, mode.Perm())
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%w: lock file is a symlink", store.ErrCorrupt)
		}
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%w: lock file is not a regular file", store.ErrCorrupt)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, store.ErrLocked
		}
		return nil, err
	}

	return f, nil
}

// unlockAndClose releases the flock and closes the file.
func unlockAndClose(f *os.File) error {
	var errs []error
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		errs = append(errs, err)
	}
	if err := f.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
