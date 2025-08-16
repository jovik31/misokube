package route

import (
	"github.com/containernetworking/plugins/pkg/ns"
	"net"
)

type Route struct {
	Dst     *net.IPNet
	Device  string
	Gateway net.IP
	Metric  int
	Onlink  bool
	Table   int
}

type RouteManager interface {
	Ensure(r *Route, iNS ...ns.NetNS) error
	Update(r *Route, iNS ...ns.NetNS) error
	Delete(r *Route, iNS ...ns.NetNS) error
}

var DefaultRouteManager RouteManager

func RegisterDefaultRouteManager(mgr RouteManager) {
	if mgr == nil {
		panic("RouteManager is nil")
	}
	DefaultRouteManager = mgr
}

func Manager() RouteManager {
	return DefaultRouteManager
}
