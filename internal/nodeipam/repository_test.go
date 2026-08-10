package nodeipam

import (
	"errors"
	"net/netip"
	"testing"
)

func TestAllocationKeyRoundTrip(t *testing.T) {
	tests := []string{
		"10.244.1.7",
		"fd00::7",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			addr := netip.MustParseAddr(value)

			key, err := allocationKey(addr)
			if err != nil {
				t.Fatal(err)
			}

			got, err := decodeAllocationKey(key)
			if err != nil {
				t.Fatal(err)
			}

			if got != addr {
				t.Fatalf(
					"got %s, want %s",
					got,
					addr,
				)
			}
		})
	}
}

func TestDecodeAllocationKeyRejectsInvalidKey(t *testing.T) {
	_, err := decodeAllocationKey(
		[]byte{
			allocationKeyPrefix,
			1,
			2,
		},
	)

	if !errors.Is(err, ErrCorruptState) {
		t.Fatalf(
			"expected ErrCorruptState, got %v",
			err,
		)
	}
}

func TestAllocationRecordRoundTrip(t *testing.T) {
	want := Allocation{
		IP: netip.MustParseAddr("10.244.1.7"),
		Owner: Owner{
			ContainerID: "container-a",
			IfName:      "eth0",
		},
		PodUID:   "pod-uid-a",
		TenantID: "tenant-a",
	}

	value, err := encodeAllocation(want)
	if err != nil {
		t.Fatal(err)
	}

	got, err := decodeAllocation(
		want.IP,
		value,
	)
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf(
			"got %+v, want %+v",
			got,
			want,
		)
	}
}
