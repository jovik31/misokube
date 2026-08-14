//go:build linux

package network

import (
	"errors"
	"os"
	"testing"
)

func TestEnableIPv4Forwarding(t *testing.T) {
	oldWriteFile := writeFile
	defer func() { writeFile = oldWriteFile }()

	called := false
	writeFile = func(path string, data []byte, mode os.FileMode) error {
		called = true
		if path != ipv4ForwardingPath {
			t.Fatalf("path = %q, want %q", path, ipv4ForwardingPath)
		}
		if string(data) != "1\n" {
			t.Fatalf("data = %q, want %q", string(data), "1\\n")
		}
		return nil
	}

	if err := NewLinux().EnableIPv4Forwarding(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("forwarding sysctl was not written")
	}
}

func TestEnableIPv4ForwardingReturnsError(t *testing.T) {
	oldWriteFile := writeFile
	defer func() { writeFile = oldWriteFile }()

	want := errors.New("write failed")
	writeFile = func(string, []byte, os.FileMode) error {
		return want
	}

	if err := NewLinux().EnableIPv4Forwarding(); !errors.Is(err, want) {
		t.Fatalf("got %v, want wrapped write error", err)
	}
}
