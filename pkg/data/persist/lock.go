package persist

import (
	"os"
	"path/filepath"

	"github.com/alexflint/go-filemutex"
)

// newFileLock ensures the parent dir exists and then creates a file-backed mutex.
func newFileLock(lockPath string) (*filemutex.FileMutex, error) {
	// If the path is a directory (or ends in “/”), use a default filename inside it.
	fi, err := os.Stat(lockPath)
	if err == nil && fi.IsDir() {
		lockPath = filepath.Join(lockPath, "state.lock")
	} else if os.IsNotExist(err) {
		// Ensure the parent directory exists
		parent := filepath.Dir(lockPath)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return nil, err
		}
		// Now lockPath doesn’t exist, but filemutex.New will create it
	} else if err != nil {
		return nil, err
	}

	m, err := filemutex.New(lockPath)
	if err != nil {
		return nil, err
	}
	return m, nil
}
