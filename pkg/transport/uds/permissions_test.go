package uds

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSocketPermissions_OK(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "nm.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	_ = os.Chmod(dir, 0o700)
	_ = os.Chmod(sock, 0o600)

	// In test environments we are usually not root; disable root-owner check.
	if err := ValidateSocketPermissions(sock, WithRequireRootOwner(false)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSocketPermissions_WorldWritableDir(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o777)
	sock := filepath.Join(dir, "nm.sock")

	// create a socket to satisfy later checks
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	err = ValidateSocketPermissions(sock, WithRequireRootOwner(false))
	if err == nil {
		t.Fatalf("expected error for world-writable dir")
	}
}

func TestValidateSocketPermissions_WorldWritableSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "nm.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	_ = os.Chmod(dir, 0o700)
	_ = os.Chmod(sock, 0o622)

	err = ValidateSocketPermissions(sock, WithRequireRootOwner(false))
	if err == nil {
		t.Fatalf("expected error for world-writable socket")
	}
}

func TestValidateSocketPermissions_GroupWritableDisallowed(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "nm.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	_ = os.Chmod(dir, 0o700)
	_ = os.Chmod(sock, 0o660)

	// Default disallows group write
	err = ValidateSocketPermissions(sock, WithRequireRootOwner(false))
	if err == nil {
		t.Fatalf("expected error for group-writable socket with AllowGroupWrite=false")
	}

	// Allow group write now
	err = ValidateSocketPermissions(sock, WithRequireRootOwner(false), WithAllowGroupWrite(true))
	if err != nil {
		t.Fatalf("unexpected error with AllowGroupWrite: %v", err)
	}
}
