//go:build linux

package nodeipam

import (
	"context"
	"net/netip"
	"testing"

	"github/setera/pkg/ipam/bitmap"
	filestore "github/setera/pkg/store/file"
)

func TestFileStoreRestore(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	firstStore, err := filestore.Open(root)
	if err != nil {
		t.Fatal(err)
	}

	firstAllocator, err := bitmap.New(
		netip.MustParsePrefix("10.244.0.0/29"),
	)
	if err != nil {
		t.Fatal(err)
	}

	first, err := New(
		firstAllocator,
		firstStore,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := first.Restore(ctx); err != nil {
		t.Fatal(err)
	}

	req := testRequest("container-a")

	want, err := first.Allocate(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if err := firstStore.Close(); err != nil {
		t.Fatal(err)
	}

	secondStore, err := filestore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()

	secondAllocator, err := bitmap.New(
		netip.MustParsePrefix("10.244.0.0/29"),
	)
	if err != nil {
		t.Fatal(err)
	}

	second, err := New(
		secondAllocator,
		secondStore,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := second.Restore(ctx); err != nil {
		t.Fatal(err)
	}

	got, ok := second.Get(req.Owner)
	if !ok {
		t.Fatal("restored allocation not found")
	}

	if got != want {
		t.Fatalf(
			"got %+v, want %+v",
			got,
			want,
		)
	}
}
