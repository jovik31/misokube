package devlink

import (
	"github.com/vishvananda/netlink"
	"net"
)

type NetlinkAPI struct{}

func (n NetlinkAPI) LinkByName(name string) (netlink.Link, error) { return netlink.LinkByName(name) }

func (n NetlinkAPI) linkByIndex(index int) (netlink.Link, error) { return netlink.LinkByIndex(index) }

func (n NetlinkAPI) LinkSetup(link netlink.Link) error { return netlink.LinkSetUp(link) }

func (n NetlinkAPI) LinkSetName(link netlink.Link, name string) error {
	return netlink.LinkSetName(link, name)
}

func (n NetlinkAPI) AddrAdd(link netlink.Link, ip *net.IPNet) error {
	return netlink.AddrAdd(link, addr)
}

func (n NetlinkAPI) AddrReplace(link netlink.Link, ipnet *net.IPNet) error {
	return netlink.AddrReplace(link, addr)
}

func (n NetlinkAPI) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}

func (n NetlinkAPI) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrDel(link, addr)
}

func (n NetlinkAPI) LinkAdd(link netlink.Link) error { return netlink.LinkAdd(link) }

func (n NetlinkAPI) LinkDel(link netlink.Link) error { return netlink.LinkDel(link) }

func NewNetlinkAPI() *NetlinkAPI { return &NetlinkAPI{} }
