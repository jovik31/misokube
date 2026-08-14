//go:build linux

package file

import "errors"

// ErrInvalidRoot reports that the configured root is empty, a symlink, or an
// existing non-directory.
var ErrInvalidRoot = errors.New("file store: invalid root")
