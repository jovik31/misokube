package noderouting

import "github/setera/pkg/network"

func routeFor(ifIndex int, node RemoteNode) network.Route {
	return network.Route{
		Prefix:  node.PodCIDR,
		IfIndex: ifIndex,
		Gateway: node.VTEPIP,
		OnLink:  true,
	}
}

func routeKey(route network.Route) string {
	return route.Prefix.Masked().String()
}