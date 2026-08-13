package loader

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestTcPodIDsGoValueSizeMatchesBPFMapValueSize(t *testing.T) {
	got := binary.Size(tcPodIDValue{})
	if got < 0 {
		t.Fatal("binary.Size(tcPodIDValue{}) returned a negative size")
	}

	if uint32(got) != tcPodIDsValueSize {
		t.Fatalf(
			"tcPodIDValue size = %d, want %d",
			got,
			tcPodIDsValueSize,
		)
	}
}

func TestTcRouterDeclaresSharedVXLANIfIndexMap(t *testing.T) {
	spec, err := loadTcFirewall()
	if err != nil {
		t.Fatalf("load TC router spec: %v", err)
	}

	got := spec.Maps["tc_vxlan_ifindex"]
	if got == nil {
		t.Fatal("TC router does not declare tc_vxlan_ifindex")
	}

	want := tcVXLANIfIndexMapSpec()
	if got.Type != want.Type ||
		got.KeySize != want.KeySize ||
		got.ValueSize != want.ValueSize ||
		got.MaxEntries != want.MaxEntries ||
		got.Flags != want.Flags {
		t.Fatalf(
			"tc_vxlan_ifindex = %s, want %s",
			got,
			want,
		)
	}
}

func TestTcRouterDeclaresPacketServiceMaps(t *testing.T) {
	spec, err := loadTcFirewall()
	if err != nil {
		t.Fatalf("load TC router spec: %v", err)
	}

	want := map[string]*ebpf.MapSpec{
		"svc_frontend":   serviceFrontendMapSpec(),
		"svc_backend":    serviceBackendMapSpec(),
		"svc_pkt_flow":   servicePacketFlowMapSpec(),
		"svc_pkt_revnat": servicePacketRevNatMapSpec(),
		"svc_pkt_stats":  servicePacketStatsMapSpec(),
	}

	for name, expected := range want {
		got := spec.Maps[name]
		if got == nil {
			t.Fatalf("TC router does not declare %s", name)
		}

		if got.Type != expected.Type ||
			got.KeySize != expected.KeySize ||
			got.ValueSize != expected.ValueSize ||
			got.MaxEntries != expected.MaxEntries ||
			got.Flags != expected.Flags {
			t.Fatalf(
				"%s = %s, want %s",
				name,
				got,
				expected,
			)
		}
	}
}

func TestTcProgramsDeclareCompatibleSharedPodMap(t *testing.T) {
	tcSpec, err := loadTcFirewall()
	if err != nil {
		t.Fatalf("load TC router spec: %v", err)
	}

	nodeSpec, err := loadNodeRouter()
	if err != nil {
		t.Fatalf("load node router spec: %v", err)
	}

	tcMap := tcSpec.Maps["tc_podIDs"]
	if tcMap == nil {
		t.Fatal("TC router does not declare tc_podIDs")
	}

	nodeMap := nodeSpec.Maps["tc_podIDs"]
	if nodeMap == nil {
		t.Fatal("node router does not declare tc_podIDs")
	}

	assertTcPodIDsMapSpec(t, "TC router", tcMap)
	assertTcPodIDsMapSpec(t, "node router", nodeMap)

	if tcMap.Type != nodeMap.Type ||
		tcMap.KeySize != nodeMap.KeySize ||
		tcMap.ValueSize != nodeMap.ValueSize ||
		tcMap.MaxEntries != nodeMap.MaxEntries ||
		tcMap.Flags != nodeMap.Flags {
		t.Fatalf(
			"tc_podIDs specs differ: TC=%s node=%s",
			tcMap,
			nodeMap,
		)
	}
}

func TestEnsureTcPodIDsSpecDisablesELFPinning(t *testing.T) {
	spec, err := loadTcFirewall()
	if err != nil {
		t.Fatal(err)
	}

	if err := prepareTcCollectionSpec(
		spec,
		"tc router",
		true,
		true,
	); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"tc_podIDs",
		"tc_vxlan_ifindex",
		"tc_iface_cfg",
		"tc_stats",
		"svc_frontend",
		"svc_backend",
		"svc_pkt_flow",
		"svc_pkt_revnat",
		"svc_pkt_stats",
	} {
		mapSpec := spec.Maps[name]
		if mapSpec == nil {
			t.Fatalf("map %s is missing", name)
		}

		if got := mapSpec.Pinning; got != ebpf.PinNone {
			t.Fatalf(
				"%s pinning = %v, want PinNone",
				name,
				got,
			)
		}
	}
}

func TestDeleteTCFilterDeletesOnlyGivenFilter(t *testing.T) {
	want := &netlink.BpfFilter{}

	var got netlink.Filter
	err := deleteTCFilter(
		want,
		func(filter netlink.Filter) error {
			got = filter
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf(
			"deleted filter = %T %v, want supplied filter %p",
			got,
			got,
			want,
		)
	}
}

func TestDeleteTCFilterTreatsMissingFilterAsDetached(t *testing.T) {
	tests := []error{
		unix.ENOENT,
		unix.ENODEV,
	}

	for _, wantErr := range tests {
		t.Run(wantErr.Error(), func(t *testing.T) {
			err := deleteTCFilter(
				&netlink.BpfFilter{},
				func(netlink.Filter) error {
					return wantErr
				},
			)
			if err != nil {
				t.Fatalf(
					"deleteTCFilter() error = %v, want nil",
					err,
				)
			}
		})
	}
}

func TestDeleteTCFilterReturnsUnexpectedError(t *testing.T) {
	wantErr := errors.New("filter delete failed")

	err := deleteTCFilter(
		&netlink.BpfFilter{},
		func(netlink.Filter) error {
			return wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf(
			"deleteTCFilter() error = %v, want wrapped %v",
			err,
			wantErr,
		)
	}
}

func TestNewTCBpfFilter(t *testing.T) {
	filter := newTCBpfFilter(
		42,
		netlink.HANDLE_MIN_INGRESS,
		100,
		"setera_test",
	)

	if filter.Attrs().LinkIndex != 42 {
		t.Fatalf(
			"LinkIndex = %d, want 42",
			filter.Attrs().LinkIndex,
		)
	}

	if filter.Attrs().Parent != netlink.HANDLE_MIN_INGRESS {
		t.Fatalf(
			"Parent = %#x, want %#x",
			filter.Attrs().Parent,
			netlink.HANDLE_MIN_INGRESS,
		)
	}

	if filter.Fd != 100 {
		t.Fatalf(
			"Fd = %d, want 100",
			filter.Fd,
		)
	}

	if filter.Name != "setera_test" {
		t.Fatalf(
			"Name = %q, want %q",
			filter.Name,
			"setera_test",
		)
	}

	if !filter.DirectAction {
		t.Fatal("DirectAction = false, want true")
	}
}

func TestTcRouterDeclaresSourceDefaultVariable(t *testing.T) {
	spec, err := loadTcFirewall()
	if err != nil {
		t.Fatalf("load TC router spec: %v", err)
	}

	if variable := spec.Variables["my_is_default"]; variable == nil {
		t.Fatal("TC router does not declare my_is_default")
	}
}

func TestTcFirewallTenantConstants(t *testing.T) {
	tests := []struct {
		name          string
		tenant        string
		wantTenant    string
		wantIsDefault uint32
	}{
		{
			name:          "default tenant",
			tenant:        "default",
			wantTenant:    "default",
			wantIsDefault: 1,
		},
		{
			name:          "explicit tenant",
			tenant:        "tenant-a",
			wantTenant:    "tenant-a",
			wantIsDefault: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenantBytes, isDefault := tcFirewallTenantConstants(
				tt.tenant,
			)

			gotTenant := string(
				tenantBytes[:len(tt.wantTenant)],
			)
			if gotTenant != tt.wantTenant {
				t.Fatalf(
					"tenant bytes = %q, want %q",
					gotTenant,
					tt.wantTenant,
				)
			}

			if tenantBytes[len(tt.wantTenant)] != 0 {
				t.Fatalf(
					"tenant bytes are not NUL-terminated after %q",
					tt.wantTenant,
				)
			}

			if isDefault != tt.wantIsDefault {
				t.Fatalf(
					"isDefault = %d, want %d",
					isDefault,
					tt.wantIsDefault,
				)
			}
		})
	}
}

func assertTcPodIDsMapSpec(
	t *testing.T,
	name string,
	got *ebpf.MapSpec,
) {
	t.Helper()

	want := tcPodIDsMapSpec()

	if got.Type != want.Type ||
		got.KeySize != want.KeySize ||
		got.ValueSize != want.ValueSize ||
		got.MaxEntries != want.MaxEntries ||
		got.Flags != want.Flags {
		t.Fatalf(
			"%s tc_podIDs = %s, want %s",
			name,
			got,
			want,
		)
	}
}
