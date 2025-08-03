package route

import "net"

type RouteManager interface {
	Add(r Route) error
	Update(r Route) error
	Delete(r Route) error
}

var DefaultRouteManager RouteManager

func SetDefaultRouteManager(m RouteManager) {
	DefaultRouteManager = m
}

type Route struct {
	Dst     *net.IPNet
	Device  string
	Gateway net.IP
	Metric  int
}
