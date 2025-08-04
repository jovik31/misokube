package netlinkfdb

import (
	"errors"
	"github.com/vishvananda/netlink"
	"github/setera/pkg/network/fdb"
	"syscall"
)

var _ fdb.FDBManager = (*netlinkFDBManager)(nil)

type netlinkFDBManager struct {
	nl NetlinkFDBHandle
}

func NewFDBManager(handle NetlinkFDBHandle) fdb.FDBManager {
	return &netlinkFDBManager{nl: handle}
}

func init() {

	fdb.RegisterFDBManager(NewFDBManager(rNetlinkFDB{}))
}

func (n netlinkFDBManager) Add(fe fdb.FDBEntry) error {

	link, err := n.nl.LinkByName(fe.Device)
	if err != nil {
		return err
	}
	return n.nl.NeighAdd(&netlink.Neigh{
		LinkIndex:    link.Attrs().Index,
		Family:       syscall.AF_BRIDGE,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		IP:           fe.IP,
		HardwareAddr: fe.Mac,
	})

}

func (n netlinkFDBManager) Update(fe fdb.FDBEntry) error {

	link, err := n.nl.LinkByName(fe.Device)
	if err != nil {
		return err
	}

	// get all neigh entries
	neighs, err := n.nl.NeighList(link.Attrs().Index, syscall.AF_BRIDGE)
	if err != nil {
		return err
	}

	var existing *netlink.Neigh
	// find the one matching the mac address
	for _, neigh := range neighs {
		if neigh.HardwareAddr.String() == fe.Mac.String() { // check if string comparison is reliable then pass to reflect or deep equal

			existing = &neigh
			break
		}
	}
	if existing == nil {
		return errors.New("fdb update: no existing entry for MAC" + fe.Mac.String())
	}

	existing.IP = fe.IP // add the updated IP
	if err := n.nl.NeighSet(existing); err != nil {
		return err
	}
	return nil
}

func (n netlinkFDBManager) Delete(fe fdb.FDBEntry) error {

	link, err := n.nl.LinkByName(fe.Device)
	if err != nil {
		return err
	}
	return n.nl.NeighDel(&netlink.Neigh{
		LinkIndex:    link.Attrs().Index,
		Family:       syscall.AF_BRIDGE,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		IP:           fe.IP,
		HardwareAddr: fe.Mac,
	})
}
