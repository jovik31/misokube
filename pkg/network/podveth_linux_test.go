//go:build linux

package network

import (
	"errors"
	"net/netip"
	"syscall"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestFindPodVeth(t *testing.T) {
	oldRoutes := routeListFiltered
	oldLink := linkByIndex
	defer func() {
		routeListFiltered = oldRoutes
		linkByIndex = oldLink
	}()

	podIP := netip.MustParseAddr("10.244.1.10")

	routeListFiltered = func(
		family int,
		filter *netlink.Route,
		mask uint64,
	) ([]netlink.Route, error) {
		if family != netlink.FAMILY_V4 {
			t.Fatalf("family = %d, want FAMILY_V4", family)
		}
		if filter == nil || filter.Table != unix.RT_TABLE_MAIN {
			t.Fatalf("route filter = %+v, want main table", filter)
		}
		if mask != netlink.RT_FILTER_TABLE {
			t.Fatalf("filter mask = %#x, want RT_FILTER_TABLE", mask)
		}

		return []netlink.Route{
			{
				Dst:       prefixToIPNet(netip.MustParsePrefix("10.244.0.0/16")),
				LinkIndex: 9,
				Scope:     netlink.SCOPE_LINK,
				Table:     unix.RT_TABLE_MAIN,
			},
			{
				Dst:       prefixToIPNet(netip.PrefixFrom(podIP, 32)),
				LinkIndex: 42,
				Scope:     netlink.SCOPE_LINK,
				Table:     unix.RT_TABLE_MAIN,
			},
		}, nil
	}

	linkByIndex = func(index int) (netlink.Link, error) {
		if index != 42 {
			t.Fatalf("ifindex = %d, want 42", index)
		}
		return &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  "veth-host",
				Index: 42,
			},
		}, nil
	}

	got, err := NewLinux().FindPodVeth(podIP)
	if err != nil {
		t.Fatal(err)
	}

	if got.HostName != "veth-host" {
		t.Fatalf("host name = %q, want veth-host", got.HostName)
	}
	if got.HostIfIndex != 42 {
		t.Fatalf("host ifindex = %d, want 42", got.HostIfIndex)
	}
}

func TestFindPodVethRejectsDefaultOrRemoteRoute(t *testing.T) {
	oldRoutes := routeListFiltered
	defer func() { routeListFiltered = oldRoutes }()

	podIP := netip.MustParseAddr("10.244.2.10")

	routeListFiltered = func(
		int,
		*netlink.Route,
		uint64,
	) ([]netlink.Route, error) {
		return []netlink.Route{
			{
				Dst:       prefixToIPNet(netip.MustParsePrefix("10.244.0.0/16")),
				LinkIndex: 2,
				Scope:     netlink.SCOPE_LINK,
				Table:     unix.RT_TABLE_MAIN,
			},
			{
				Dst:       prefixToIPNet(netip.PrefixFrom(podIP, 32)),
				LinkIndex: 8,
				Gw:        addrToNetIP(netip.MustParseAddr("192.0.2.2")),
				Scope:     netlink.SCOPE_UNIVERSE,
				Table:     unix.RT_TABLE_MAIN,
			},
		}, nil
	}

	_, err := NewLinux().FindPodVeth(podIP)
	if !errors.Is(err, ErrPodVethNotFound) {
		t.Fatalf("error = %v, want ErrPodVethNotFound", err)
	}
}

func TestFindPodVethRejectsNonVethInterface(t *testing.T) {
	oldRoutes := routeListFiltered
	oldLink := linkByIndex
	defer func() {
		routeListFiltered = oldRoutes
		linkByIndex = oldLink
	}()

	podIP := netip.MustParseAddr("10.244.1.10")

	routeListFiltered = func(
		int,
		*netlink.Route,
		uint64,
	) ([]netlink.Route, error) {
		return []netlink.Route{{
			Dst:       prefixToIPNet(netip.PrefixFrom(podIP, 32)),
			LinkIndex: 42,
			Scope:     netlink.SCOPE_LINK,
			Table:     unix.RT_TABLE_MAIN,
		}}, nil
	}

	linkByIndex = func(int) (netlink.Link, error) {
		return &netlink.Dummy{
			LinkAttrs: netlink.LinkAttrs{
				Name:  "dummy0",
				Index: 42,
			},
		}, nil
	}

	_, err := NewLinux().FindPodVeth(podIP)
	if !errors.Is(err, ErrPodVethNotFound) {
		t.Fatalf("error = %v, want ErrPodVethNotFound", err)
	}
}

func TestFindPodVethMissingInterface(t *testing.T) {
	oldRoutes := routeListFiltered
	oldLink := linkByIndex
	defer func() {
		routeListFiltered = oldRoutes
		linkByIndex = oldLink
	}()

	podIP := netip.MustParseAddr("10.244.1.10")

	routeListFiltered = func(
		int,
		*netlink.Route,
		uint64,
	) ([]netlink.Route, error) {
		return []netlink.Route{{
			Dst:       prefixToIPNet(netip.PrefixFrom(podIP, 32)),
			LinkIndex: 42,
			Scope:     netlink.SCOPE_LINK,
			Table:     unix.RT_TABLE_MAIN,
		}}, nil
	}

	linkByIndex = func(int) (netlink.Link, error) {
		return nil, syscall.ENODEV
	}

	_, err := NewLinux().FindPodVeth(podIP)
	if !errors.Is(err, ErrPodVethNotFound) {
		t.Fatalf("error = %v, want ErrPodVethNotFound", err)
	}
}

func TestFindPodVethRejectsAmbiguousRoutes(t *testing.T) {
	oldRoutes := routeListFiltered
	defer func() { routeListFiltered = oldRoutes }()

	podIP := netip.MustParseAddr("10.244.1.10")
	route := netlink.Route{
		Dst:       prefixToIPNet(netip.PrefixFrom(podIP, 32)),
		LinkIndex: 42,
		Scope:     netlink.SCOPE_LINK,
		Table:     unix.RT_TABLE_MAIN,
	}

	routeListFiltered = func(
		int,
		*netlink.Route,
		uint64,
	) ([]netlink.Route, error) {
		return []netlink.Route{route, route}, nil
	}

	if _, err := NewLinux().FindPodVeth(podIP); err == nil {
		t.Fatal("expected ambiguous-route error")
	}
}

func TestFindPodVethWrapsRouteListError(t *testing.T) {
	oldRoutes := routeListFiltered
	defer func() { routeListFiltered = oldRoutes }()

	wantErr := errors.New("route list failed")
	routeListFiltered = func(
		int,
		*netlink.Route,
		uint64,
	) ([]netlink.Route, error) {
		return nil, wantErr
	}

	_, err := NewLinux().FindPodVeth(
		netip.MustParseAddr("10.244.1.10"),
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
}
