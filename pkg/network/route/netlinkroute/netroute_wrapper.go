package netlinkroute

import (
	"github.com/vishvananda/netlink"
)

type NetlinkRouteHandle interface {
	LinkByName(name string) (netlink.Link, error)
	RouteAdd(*netlink.Route) error
	RouteDel(*netlink.Route) error
	RouteListFiltered(family int, filter *netlink.Route, filterMask uint64) ([]netlink.Route, error)
}

type rNetlinkRoute struct{}

func (r rNetlinkRoute) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}
func (r rNetlinkRoute) RouteAdd(route *netlink.Route) error { return netlink.RouteAdd(route) }
func (r rNetlinkRoute) RouteDel(route *netlink.Route) error { return netlink.RouteDel(route) }
func (r rNetlinkRoute) RouteListFiltered(family int, filter *netlink.Route, filterMask uint64) ([]netlink.Route, error) {
	return netlink.RouteListFiltered(family, filter, filterMask)
}
