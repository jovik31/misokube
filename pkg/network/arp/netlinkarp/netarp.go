package netlinkarp

import (
	"fmt"
	"github.com/vishvananda/netlink"
	"github/setera/pkg/network/arp"
	"syscall"
)

var _ arp.ARPManager = (*netlinkARPManager)(nil)

type netlinkARPManager struct {
	nl NetlinkARPHandle
}

func NewARPManager(handle NetlinkARPHandle) arp.ARPManager {
	return &netlinkARPManager{nl: handle}
}

func init() {

	arp.RegisterARPManager(NewARPManager(rNetlinkARP{}))
}

func (n netlinkARPManager) Add(ae arp.ARPEntry) error {

	if ae.Device == "" || ae.IP == nil || len(ae.MAC) == 0 {
		return fmt.Errorf("arp add: invalid entry")
	}
	l, err := n.nl.LinkByName(ae.Device)
	if err != nil {
		return fmt.Errorf("arp add: link %q: %w", ae.Device, err)
	}
	ne := &netlink.Neigh{
		LinkIndex:    l.Attrs().Index,
		Family:       pickFamily(ae),
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           ae.IP,
		HardwareAddr: ae.MAC,
	}
	return n.nl.NeighAdd(ne)
}
func (n netlinkARPManager) Update(ae arp.ARPEntry) error {

	if ae.Device == "" || ae.IP == nil || len(ae.MAC) == 0 {
		return fmt.Errorf("arp update: invalid entry")
	}
	l, err := n.nl.LinkByName(ae.Device)
	if err != nil {
		return fmt.Errorf("arp update: link %q: %w", ae.Device, err)
	}
	ne := &netlink.Neigh{
		LinkIndex:    l.Attrs().Index,
		Family:       pickFamily(ae),
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           ae.IP,
		HardwareAddr: ae.MAC,
	}
	return n.nl.NeighSet(ne) // replace semantics
}

func (n netlinkARPManager) Delete(ae arp.ARPEntry) error {

	if ae.Device == "" || ae.IP == nil || len(ae.MAC) == 0 {
		return fmt.Errorf("arp delete: invalid entry")
	}
	l, err := n.nl.LinkByName(ae.Device)
	if err != nil {
		return fmt.Errorf("arp delete: link %q: %w", ae.Device, err)
	}
	ne := &netlink.Neigh{
		LinkIndex:    l.Attrs().Index,
		Family:       pickFamily(ae),
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           ae.IP,
		HardwareAddr: ae.MAC,
	}
	return n.nl.NeighDel(ne)

}

func pickFamily(ae arp.ARPEntry) int {
	if ae.Family != 0 {
		return ae.Family
	}
	if ae.IP.To4() != nil {
		return netlink.FAMILY_V4
	}
	return netlink.FAMILY_V6
}
