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
// OnLink is used for a gateway that is intentionally reachable through the
// interface even though the interface does not carry the gateway's subnet.
type Route struct {
	Prefix  netip.Prefix
	IfIndex int
	Gateway netip.Addr
	OnLink  bool
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

	if err := routeDelete(buildNetlinkRoute(route)); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, syscall.ENOENT) {
		return fmt.Errorf("delete route %s: %w", route.Prefix, err)
	}
	return nil
}

// ListRoutes returns IPv4 main-table routes whose output interface is IfIndex.
func (n *Linux) ListRoutes(ifIndex int) ([]Route, error) {
	if ifIndex <= 0 {
		return nil, fmt.Errorf("network: invalid route ifindex %d", ifIndex)
	}

	filter := &netlink.Route{
		LinkIndex: ifIndex,
		Table:     unix.RT_TABLE_MAIN,
	}
	routes, err := routeListFiltered(
		netlink.FAMILY_V4,
		filter,
		netlink.RT_FILTER_OIF|netlink.RT_FILTER_TABLE,
	)
	if err != nil {
		return nil, fmt.Errorf("list routes for ifindex %d: %w", ifIndex, err)
	}

	out := make([]Route, 0, len(routes))
	for _, route := range routes {
		if route.Dst == nil {
			continue
		}

		prefix, err := netip.ParsePrefix(route.Dst.String())
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		if !prefix.Addr().Is4() {
			continue
		}

		result := Route{
			Prefix:  prefix,
			IfIndex: route.LinkIndex,
			OnLink:  route.Flags&unix.RTNH_F_ONLINK != 0,
		}
		if route.Gw != nil {
			if gateway, ok := netip.AddrFromSlice(route.Gw); ok {
				result.Gateway = gateway.Unmap()
			}
		}
		out = append(out, result)
	}

	return out, nil
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

	if route.OnLink && !route.Gateway.IsValid() {
		return errors.New("network: on-link route requires a gateway")
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
	if route.OnLink {
		result.Flags |= unix.RTNH_F_ONLINK
	}

	return result
}
