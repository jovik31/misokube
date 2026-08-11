package noderouting

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"testing"

	"github/setera/pkg/network"
)

func TestVTEPAddress(t *testing.T) {
	got, err := VTEPAddress(netip.MustParsePrefix("10.244.7.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	if want := netip.MustParsePrefix("10.244.7.1/32"); got != want {
		t.Fatalf("VTEP = %s, want %s", got, want)
	}
}

func TestEnsureLocalCreatesVXLANAndAttachesNodeProgram(t *testing.T) {
	fake := newFakeNetwork(t)
	routing, err := New(fake, DefaultConfig(1450))
	if err != nil {
		t.Fatal(err)
	}

	attachCount := 0
	routing.attachNodeProgram = func(ifName string) (nodeProgram, error) {
		attachCount++
		if ifName != DefaultInterfaceName {
			t.Fatalf("attach interface = %q, want %q", ifName, DefaultInterfaceName)
		}
		return &fakeProgram{}, nil
	}

	publishCount := 0
	routing.setVXLANIfIndex = func(ifIndex int) error {
		publishCount++
		if ifIndex != 17 {
			t.Fatalf("published VXLAN ifindex = %d, want 17", ifIndex)
		}
		return nil
	}

	got, err := routing.EnsureLocal(netip.MustParsePrefix("10.244.1.0/24"), netip.MustParseAddr("172.18.0.2"))
	if err != nil {
		t.Fatal(err)
	}
	if got.VTEPIP != netip.MustParseAddr("10.244.1.1") {
		t.Fatalf("VTEP IP = %s, want 10.244.1.1", got.VTEPIP)
	}
	if fake.lastVXLAN.Address != netip.MustParsePrefix("10.244.1.1/32") {
		t.Fatalf("VXLAN address = %s, want 10.244.1.1/32", fake.lastVXLAN.Address)
	}
	if fake.lastVXLAN.UnderlayIP != netip.MustParseAddr("172.18.0.2") {
		t.Fatalf("VXLAN underlay = %s, want 172.18.0.2", fake.lastVXLAN.UnderlayIP)
	}
	if got.UnderlayIP != netip.MustParseAddr("172.18.0.2") {
		t.Fatalf("local underlay = %s, want 172.18.0.2", got.UnderlayIP)
	}
	if fake.lastVXLAN.MTU != 1450 {
		t.Fatalf("VXLAN MTU = %d, want 1450", fake.lastVXLAN.MTU)
	}

	if _, err := routing.EnsureLocal(netip.MustParsePrefix("10.244.1.0/24"), netip.MustParseAddr("172.18.0.2")); err != nil {
		t.Fatal(err)
	}
	if attachCount != 1 {
		t.Fatalf("node program attached %d times, want 1", attachCount)
	}
	if publishCount != 2 {
		t.Fatalf("VXLAN ifindex published %d times, want 2", publishCount)
	}
}

func TestReconcileRemoteNodesProgramsAndSweepsKernelState(t *testing.T) {
	fake := newFakeNetwork(t)
	routing, err := New(fake, DefaultConfig(1450))
	if err != nil {
		t.Fatal(err)
	}
	routing.attachNodeProgram = func(string) (nodeProgram, error) {
		return &fakeProgram{}, nil
	}
	routing.setVXLANIfIndex = func(int) error { return nil }
	if _, err := routing.EnsureLocal(netip.MustParsePrefix("10.244.1.0/24"), netip.MustParseAddr("172.18.0.2")); err != nil {
		t.Fatal(err)
	}

	staleMAC := mustMAC(t, "02:00:00:00:00:09")
	fake.routes = []network.Route{{
		Prefix:  netip.MustParsePrefix("10.244.9.0/24"),
		IfIndex: 17,
		Gateway: netip.MustParseAddr("10.244.9.1"),
		OnLink:  true,
	}}
	fake.neighbors = []network.Neighbor{{
		IfIndex: 17,
		IP:      netip.MustParseAddr("10.244.9.1"),
		MAC:     staleMAC,
	}}
	fake.fdb = []network.FDBEntry{{
		IfIndex:  17,
		RemoteIP: netip.MustParseAddr("172.18.0.9"),
		MAC:      staleMAC,
	}}

	remoteMAC := mustMAC(t, "02:00:00:00:00:02")
	remote := RemoteNode{
		Name:       "node-b",
		PodCIDR:    netip.MustParsePrefix("10.244.2.0/24"),
		UnderlayIP: netip.MustParseAddr("172.18.0.3"),
		VTEPIP:     netip.MustParseAddr("10.244.2.1"),
		VTEPMAC:    remoteMAC,
	}

	if err := routing.ReconcileRemoteNodes(context.Background(), []RemoteNode{remote}); err != nil {
		t.Fatal(err)
	}

	wantRoute := routeFor(17, remote)
	if len(fake.routes) != 1 || fake.routes[0].Prefix != wantRoute.Prefix || fake.routes[0].Gateway != wantRoute.Gateway || !fake.routes[0].OnLink {
		t.Fatalf("routes = %+v, want only %+v", fake.routes, wantRoute)
	}
	wantNeighbor := neighborFor(17, remote)
	if len(fake.neighbors) != 1 || fake.neighbors[0].IP != wantNeighbor.IP || fake.neighbors[0].MAC.String() != wantNeighbor.MAC.String() {
		t.Fatalf("neighbors = %+v, want only %+v", fake.neighbors, wantNeighbor)
	}
	wantFDB := fdbFor(17, remote)
	if len(fake.fdb) != 1 || fake.fdb[0].RemoteIP != wantFDB.RemoteIP || fake.fdb[0].MAC.String() != wantFDB.MAC.String() {
		t.Fatalf("FDB = %+v, want only %+v", fake.fdb, wantFDB)
	}

	wantPrefix := []string{"set-fdb", "set-neighbor", "replace-route"}
	if len(fake.calls) < len(wantPrefix) || !slices.Equal(fake.calls[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("programming order = %v, want prefix %v", fake.calls, wantPrefix)
	}
}

func TestReconcileRemoteNodesRejectsNonCanonicalVTEP(t *testing.T) {
	fake := newFakeNetwork(t)
	routing, err := New(fake, DefaultConfig(1450))
	if err != nil {
		t.Fatal(err)
	}
	routing.attachNodeProgram = func(string) (nodeProgram, error) { return &fakeProgram{}, nil }
	routing.setVXLANIfIndex = func(int) error { return nil }
	if _, err := routing.EnsureLocal(netip.MustParsePrefix("10.244.1.0/24"), netip.MustParseAddr("172.18.0.2")); err != nil {
		t.Fatal(err)
	}

	err = routing.ReconcileRemoteNodes(context.Background(), []RemoteNode{{
		Name:       "node-b",
		PodCIDR:    netip.MustParsePrefix("10.244.2.0/24"),
		UnderlayIP: netip.MustParseAddr("172.18.0.3"),
		VTEPIP:     netip.MustParseAddr("10.244.2.2"),
		VTEPMAC:    mustMAC(t, "02:00:00:00:00:02"),
	}})
	if err == nil {
		t.Fatal("expected non-canonical VTEP error")
	}
}

func TestEnsureLocalRejectsUnderlayChange(t *testing.T) {
	fake := newFakeNetwork(t)
	routing, err := New(fake, DefaultConfig(1450))
	if err != nil {
		t.Fatal(err)
	}
	routing.attachNodeProgram = func(string) (nodeProgram, error) { return &fakeProgram{}, nil }
	routing.setVXLANIfIndex = func(int) error { return nil }

	if _, err := routing.EnsureLocal(
		netip.MustParsePrefix("10.244.1.0/24"),
		netip.MustParseAddr("172.18.0.2"),
	); err != nil {
		t.Fatal(err)
	}

	if _, err := routing.EnsureLocal(
		netip.MustParsePrefix("10.244.1.0/24"),
		netip.MustParseAddr("172.18.0.99"),
	); err == nil {
		t.Fatal("expected underlay change to be rejected")
	}
}

func TestEnsureLocalFailsWhenVXLANIfIndexCannotBePublished(t *testing.T) {
	fake := newFakeNetwork(t)
	routing, err := New(fake, DefaultConfig(1450))
	if err != nil {
		t.Fatal(err)
	}
	routing.attachNodeProgram = func(string) (nodeProgram, error) {
		t.Fatal("node program must not attach before VXLAN ifindex publication succeeds")
		return nil, nil
	}
	routing.setVXLANIfIndex = func(int) error { return context.Canceled }

	if _, err := routing.EnsureLocal(
		netip.MustParsePrefix("10.244.1.0/24"),
		netip.MustParseAddr("172.18.0.2"),
	); err == nil {
		t.Fatal("expected VXLAN ifindex publication error")
	}
}

type fakeProgram struct{}

func (*fakeProgram) Close() error { return nil }

type fakeNetwork struct {
	t         *testing.T
	lastVXLAN network.VXLANConfig
	routes    []network.Route
	neighbors []network.Neighbor
	fdb       []network.FDBEntry
	calls     []string
}

func newFakeNetwork(t *testing.T) *fakeNetwork {
	return &fakeNetwork{t: t}
}

func (f *fakeNetwork) EnsureVXLAN(config network.VXLANConfig) (network.VXLANLink, error) {
	f.lastVXLAN = config
	return network.VXLANLink{
		Name:    config.Name,
		IfIndex: 17,
		MAC:     mustMAC(f.t, "02:00:00:00:01:01"),
	}, nil
}

func (f *fakeNetwork) ReplaceRoute(route network.Route) error {
	f.calls = append(f.calls, "replace-route")
	for i := range f.routes {
		if f.routes[i].Prefix == route.Prefix {
			f.routes[i] = route
			return nil
		}
	}
	f.routes = append(f.routes, route)
	return nil
}

func (f *fakeNetwork) DeleteRoute(route network.Route) error {
	f.calls = append(f.calls, "delete-route")
	for i := range f.routes {
		if f.routes[i].Prefix == route.Prefix {
			f.routes = append(f.routes[:i], f.routes[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeNetwork) ListRoutes(int) ([]network.Route, error) {
	return append([]network.Route(nil), f.routes...), nil
}

func (f *fakeNetwork) SetNeighbor(neighbor network.Neighbor) error {
	f.calls = append(f.calls, "set-neighbor")
	for i := range f.neighbors {
		if f.neighbors[i].IP == neighbor.IP {
			f.neighbors[i] = neighbor
			return nil
		}
	}
	f.neighbors = append(f.neighbors, neighbor)
	return nil
}

func (f *fakeNetwork) DeleteNeighbor(neighbor network.Neighbor) error {
	f.calls = append(f.calls, "delete-neighbor")
	for i := range f.neighbors {
		if f.neighbors[i].IP == neighbor.IP {
			f.neighbors = append(f.neighbors[:i], f.neighbors[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeNetwork) ListNeighbors(int) ([]network.Neighbor, error) {
	return append([]network.Neighbor(nil), f.neighbors...), nil
}

func (f *fakeNetwork) SetFDB(entry network.FDBEntry) error {
	f.calls = append(f.calls, "set-fdb")
	for i := range f.fdb {
		if f.fdb[i].MAC.String() == entry.MAC.String() {
			f.fdb[i] = entry
			return nil
		}
	}
	f.fdb = append(f.fdb, entry)
	return nil
}

func (f *fakeNetwork) DeleteFDB(entry network.FDBEntry) error {
	f.calls = append(f.calls, "delete-fdb")
	for i := range f.fdb {
		if f.fdb[i].MAC.String() == entry.MAC.String() && f.fdb[i].RemoteIP == entry.RemoteIP {
			f.fdb = append(f.fdb[:i], f.fdb[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeNetwork) ListFDB(int) ([]network.FDBEntry, error) {
	return append([]network.FDBEntry(nil), f.fdb...), nil
}

func mustMAC(t *testing.T, value string) net.HardwareAddr {
	t.Helper()
	mac, err := net.ParseMAC(value)
	if err != nil {
		t.Fatal(err)
	}
	return mac
}