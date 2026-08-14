
package ebpf

import (
	"errors"
	"testing"
)

func TestSetVXLANIfIndex(t *testing.T) {
	called := 0
	err := setVXLANIfIndex(17, func(ifIndex int) error {
		called++
		if ifIndex != 17 {
			t.Fatalf("ifindex = %d, want 17", ifIndex)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("writer called %d times, want 1", called)
	}
}

func TestSetVXLANIfIndexRejectsInvalidIndex(t *testing.T) {
	if err := setVXLANIfIndex(0, func(int) error { return nil }); err == nil {
		t.Fatal("expected invalid ifindex error")
	}
}

func TestSetVXLANIfIndexWrapsWriterError(t *testing.T) {
	want := errors.New("write failed")
	err := setVXLANIfIndex(17, func(int) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want wrapped %v", err, want)
	}
}