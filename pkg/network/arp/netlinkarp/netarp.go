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

	link, err := n.nl.LinkByName(ae.Device)
	if err != nil {
		return err
	}

	if ae.IP == nil || ae.IP.To4() == nil {
		return fmt.Errorf("IP is nil")
	}

	if len(ae.MAC) == 0 {
		return fmt.Errorf("MAC is nil")
	}

	return n.nl.NeighSet(&netlink.Neigh{
		LinkIndex:    link.Attrs().Index,
		State:        netlink.NUD_PERMANENT,
		Family:       syscall.AF_INET,
		Type:         syscall.RTN_UNICAST,
		IP:           ae.IP,
		HardwareAddr: ae.MAC,
	})

}
func (n netlinkARPManager) Update(ae arp.ARPEntry) error {

	link, err := n.nl.LinkByName(ae.Device)
	if err != nil {
		return err
	}

	neighs, err := n.nl.NeighList(link.Attrs().Index, syscall.AF_INET)
	if err != nil {
		return err
	}

	var ei *netlink.Neigh

	for i := range neighs {
		if neighs[i].IP.Equal(ei.IP) {
			ei = &neighs[i]
			break
		}
	}

	if ei == nil {
		return fmt.Errorf("Neighbor %v not found", ei)
	}

	ei.HardwareAddr = ae.MAC
	ei.State = netlink.NUD_PERMANENT
	ei.Family = syscall.AF_INET
	ei.Type = syscall.RTN_UNICAST

	return n.nl.NeighSet(ei)
}
func (n netlinkARPManager) Delete(ae arp.ARPEntry) error {

	link, err := n.nl.LinkByName(ae.Device)
	if err != nil {
		return err
	}
	return n.nl.NeighDel(&netlink.Neigh{
		LinkIndex:    link.Attrs().Index,
		Family:       syscall.AF_INET,
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           ae.IP,
		HardwareAddr: ae.MAC,
	})

}
