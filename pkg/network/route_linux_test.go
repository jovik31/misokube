//go:build linux

package network

import (
	"errors"
	"net"
	"net/netip"
	"syscall"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestReplaceRoute(t *testing.T) {
	oldReplace := routeReplace
	defer func() { routeReplace = oldReplace }()

	var got *netlink.Route
	routeReplace = func(route *netlink.Route) error {
		copy := *route
		got = &copy
		return nil
	}

	err := NewLinux().ReplaceRoute(Route{
		Prefix:  netip.MustParsePrefix("10.245.0.0/24"),
		IfIndex: 17,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got == nil {
		t.Fatal("route was not programmed")
	}
	if got.LinkIndex != 17 {
		t.Fatalf("ifindex = %d, want 17", got.LinkIndex)
	}
	if got.Dst.String() != "10.245.0.0/24" {
		t.Fatalf("prefix = %s, want 10.245.0.0/24", got.Dst)
	}
	if got.Gw != nil {
		t.Fatalf("gateway = %s, want none", got.Gw)
	}
	if got.Scope != netlink.SCOPE_LINK {
		t.Fatalf("scope = %d, want link scope", got.Scope)
	}
}

func TestReplaceRouteWithGateway(t *testing.T) {
	oldReplace := routeReplace
	defer func() { routeReplace = oldReplace }()

	var got *netlink.Route
	routeReplace = func(route *netlink.Route) error {
		copy := *route
		got = &copy
		return nil
	}

	err := NewLinux().ReplaceRoute(Route{
		Prefix:  netip.MustParsePrefix("10.245.0.0/24"),
		IfIndex: 17,
		Gateway: netip.MustParseAddr("192.0.2.2"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Gw.String() != "192.0.2.2" {
		t.Fatalf("gateway = %s, want 192.0.2.2", got.Gw)
	}
	if got.Scope != netlink.SCOPE_UNIVERSE {
		t.Fatalf("scope = %d, want universe scope", got.Scope)
	}
}

func TestDeleteRouteIsIdempotent(t *testing.T) {
	oldDelete := routeDelete
	defer func() { routeDelete = oldDelete }()

	routeDelete = func(*netlink.Route) error {
		return syscall.ESRCH
	}

	err := NewLinux().DeleteRoute(Route{
		Prefix:  netip.MustParsePrefix("10.245.0.0/24"),
		IfIndex: 17,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRouteValidation(t *testing.T) {
	tests := []Route{
		{},
		{Prefix: netip.MustParsePrefix("10.245.0.0/24")},
		{
			Prefix:  netip.MustParsePrefix("10.245.0.0/24"),
			IfIndex: 17,
			Gateway: netip.MustParseAddr("2001:db8::1"),
		},
	}

	for i, route := range tests {
		if err := NewLinux().ReplaceRoute(route); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestSetNeighbor(t *testing.T) {
	oldSet := neighborSet
	defer func() { neighborSet = oldSet }()

	var got *netlink.Neigh
	neighborSet = func(neigh *netlink.Neigh) error {
		copy := *neigh
		got = &copy
		return nil
	}

	mac, err := net.ParseMAC("02:00:00:00:00:02")
	if err != nil {
		t.Fatal(err)
	}

	err = NewLinux().SetNeighbor(Neighbor{
		IfIndex: 21,
		IP:      netip.MustParseAddr("10.245.0.2"),
		MAC:     mac,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got == nil {
		t.Fatal("neighbor was not programmed")
	}
	if got.LinkIndex != 21 {
		t.Fatalf("ifindex = %d, want 21", got.LinkIndex)
	}
	if got.IP.String() != "10.245.0.2" {
		t.Fatalf("IP = %s, want 10.245.0.2", got.IP)
	}
	if got.State != netlink.NUD_PERMANENT {
		t.Fatalf("state = %d, want permanent", got.State)
	}
}

func TestDeleteNeighborIsIdempotent(t *testing.T) {
	oldDelete := neighborDelete
	defer func() { neighborDelete = oldDelete }()

	neighborDelete = func(*netlink.Neigh) error {
		return syscall.ENOENT
	}

	err := NewLinux().DeleteNeighbor(Neighbor{
		IfIndex: 21,
		IP:      netip.MustParseAddr("10.245.0.2"),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNeighborValidation(t *testing.T) {
	err := NewLinux().SetNeighbor(Neighbor{
		IfIndex: 21,
		IP:      netip.MustParseAddr("10.245.0.2"),
	})
	if err == nil {
		t.Fatal("expected missing MAC error")
	}
}

func TestSetFDB(t *testing.T) {
	oldSet := neighborSet
	defer func() { neighborSet = oldSet }()

	var got *netlink.Neigh
	neighborSet = func(neigh *netlink.Neigh) error {
		copy := *neigh
		got = &copy
		return nil
	}

	mac, err := net.ParseMAC("02:00:00:00:00:03")
	if err != nil {
		t.Fatal(err)
	}

	err = NewLinux().SetFDB(FDBEntry{
		IfIndex:  30,
		RemoteIP: netip.MustParseAddr("192.0.2.20"),
		MAC:      mac,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got == nil {
		t.Fatal("FDB entry was not programmed")
	}
	if got.LinkIndex != 30 {
		t.Fatalf("ifindex = %d, want 30", got.LinkIndex)
	}
	if got.Family != syscall.AF_BRIDGE {
		t.Fatalf("family = %d, want AF_BRIDGE", got.Family)
	}
	if got.Flags != netlink.NTF_SELF {
		t.Fatalf("flags = %d, want NTF_SELF", got.Flags)
	}
}

func TestDeleteFDBReturnsRealErrors(t *testing.T) {
	oldDelete := neighborDelete
	defer func() { neighborDelete = oldDelete }()

	want := errors.New("delete failed")
	neighborDelete = func(*netlink.Neigh) error {
		return want
	}

	mac, err := net.ParseMAC("02:00:00:00:00:03")
	if err != nil {
		t.Fatal(err)
	}

	err = NewLinux().DeleteFDB(FDBEntry{
		IfIndex:  30,
		RemoteIP: netip.MustParseAddr("192.0.2.20"),
		MAC:      mac,
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want wrapped delete error", err)
	}
}
