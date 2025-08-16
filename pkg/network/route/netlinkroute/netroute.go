package netlinkroute

import (
	"errors"
	"fmt"
	"syscall"

	//gitHub imports

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"github/setera/pkg/network/route"
	"golang.org/x/sys/unix"
)

var _ route.RouteManager = (*netlinkRouteManager)(nil)

type netlinkRouteManager struct {
	nl NetlinkRouteHandle
}

func NewNetlinkRouteManager(handle NetlinkRouteHandle) route.RouteManager {
	return &netlinkRouteManager{nl: handle}
}

func init() {
	route.RegisterDefaultRouteManager(NewNetlinkRouteManager(rNetlinkRoute{}))
}

func (n *netlinkRouteManager) Ensure(route *route.Route, ns ...ns.NetNS) error {

	return do(ns, func() error {

		if route == nil {
			return fmt.Errorf("invalid route")
		}
		if route.Dst == nil {
			return fmt.Errorf("invalid destination")
		}
		if route.Device == "" {
			return fmt.Errorf("invalid device")
		}

		link, err := n.nl.LinkByName(route.Device)
		if err != nil {
			return err
		}

		return n.nl.RouteAdd(&netlink.Route{
			LinkIndex: link.Attrs().Index,
			Scope:     netlink.SCOPE_UNIVERSE,
			Dst:       route.Dst,
			Flags:     syscall.RTNH_F_ONLINK,
			Gw:        route.Gateway,
			Table:     unix.RT_TABLE_MAIN,
			Priority:  route.Metric,
		})

	})

}
func (n *netlinkRouteManager) Update(route *route.Route, ns ...ns.NetNS) error {

	return do(ns, func() error {
		if route == nil {
			return fmt.Errorf("invalid route")
		}
		if route.Dst == nil {
			return fmt.Errorf("invalid destination")
		}
		if route.Device == "" {
			return fmt.Errorf("invalid device")
		}
		link, err := n.nl.LinkByName(route.Device)
		if err != nil {
			return err
		}
		mask := netlink.RT_FILTER_OIF | netlink.RT_FILTER_TABLE | netlink.RT_FILTER_DST
		filter := netlink.Route{
			LinkIndex: link.Attrs().Index,
			Table:     unix.RT_TABLE_MAIN,
			Dst:       route.Dst,
		}

		old, err := n.nl.RouteListFiltered(netlink.FAMILY_ALL, &filter, mask)
		if err != nil {
			return err
		}

		for i := range old {
			if err := n.nl.RouteDel(&old[i]); err != nil {
				return err
			}
		}

		// add new route
		rt := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       route.Dst,
			Gw:        route.Gateway,
			Priority:  route.Metric,
			Table:     unix.RT_TABLE_MAIN,
			Scope:     netlink.SCOPE_LINK,
		}

		if route.Gateway != nil && !route.Gateway.IsUnspecified() {
			rt.Scope = netlink.SCOPE_UNIVERSE
		}
		if route.Onlink {
			rt.Flags |= syscall.RTNH_F_ONLINK
		}

		if err := n.nl.RouteAdd(rt); err != nil && !errors.Is(err, syscall.EEXIST) {
			return err
		}

		return nil

	})

}
func (n *netlinkRouteManager) Delete(route *route.Route, ns ...ns.NetNS) error {

	return do(ns, func() error {
		if route.Dst == nil {
			return fmt.Errorf("route delete: Dst cannot be nil")
		}
		if route.Device == "" {
			return fmt.Errorf("route delete: Device cannot be empty")
		}
		link, err := n.nl.LinkByName(route.Device)
		if err != nil {
			return fmt.Errorf("route delete: link %q: %w", route.Device, err)
		}
		rt := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       route.Dst,
			Gw:        route.Gateway,
			Priority:  route.Metric,
			Table:     unix.RT_TABLE_MAIN,
		}
		return n.nl.RouteDel(rt)
	})
}

func do(inNS []ns.NetNS, op func() error) error {

	switch len(inNS) {
	case 0:
		return op()

	case 1:
		n := inNS[0]
		if n == nil {
			return fmt.Errorf("invalid netns path")
		}
		return inNS[0].Do(func(_ ns.NetNS) error { return nil })
	default:
		return fmt.Errorf("atmost one namespace is allowed")

	}
}
