//go:build linux

package file

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github/setera/pkg/store"
)

const (
	keyFilePrefix = "k-"

	// A Linux filename component has a 255-byte limit. The prefix takes 2 bytes.
	// Hex doubles the key length. So the real key limit is near 119 bytes.
	// encodeKey rejects a longer key before it reaches the filesystem.
	maxEncodedKeyName = 240
)

// encodeKey turns a caller key into a safe filename. It hex-encodes the key, so
// the filesystem never reads the key as a path. It rejects an empty key and an
// oversized key.
func encodeKey(key []byte) (string, error) {
	if len(key) == 0 {
		return "", store.ErrInvalidKey
	}
	name := keyFilePrefix + hex.EncodeToString(key)
	if len(name) > maxEncodedKeyName {
		return "", fmt.Errorf("%w: key is too large for the file backend", store.ErrInvalidKey)
	}
	return name, nil
}

// decodeKey reverses encodeKey. It re-encodes the result and compares it to the
// original name. This canonical check rejects a malformed key file with
// store.ErrCorrupt.
func decodeKey(name string) ([]byte, error) {
	if !strings.HasPrefix(name, keyFilePrefix) {
		return nil, store.ErrCorrupt
	}
	encoded := strings.TrimPrefix(name, keyFilePrefix)
	if encoded == "" {
		return nil, store.ErrCorrupt
	}

	key, err := hex.DecodeString(encoded)
	if err != nil || len(key) == 0 {
		return nil, store.ErrCorrupt
	}
	canonical, err := encodeKey(key)
	if err != nil || canonical != name {
		return nil, store.ErrCorrupt
	}
	return key, nil
}
