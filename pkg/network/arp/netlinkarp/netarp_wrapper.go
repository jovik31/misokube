package netlinkarp

import (
	"github.com/vishvananda/netlink"
)

type NetlinkARPHandle interface {
	LinkByName(name string) (netlink.Link, error)
	NeighAdd(*netlink.Neigh) error
	NeighSet(*netlink.Neigh) error
	NeighDel(*netlink.Neigh) error
	NeighList(ifindex, family int) ([]netlink.Neigh, error)
}

type rNetlinkARP struct{}

func (r rNetlinkARP) LinkByName(name string) (netlink.Link, error) { return netlink.LinkByName(name) }
func (r rNetlinkARP) NeighAdd(n *netlink.Neigh) error              { return netlink.NeighAdd(n) }
func (r rNetlinkARP) NeighSet(n *netlink.Neigh) error              { return netlink.NeighSet(n) }
func (r rNetlinkARP) NeighDel(n *netlink.Neigh) error              { return netlink.NeighDel(n) }
func (r rNetlinkARP) NeighList(ifindex, family int) ([]netlink.Neigh, error) {
	return netlink.NeighList(ifindex, family)
}
