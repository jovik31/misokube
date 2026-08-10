package tenantmeta

import (
	"net"
	"net/netip"
	"slices"
	"testing"
)

func TestNodeTenantLabel(t *testing.T) {
	got, err := NodeTenantLabel("tenant-a")
	if err != nil {
		t.Fatal(err)
	}

	if want := "setera.com/tenant.tenant-a"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNodeTenantLabelRejectsInvalidName(t *testing.T) {
	if _, err := NodeTenantLabel(""); err == nil {
		t.Fatal("expected error for empty tenant name")
	}
}

func TestTenants(t *testing.T) {
	labels := map[string]string{
		"setera.com/tenant.beta":  "true",
		"setera.com/tenant.alpha": "true",
		"setera.com/tenant.skip":  "false",
		"example.com/other":       "true",
	}

	got := Tenants(labels)
	want := []string{"alpha", "beta"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestVTEPMetadataRoundTrip(t *testing.T) {
	want := VTEP{
		IP:  netip.MustParseAddr("10.0.0.8"),
		MAC: mustMAC(t, "02:42:ac:11:00:08"),
	}

	labels, annotations, err := VTEPNodeMetadata(want)
	if err != nil {
		t.Fatal(err)
	}

	got, ready, err := VTEPFromMetadata(labels, annotations)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("expected VTEP to be ready")
	}
	if got.IP != want.IP || got.MAC.String() != want.MAC.String() {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestVTEPFromMetadataNotReady(t *testing.T) {
	_, ready, err := VTEPFromMetadata(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("expected VTEP not ready")
	}
}

func mustMAC(t *testing.T, value string) net.HardwareAddr {
	t.Helper()
	mac, err := net.ParseMAC(value)
	if err != nil {
		t.Fatal(err)
	}
	return mac
}
