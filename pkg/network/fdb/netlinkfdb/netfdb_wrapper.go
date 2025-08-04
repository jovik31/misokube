package netlinkfdb

import (
	"github.com/vishvananda/netlink"
)

type NetlinkFDBHandle interface {
	LinkByName(name string) (netlink.Link, error)
	NeighAdd(*netlink.Neigh) error
	NeighSet(*netlink.Neigh) error
	NeighDel(*netlink.Neigh) error
	NeighList(ifindex, family int) ([]netlink.Neigh, error)
}

type rNetlinkFDB struct {
}

func (r rNetlinkFDB) LinkByName(name string) (netlink.Link, error) { return netlink.LinkByName(name) }
func (r rNetlinkFDB) NeighAdd(n *netlink.Neigh) error              { return netlink.NeighAdd(n) }
func (r rNetlinkFDB) NeighSet(n *netlink.Neigh) error              { return netlink.NeighSet(n) }
func (r rNetlinkFDB) NeighDel(n *netlink.Neigh) error              { return netlink.NeighDel(n) }
func (r rNetlinkFDB) NeighList(ifindex, family int) ([]netlink.Neigh, error) {
	return netlink.NeighList(ifindex, family)
}
