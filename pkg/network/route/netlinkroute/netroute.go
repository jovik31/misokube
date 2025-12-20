package netlinkroute

import (
	"errors"
	"fmt"
	"syscall"

	//gitHub imports

	"github/setera/pkg/network/route"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
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

		rt := buildRoute(route, link.Attrs().Index)

		return n.nl.RouteAdd(rt)

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
		rt := buildRoute(route, link.Attrs().Index)
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
		rt := buildRoute(route, link.Attrs().Index)

		if err := n.nl.RouteDel(rt); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
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

func buildRoute(r *route.Route, linkIndex int) *netlink.Route {

	rt := &netlink.Route{
		LinkIndex: linkIndex,
		Dst:       r.Dst,
		Gw:        r.Gateway,
		Table:     unix.RT_TABLE_MAIN, // default table
		Priority:  0,                  // default metric
		Flags:     syscall.RTNH_F_ONLINK,
	}

	if r.Gateway != nil && !r.Gateway.IsUnspecified() {

		rt.Scope = netlink.SCOPE_UNIVERSE
	} else {
		rt.Scope = netlink.SCOPE_LINK
	}

	return rt
}
