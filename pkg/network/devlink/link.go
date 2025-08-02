package devlink

import (
	"github.com/vishvananda/netlink"
	"net"
)

type LinkAPI interface {
	LinkByName(name string) (netlink.Link, error)
	linkByIndex(index int) (netlink.Link, error)

	LinkSetup(link netlink.Link) error
	LinkSetName(link netlink.Link, name string) error

	AddrAdd(link netlink.Link, ip *net.IPNet) error
	AddrReplace(link netlink.Link, ip *net.IPNet) error
	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
	AddrDel(link netlink.Link, ip *net.IPNet) error

	LinkAdd(netlink.Link) error
	LinkDel(link netlink.Link) error
}
