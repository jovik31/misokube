package routing

import (
	"log"
	"net"
	"syscall"

	"github.com/vishvananda/netlink"
)

func AddARP(localVtepID int, remoteVtepIP net.IP, remoteVtepMac net.HardwareAddr) error {

	return netlink.NeighSet(&netlink.Neigh{
		LinkIndex:    localVtepID,
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           remoteVtepIP,
		HardwareAddr: remoteVtepMac,
	})
}

func DelARP(localVtepID int, remoteVtepIP net.IP, remoteVtepMac net.HardwareAddr) error {

	return netlink.NeighDel(&netlink.Neigh{
		LinkIndex:    localVtepID,
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           remoteVtepIP,
		HardwareAddr: remoteVtepMac,
	})
}

func AddFDB(localVtepID int, remoteHostIP net.IP, remoteVtepMac net.HardwareAddr) error {
	return netlink.NeighSet(&netlink.Neigh{
		LinkIndex:    localVtepID,
		Family:       syscall.AF_BRIDGE,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		IP:           remoteHostIP,
		HardwareAddr: remoteVtepMac,
	})
}

func DelFDB(localVtepID int, remoteHostIP net.IP, remoteVtepMac net.HardwareAddr) error {
	return netlink.NeighDel(&netlink.Neigh{
		LinkIndex:    localVtepID,
		Family:       syscall.AF_BRIDGE,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		IP:           remoteHostIP,
		HardwareAddr: remoteVtepMac,
	})
}

func AddRoutes(localVtepID int, remoteTenantCIDR *net.IPNet, remoteVtepIP net.IP) error {

	netlink.RouteAdd(&netlink.Route{
		LinkIndex: localVtepID,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst:       &net.IPNet{IP: remoteVtepIP, Mask: net.CIDRMask(32, 32)},
		Flags:     syscall.RTNH_F_ONLINK,
	})

	netlink.RouteAdd(&netlink.Route{
		LinkIndex: localVtepID,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst:       remoteTenantCIDR,
		Gw:        remoteVtepIP,
		Flags:     syscall.RTNH_F_ONLINK, //Check if this is the correct flag or necessary
	})
	log.Printf("Adding route to %s via %s", remoteTenantCIDR.String(), remoteVtepIP.String())

	netlink.RouteDel(&netlink.Route{
		LinkIndex: localVtepID,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst:       &net.IPNet{IP: remoteVtepIP, Mask: net.CIDRMask(32, 32)}, // it is always a /32 ip address
		Flags:     syscall.RTNH_F_ONLINK,
	})
	return nil
}

func CheckARP(localVtepID int) ([]netlink.Neigh, error) {

	return netlink.NeighList(localVtepID, netlink.FAMILY_V4)

}
