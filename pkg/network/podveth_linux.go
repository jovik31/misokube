//go:build linux

package network

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var (
	// ErrPodVethNotFound means the kernel no longer has the local /32 route and
	// veth that Setera created for the Pod.
	ErrPodVethNotFound = errors.New("network: Pod veth not found")

	linkByIndex = netlink.LinkByIndex
)

// FindPodVeth rediscovers the host side of an existing local Pod veth from the
// /32 route installed by SetupVeth.
//
// Setera deliberately does not persist the host-veth name or ifindex. Both are
// kernel-runtime state and can be recovered from the route:
//
//	podIP/32 -> host veth ifindex -> host veth name
//
// Only a directly connected, link-scope /32 route in the main table that
// points to a veth is accepted. A default route or a remote-node route must
// never be mistaken for a local Pod veth.
func (n *Linux) FindPodVeth(podIP netip.Addr) (Veth, error) {
	podIP = podIP.Unmap()
	if !podIP.IsValid() || !podIP.Is4() || podIP.Zone() != "" {
		return Veth{}, fmt.Errorf(
			"network: invalid IPv4 Pod address %s",
			podIP,
		)
	}

	routes, err := routeListFiltered(
		netlink.FAMILY_V4,
		&netlink.Route{Table: unix.RT_TABLE_MAIN},
		netlink.RT_FILTER_TABLE,
	)
	if err != nil {
		return Veth{}, fmt.Errorf(
			"network: list main-table IPv4 routes for Pod %s: %w",
			podIP,
			err,
		)
	}

	var route *netlink.Route
	for i := range routes {
		if !isLocalPodHostRoute(routes[i], podIP) {
			continue
		}

		if route != nil {
			return Veth{}, fmt.Errorf(
				"network: multiple local /32 routes found for Pod %s",
				podIP,
			)
		}

		copy := routes[i]
		route = &copy
	}

	if route == nil {
		return Veth{}, fmt.Errorf(
			"%w: no local /32 route for %s",
			ErrPodVethNotFound,
			podIP,
		)
	}

	link, err := linkByIndex(route.LinkIndex)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) ||
			errors.Is(err, syscall.ENODEV) {
			return Veth{}, fmt.Errorf(
				"%w: route for %s points to missing ifindex %d",
				ErrPodVethNotFound,
				podIP,
				route.LinkIndex,
			)
		}

		return Veth{}, fmt.Errorf(
			"network: lookup ifindex %d for Pod %s: %w",
			route.LinkIndex,
			podIP,
			err,
		)
	}

	if link == nil || link.Attrs() == nil {
		return Veth{}, fmt.Errorf(
			"%w: route for %s points to invalid ifindex %d",
			ErrPodVethNotFound,
			podIP,
			route.LinkIndex,
		)
	}

	if link.Type() != "veth" {
		return Veth{}, fmt.Errorf(
			"%w: route for %s points to %q interface %q",
			ErrPodVethNotFound,
			podIP,
			link.Type(),
			link.Attrs().Name,
		)
	}

	if link.Attrs().Index <= 0 || link.Attrs().Name == "" {
		return Veth{}, fmt.Errorf(
			"%w: veth for %s has invalid kernel identity",
			ErrPodVethNotFound,
			podIP,
		)
	}

	return Veth{
		HostName:    link.Attrs().Name,
		HostIfIndex: link.Attrs().Index,
	}, nil
}

func isLocalPodHostRoute(
	route netlink.Route,
	podIP netip.Addr,
) bool {
	if route.Dst == nil ||
		route.LinkIndex <= 0 ||
		route.Scope != netlink.SCOPE_LINK ||
		len(route.Gw) != 0 {
		return false
	}

	prefix, ok := prefixFromIPNet(route.Dst)
	if !ok {
		return false
	}

	return prefix.Bits() == 32 &&
		prefix.Addr() == podIP
}

func prefixFromIPNet(network *net.IPNet) (netip.Prefix, bool) {
	if network == nil || network.IP == nil {
		return netip.Prefix{}, false
	}

	ones, bits := network.Mask.Size()
	if ones < 0 || bits != 32 {
		return netip.Prefix{}, false
	}

	addr, ok := netip.AddrFromSlice(network.IP)
	if !ok {
		return netip.Prefix{}, false
	}
	addr = addr.Unmap()
	if !addr.Is4() {
		return netip.Prefix{}, false
	}

	return netip.PrefixFrom(addr, ones).Masked(), true
}
