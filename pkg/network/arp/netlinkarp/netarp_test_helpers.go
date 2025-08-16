package netlinkarp

import (
	"github.com/vishvananda/netlink"
	"syscall"
)

type mockARPLink struct {
	idx  int
	name string
}

func (m *mockARPLink) Attrs() *netlink.LinkAttrs {
	return &netlink.LinkAttrs{Index: m.idx, Name: m.name}
}
func (m *mockARPLink) Type() string { return "device" }

type mockARPNL struct {
	linkIndex map[string]int
	adds      []*netlink.Neigh
	sets      []*netlink.Neigh
	dels      []*netlink.Neigh
	lists     []netlink.Neigh
}

func NewMockARPNL() *mockARPNL {
	return &mockARPNL{linkIndex: map[string]int{}}
}

func (m *mockARPNL) LinkByName(name string) (netlink.Link, error) {
	if idx, ok := m.linkIndex[name]; ok {
		return &mockARPLink{idx: idx, name: name}, nil
	}
	return nil, syscall.ENOENT
}

func (m *mockARPNL) NeighAdd(n *netlink.Neigh) error { m.adds = append(m.adds, n); return nil }
func (m *mockARPNL) NeighSet(n *netlink.Neigh) error { m.sets = append(m.sets, n); return nil }
func (m *mockARPNL) NeighDel(n *netlink.Neigh) error { m.dels = append(m.dels, n); return nil }
func (m *mockARPNL) NeighList(_ int, _ int) ([]netlink.Neigh, error) {
	return m.lists, nil
}
