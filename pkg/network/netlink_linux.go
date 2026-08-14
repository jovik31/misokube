//go:build linux

package network

import "github.com/vishvananda/netlink"

// Shared netlink operations used by more than one Linux networking primitive.
//
// Keeping these package-level function variables in one place avoids duplicate
// declarations and lets unit tests replace the kernel call without coupling
// route reconciliation to Pod-veth recovery.
var routeListFiltered = netlink.RouteListFiltered
