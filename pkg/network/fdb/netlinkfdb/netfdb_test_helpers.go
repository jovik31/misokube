package netlinkfdb

import (
	"github.com/vishvananda/netlink"
	"syscall"
)

type MockNetlinkFDB struct {
	adds      []*netlink.Neigh
	sets      []*netlink.Neigh
	dels      []*netlink.Neigh
	lists     []netlink.Neigh
	linkIndex map[string]int
}

func (m *MockNetlinkFDB) LinkByName(name string) (netlink.Link, error) {

	idx, ok := m.linkIndex[name]
	if !ok {
		return nil, syscall.ENOENT
	}
	return &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Index: idx,
			Name:  name,
		},
		VxlanId: 1,
	}, nil
}

func (m *MockNetlinkFDB) NeighAdd(n *netlink.Neigh) error {
	m.adds = append(m.adds, n)
	return nil

}

func (m *MockNetlinkFDB) NeighSet(n *netlink.Neigh) error {
	m.sets = append(m.sets, n)
	return nil
}

func (m *MockNetlinkFDB) NeighDel(n *netlink.Neigh) error {
	m.dels = append(m.dels, n)
	return nil
}

func (m *MockNetlinkFDB) NeighList(ifindex int, family int) ([]netlink.Neigh, error) {
	return m.lists, nil
}

func NewMockNetlinkFDB() *MockNetlinkFDB {
	return &MockNetlinkFDB{linkIndex: map[string]int{}}
}
