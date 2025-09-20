package uds

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// PermOptions controls how ValidateSocketPermissions enforces ownership & modes.
type PermOptions struct {
	// If true, require socket owner uid == 0 (root). Default: true.
	RequireRootOwner bool
	// If true, allow group-writable socket (e.g., 0660). Default: false.
	AllowGroupWrite bool
	// If set, require socket's group id to match this value.
	AllowedGID *uint32
}

// PermOption is a functional option for ValidateSocketPermissions.
type PermOption func(*PermOptions)

func WithRequireRootOwner(b bool) PermOption { return func(o *PermOptions) { o.RequireRootOwner = b } }
func WithAllowGroupWrite(b bool) PermOption  { return func(o *PermOptions) { o.AllowGroupWrite = b } }
func WithAllowedGID(gid uint32) PermOption   { return func(o *PermOptions) { o.AllowedGID = &gid } }

// ValidateSocketPermissions checks that:
//   - the parent directory exists, is a directory, and is NOT world-writable
//   - the path is a Unix domain socket
//   - the socket is NOT world-writable
//   - if AllowGroupWrite=false, the socket is NOT group-writable
//   - if RequireRootOwner=true, the socket owner uid == 0
//   - if AllowedGID is set, the socket gid matches it
//
// Typical secure settings:
//
//	dir: 0700 or 0755
//	sock: 0600 (or 0660 if AllowGroupWrite with a controlled group)
func ValidateSocketPermissions(sock string, opts ...PermOption) error {
	// defaults
	o := &PermOptions{
		RequireRootOwner: true,
		AllowGroupWrite:  false,
	}
	for _, fn := range opts {
		fn(o)
	}

	dir := filepath.Dir(sock)
	dfi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat dir %q: %w", dir, err)
	}
	if !dfi.IsDir() {
		return fmt.Errorf("socket dir %q is not a directory", dir)
	}
	if isWorldWritable(dfi.Mode().Perm()) {
		return fmt.Errorf("socket dir %q is world-writable; tighten to 0700/0755", dir)
	}

	fi, err := os.Lstat(sock)
	if err != nil {
		return fmt.Errorf("stat socket %q: %w", sock, err)
	}
	if (fi.Mode() & fs.ModeSocket) == 0 {
		return fmt.Errorf("%q is not a unix domain socket", sock)
	}
	perm := fi.Mode().Perm()
	if isWorldWritable(perm) {
		return fmt.Errorf("socket %q is world-writable (%#o); refused", sock, perm)
	}
	if !o.AllowGroupWrite && isGroupWritable(perm) {
		return fmt.Errorf("socket %q is group-writable (%#o) but AllowGroupWrite=false", sock, perm)
	}

	// Ownership checks (best-effort; only if Stat_t is available)
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		if o.RequireRootOwner && st.Uid != 0 {
			return fmt.Errorf("socket %q must be owned by root (uid 0), got uid %d", sock, st.Uid)
		}
		if o.AllowedGID != nil && st.Gid != *o.AllowedGID {
			return fmt.Errorf("socket %q gid %d != allowed gid %d", sock, st.Gid, *o.AllowedGID)
		}
	}

	return nil
}

func isWorldWritable(perm fs.FileMode) bool { return (perm & 0o002) != 0 }
func isGroupWritable(perm fs.FileMode) bool { return (perm & 0o020) != 0 }
