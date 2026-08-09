//go:build linux

package file_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github/setera/pkg/store"
	filestore "github/setera/pkg/store/file"
)

func openStore(t *testing.T, opts ...filestore.Option) *filestore.Store {
	t.Helper()
	s, err := filestore.Open(filepath.Join(t.TempDir(), "store"), opts...)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return s
}

func TestOpenCreatesMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	s, err := filestore.Open(root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("root is not a directory")
	}
}

func TestOpenRejectsEmptyRoot(t *testing.T) {
	_, err := filestore.Open("")
	if !errors.Is(err, filestore.ErrInvalidRoot) {
		t.Fatalf("Open() error = %v, want ErrInvalidRoot", err)
	}
}

func TestOpenRejectsFileRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := filestore.Open(root)
	if !errors.Is(err, filestore.ErrInvalidRoot) {
		t.Fatalf("Open() error = %v, want ErrInvalidRoot", err)
	}
}

func TestOpenRejectsSymlinkRoot(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "root")
	if err := os.Symlink(target, root); err != nil {
		t.Fatal(err)
	}

	_, err := filestore.Open(root)
	if !errors.Is(err, filestore.ErrInvalidRoot) {
		t.Fatalf("Open() error = %v, want ErrInvalidRoot", err)
	}
}

func TestPutAndGet(t *testing.T) {
	s := openStore(t)
	key := []byte("allocations/10.244.1.7")
	value := []byte("pod_uid=abc")

	if err := s.Update(context.Background(), func(tx store.WriteTx) error {
		return tx.Put(key, value)
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if err := s.View(context.Background(), func(tx store.ReadTx) error {
		got, err := tx.Get(key)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, value) {
			t.Fatalf("Get() = %q, want %q", got, value)
		}
		return nil
	}); err != nil {
		t.Fatalf("View() error = %v", err)
	}
}

func TestOpaqueBinaryKey(t *testing.T) {
	s := openStore(t)
	key := []byte{0x00, '/', 0xff, '\n'}
	value := []byte("value")

	if err := s.Update(context.Background(), func(tx store.WriteTx) error {
		return tx.Put(key, value)
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if err := s.View(context.Background(), func(tx store.ReadTx) error {
		got, err := tx.Get(key)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, value) {
			t.Fatalf("Get() = %q, want %q", got, value)
		}
		return nil
	}); err != nil {
		t.Fatalf("View() error = %v", err)
	}
}

func TestPutCopiesKeyAndValue(t *testing.T) {
	s := openStore(t)
	key := []byte("key")
	value := []byte("value")

	if err := s.Update(context.Background(), func(tx store.WriteTx) error {
		if err := tx.Put(key, value); err != nil {
			return err
		}
		key[0] = 'X'
		value[0] = 'X'
		return nil
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if err := s.View(context.Background(), func(tx store.ReadTx) error {
		got, err := tx.Get([]byte("key"))
		if err != nil {
			return err
		}
		if string(got) != "value" {
			t.Fatalf("Get() = %q, want value", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("View() error = %v", err)
	}
}

func TestReadYourWrites(t *testing.T) {
	s := openStore(t)
	key := []byte("key")

	if err := s.Update(context.Background(), func(tx store.WriteTx) error {
		if err := tx.Put(key, []byte("new")); err != nil {
			return err
		}
		got, err := tx.Get(key)
		if err != nil {
			return err
		}
		if string(got) != "new" {
			t.Fatalf("Get() = %q, want new", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
}

func TestRollbackOnCallbackError(t *testing.T) {
	s := openStore(t)
	wantErr := errors.New("rollback")

	err := s.Update(context.Background(), func(tx store.WriteTx) error {
		if err := tx.Put([]byte("key"), []byte("value")); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Update() error = %v, want %v", err, wantErr)
	}

	err = s.View(context.Background(), func(tx store.ReadTx) error {
		_, err := tx.Get([]byte("key"))
		return err
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	s := openStore(t)
	if err := s.Update(context.Background(), func(tx store.WriteTx) error {
		return tx.Delete([]byte("missing"))
	}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestIteratePrefix(t *testing.T) {
	s := openStore(t)
	pairs := map[string]string{
		"alloc/a": "1",
		"alloc/b": "2",
		"other/c": "3",
	}

	if err := s.Update(context.Background(), func(tx store.WriteTx) error {
		for key, value := range pairs {
			if err := tx.Put([]byte(key), []byte(value)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got := map[string]string{}
	if err := s.View(context.Background(), func(tx store.ReadTx) error {
		return tx.Iterate([]byte("alloc/"), func(key, value []byte) error {
			got[string(key)] = string(value)
			return nil
		})
	}); err != nil {
		t.Fatalf("Iterate() error = %v", err)
	}

	if len(got) != 2 || got["alloc/a"] != "1" || got["alloc/b"] != "2" {
		t.Fatalf("Iterate() = %#v", got)
	}
}

func TestEmptyKeyIsInvalid(t *testing.T) {
	s := openStore(t)
	err := s.Update(context.Background(), func(tx store.WriteTx) error {
		return tx.Put(nil, []byte("value"))
	})
	if !errors.Is(err, store.ErrInvalidKey) {
		t.Fatalf("Put() error = %v, want ErrInvalidKey", err)
	}
}

func TestValueTooLarge(t *testing.T) {
	s := openStore(t, filestore.WithMaxValueSize(3))
	err := s.Update(context.Background(), func(tx store.WriteTx) error {
		return tx.Put([]byte("key"), []byte("four"))
	})
	if !errors.Is(err, store.ErrValueTooLarge) {
		t.Fatalf("Put() error = %v, want ErrValueTooLarge", err)
	}
}

func TestTransactionClosedAfterCallback(t *testing.T) {
	s := openStore(t)
	var leaked store.ReadTx

	if err := s.View(context.Background(), func(tx store.ReadTx) error {
		leaked = tx
		return nil
	}); err != nil {
		t.Fatalf("View() error = %v", err)
	}

	_, err := leaked.Get([]byte("key"))
	if !errors.Is(err, store.ErrTxClosed) {
		t.Fatalf("Get() error = %v, want ErrTxClosed", err)
	}
}

func TestSecondStoreIsLocked(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	first, err := filestore.Open(root)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	defer first.Close()

	second, err := filestore.Open(root)
	if second != nil {
		_ = second.Close()
	}
	if !errors.Is(err, store.ErrLocked) {
		t.Fatalf("second Open() error = %v, want ErrLocked", err)
	}
}

func TestClosedStore(t *testing.T) {
	s := openStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	err := s.View(context.Background(), func(store.ReadTx) error { return nil })
	if !errors.Is(err, store.ErrClosed) {
		t.Fatalf("View() error = %v, want ErrClosed", err)
	}
}
