package loader

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/cilium/ebpf"
)

func TestServiceMapABI(t *testing.T) {
	if got, want := binary.Size(serviceFrontendKey{}), 8; got != want {
		t.Fatalf(
			"frontend key size = %d, want %d",
			got,
			want,
		)
	}

	if got, want := binary.Size(serviceFrontendValue{}), 8; got != want {
		t.Fatalf(
			"frontend value size = %d, want %d",
			got,
			want,
		)
	}

	if got, want := binary.Size(serviceBackendKey{}), 12; got != want {
		t.Fatalf(
			"backend key size = %d, want %d",
			got,
			want,
		)
	}

	if got, want := binary.Size(serviceBackendValue{}), 72; got != want {
		t.Fatalf(
			"backend value size = %d, want %d",
			got,
			want,
		)
	}

	if got, want := binary.Size(serviceSocketRevNatKey{}), 16; got != want {
		t.Fatalf(
			"socket revnat key size = %d, want %d",
			got,
			want,
		)
	}

	if got, want := binary.Size(serviceSocketRevNatValue{}), 8; got != want {
		t.Fatalf(
			"socket revnat value size = %d, want %d",
			got,
			want,
		)
	}

	if got, want := binary.Size(servicePacketFlowKey{}), 16; got != want {
		t.Fatalf(
			"packet flow key size = %d, want %d",
			got,
			want,
		)
	}

	if got, want := binary.Size(servicePacketFlowValue{}), 16; got != want {
		t.Fatalf(
			"packet flow value size = %d, want %d",
			got,
			want,
		)
	}

	if got, want := binary.Size(servicePacketRevNatKey{}), 16; got != want {
		t.Fatalf(
			"packet revnat key size = %d, want %d",
			got,
			want,
		)
	}

	if got, want := binary.Size(servicePacketRevNatValue{}), 16; got != want {
		t.Fatalf(
			"packet revnat value size = %d, want %d",
			got,
			want,
		)
	}
}

func TestServiceSocketStatsMapSpec(t *testing.T) {
	spec := serviceSocketStatsMapSpec()

	if spec.Type != ebpf.PerCPUArray {
		t.Fatalf(
			"socket stats map type = %v, want PerCPUArray",
			spec.Type,
		)
	}

	if spec.KeySize != 4 {
		t.Fatalf(
			"socket stats key size = %d, want 4",
			spec.KeySize,
		)
	}

	if spec.ValueSize != 8 {
		t.Fatalf(
			"socket stats value size = %d, want 8",
			spec.ValueSize,
		)
	}

	if spec.MaxEntries != serviceSocketStatsMaxEntries {
		t.Fatalf(
			"socket stats max entries = %d, want %d",
			spec.MaxEntries,
			serviceSocketStatsMaxEntries,
		)
	}
}

func TestServicePacketMapSpecs(t *testing.T) {
	flow := servicePacketFlowMapSpec()

	if flow.Type != ebpf.LRUHash {
		t.Fatalf(
			"packet flow map type = %v, want LRUHash",
			flow.Type,
		)
	}

	if flow.KeySize != 16 ||
		flow.ValueSize != 16 {
		t.Fatalf(
			"packet flow map ABI = key %d value %d, want key 16 value 16",
			flow.KeySize,
			flow.ValueSize,
		)
	}

	if flow.MaxEntries != servicePacketFlowMaxEntries {
		t.Fatalf(
			"packet flow max entries = %d, want %d",
			flow.MaxEntries,
			servicePacketFlowMaxEntries,
		)
	}

	revNAT := servicePacketRevNatMapSpec()

	if revNAT.Type != ebpf.LRUHash {
		t.Fatalf(
			"packet revnat map type = %v, want LRUHash",
			revNAT.Type,
		)
	}

	if revNAT.KeySize != 16 ||
		revNAT.ValueSize != 16 {
		t.Fatalf(
			"packet revnat map ABI = key %d value %d, want key 16 value 16",
			revNAT.KeySize,
			revNAT.ValueSize,
		)
	}

	if revNAT.MaxEntries != servicePacketRevNatMaxEntries {
		t.Fatalf(
			"packet revnat max entries = %d, want %d",
			revNAT.MaxEntries,
			servicePacketRevNatMaxEntries,
		)
	}

	stats := servicePacketStatsMapSpec()

	if stats.Type != ebpf.PerCPUArray {
		t.Fatalf(
			"packet stats map type = %v, want PerCPUArray",
			stats.Type,
		)
	}

	if stats.KeySize != 4 ||
		stats.ValueSize != 8 {
		t.Fatalf(
			"packet stats map ABI = key %d value %d, want key 4 value 8",
			stats.KeySize,
			stats.ValueSize,
		)
	}

	if stats.MaxEntries != servicePacketStatsMaxEntries {
		t.Fatalf(
			"packet stats max entries = %d, want %d",
			stats.MaxEntries,
			servicePacketStatsMaxEntries,
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
		t.Fatalf(
			"address = %v",
			got.Address,
		)
	}

	if got.Port != [2]byte{0x12, 0x34} {
		t.Fatalf(
			"port bytes = %v, want network byte order",
			got.Port,
		)
	}

	if got.Protocol != 17 {
		t.Fatalf(
			"protocol = %d, want 17",
			got.Protocol,
		)
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
		t.Fatalf(
			"address = %v",
			got.Address,
		)
	}

	if got.Port != [2]byte{0x1f, 0x90} {
		t.Fatalf(
			"port bytes = %v, want 8080 in network byte order",
			got.Port,
		)
	}

	if got.Flags&serviceBackendFlagManagedPod == 0 {
		t.Fatal(
			"managed Pod flag is not set",
		)
	}

	if string(got.Tenant[:8]) != "tenant-a" {
		t.Fatalf(
			"tenant bytes = %q",
			got.Tenant[:8],
		)
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
		t.Fatal(
			"external backend was marked as managed Pod",
		)
	}

	if got.Tenant != [64]byte{} {
		t.Fatalf(
			"external backend tenant = %q, want empty",
			got.Tenant,
		)
	}
}
