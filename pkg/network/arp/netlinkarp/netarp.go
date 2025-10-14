package netlinkarp

import (
	"errors"
	"fmt"
	"github/setera/pkg/network/arp"
	"net"
	"syscall"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
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
		Family:       pickFamily(ae.IP),
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           ae.IP,
		HardwareAddr: ae.MAC,
	}
	return n.nl.NeighSet(ne)
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
		Family:       pickFamily(ae.IP),
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           ae.IP,
		HardwareAddr: ae.MAC,
	}
	return n.nl.NeighSet(ne) // replace semantics
}

func (n netlinkARPManager) Delete(ae arp.ARPEntry) error {

	if ae.Device == "" || ae.IP == nil {
		return fmt.Errorf("arp delete: invalid entry")
	}
	l, err := n.nl.LinkByName(ae.Device)
	if err != nil {
		return fmt.Errorf("arp delete: link %q: %w", ae.Device, err)
	}
	ne := &netlink.Neigh{
		LinkIndex: l.Attrs().Index,
		Family:    pickFamily(ae.IP),
		IP:        ae.IP,
	}

	if len(ae.MAC) > 0 {
		ne.State = netlink.NUD_PERMANENT
		ne.Type = syscall.RTN_UNICAST
		ne.HardwareAddr = ae.MAC

	}
	if err := n.nl.NeighDel(ne); err != nil && !errors.Is(err, syscall.ENOENT) {
		return fmt.Errorf("arp delete: %w", err)
	}
	return nil

}

func pickFamily(ip net.IP) int {

	if len(ip) == 0 {

		return unix.AF_UNSPEC

	}
	if ip.To4() != nil {
		return netlink.FAMILY_V4
	}

	if ip.To16() != nil {
		return netlink.FAMILY_V6
	}

	return unix.AF_UNSPEC

}
