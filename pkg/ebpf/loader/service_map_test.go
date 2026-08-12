package loader

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/cilium/ebpf"
)

func TestServiceMapABI(t *testing.T) {
	if got, want := binary.Size(serviceFrontendKey{}), 8; got != want {
		t.Fatalf("frontend key size = %d, want %d", got, want)
	}
	if got, want := binary.Size(serviceFrontendValue{}), 8; got != want {
		t.Fatalf("frontend value size = %d, want %d", got, want)
	}
	if got, want := binary.Size(serviceBackendKey{}), 12; got != want {
		t.Fatalf("backend key size = %d, want %d", got, want)
	}
	if got, want := binary.Size(serviceBackendValue{}), 72; got != want {
		t.Fatalf("backend value size = %d, want %d", got, want)
	}
	if got, want := binary.Size(serviceSocketRevNatKey{}), 16; got != want {
		t.Fatalf("socket revnat key size = %d, want %d", got, want)
	}
	if got, want := binary.Size(serviceSocketRevNatValue{}), 8; got != want {
		t.Fatalf("socket revnat value size = %d, want %d", got, want)
	}
}

func TestServiceSocketStatsMapSpec(t *testing.T) {
	spec := serviceSocketStatsMapSpec()

	if spec.Type != ebpf.PerCPUArray {
		t.Fatalf("socket stats map type = %v, want PerCPUArray", spec.Type)
	}
	if spec.KeySize != 4 {
		t.Fatalf("socket stats key size = %d, want 4", spec.KeySize)
	}
	if spec.ValueSize != 8 {
		t.Fatalf("socket stats value size = %d, want 8", spec.ValueSize)
	}
	if spec.MaxEntries != serviceSocketStatsMaxEntries {
		t.Fatalf(
			"socket stats max entries = %d, want %d",
			spec.MaxEntries,
			serviceSocketStatsMaxEntries,
		)
	}
}

func TestEncodeServiceKeyUsesNetworkByteOrder(t *testing.T) {
	got, err := encodeServiceKey(ServiceKey{
		IP:       net.ParseIP("10.96.0.10"),
		Port:     0x1234,
		Protocol: 17,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Address != [4]byte{10, 96, 0, 10} {
		t.Fatalf("address = %v", got.Address)
	}
	if got.Port != [2]byte{0x12, 0x34} {
		t.Fatalf("port bytes = %v, want network byte order", got.Port)
	}
	if got.Protocol != 17 {
		t.Fatalf("protocol = %d, want 17", got.Protocol)
	}
}

func TestEncodeServiceBackendManagedPod(t *testing.T) {
	got, err := encodeServiceBackend(ServiceBackend{
		IP:         net.ParseIP("10.244.1.10"),
		Port:       8080,
		Tenant:     "tenant-a",
		ManagedPod: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Address != [4]byte{10, 244, 1, 10} {
		t.Fatalf("address = %v", got.Address)
	}
	if got.Port != [2]byte{0x1f, 0x90} {
		t.Fatalf("port bytes = %v, want 8080 in network byte order", got.Port)
	}
	if got.Flags&serviceBackendFlagManagedPod == 0 {
		t.Fatal("managed Pod flag is not set")
	}
	if string(got.Tenant[:8]) != "tenant-a" {
		t.Fatalf("tenant bytes = %q", got.Tenant[:8])
	}
}

func TestEncodeServiceBackendExternal(t *testing.T) {
	got, err := encodeServiceBackend(ServiceBackend{
		IP:   net.ParseIP("192.0.2.10"),
		Port: 443,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Flags&serviceBackendFlagManagedPod != 0 {
		t.Fatal("external backend was marked as managed Pod")
	}
	if got.Tenant != [64]byte{} {
		t.Fatalf("external backend tenant = %q, want empty", got.Tenant)
	}
}
