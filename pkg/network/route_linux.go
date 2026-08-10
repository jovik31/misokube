//go:build linux

package network

import (
	"errors"
	"fmt"
	"net/netip"
	"syscall"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var routeDelete = netlink.RouteDel

// Route describes one route in the main routing table.
//
// Gateway can be invalid when the route is directly reachable through IfIndex.
type Route struct {
	Prefix  netip.Prefix
	IfIndex int
	Gateway netip.Addr
}

// ReplaceRoute creates or replaces a route.
func (n *Linux) ReplaceRoute(route Route) error {
	if err := validateRoute(route); err != nil {
		return err
	}

	netlinkRoute := buildNetlinkRoute(route)
	if err := routeReplace(netlinkRoute); err != nil {
		return fmt.Errorf("replace route %s: %w", route.Prefix, err)
	}
	return nil
}

// DeleteRoute deletes a route. A missing route is treated as already deleted.
func (n *Linux) DeleteRoute(route Route) error {
	if err := validateRoute(route); err != nil {
		return err
	}

	if err := routeDelete(buildNetlinkRoute(route)); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("delete route %s: %w", route.Prefix, err)
	}
	return nil
}

func validateRoute(route Route) error {
	if !route.Prefix.IsValid() {
		return errors.New("network: invalid route prefix")
	}
	if route.Prefix.Addr().Zone() != "" {
		return errors.New("network: zoned route prefix is not supported")
	}
	if route.IfIndex <= 0 {
		return fmt.Errorf("network: invalid route ifindex %d", route.IfIndex)
	}

	if route.Gateway.IsValid() {
		gateway := route.Gateway.Unmap()
		if gateway.Zone() != "" {
			return errors.New("network: zoned route gateway is not supported")
		}
		if gateway.Is4() != route.Prefix.Addr().Unmap().Is4() {
			return errors.New("network: route prefix and gateway use different address families")
		}
	}

	return nil
}

func buildNetlinkRoute(route Route) *netlink.Route {
	result := &netlink.Route{
		LinkIndex: route.IfIndex,
		Dst:       prefixToIPNet(route.Prefix),
		Table:     unix.RT_TABLE_MAIN,
		Scope:     netlink.SCOPE_LINK,
	}

	if route.Gateway.IsValid() {
		result.Gw = addrToNetIP(route.Gateway)
		result.Scope = netlink.SCOPE_UNIVERSE
	}

	return result
}
