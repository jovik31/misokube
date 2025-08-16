package netlinkroute

import (
	"errors"
	"github.com/vishvananda/netlink"
	"syscall"
)

type mockRouteHandle struct {
	linkIndex map[string]int

	adds []*netlink.Route
	dels []*netlink.Route

	// pre-seeded list results returned by RouteListFiltered
	lists []netlink.Route

	// injectable errors
	linkByNameErr        error
	routeAddErr          error
	routeDelErr          error
	routeListFilteredErr error
}

func newMockRouteHandle() *mockRouteHandle {
	return &mockRouteHandle{
		linkIndex: map[string]int{},
	}
}

func (m *mockRouteHandle) LinkByName(name string) (netlink.Link, error) {
	if m.linkByNameErr != nil {
		return nil, m.linkByNameErr
	}
	idx, ok := m.linkIndex[name]
	if !ok {
		return nil, syscall.ENOENT
	}
	return &netlink.Dummy{
		LinkAttrs: netlink.LinkAttrs{
			Index: idx,
			Name:  name,
		},
	}, nil
}

func (m *mockRouteHandle) RouteAdd(rt *netlink.Route) error {
	m.adds = append(m.adds, rt)
	if m.routeAddErr != nil {
		return m.routeAddErr
	}
	return nil
}

func (m *mockRouteHandle) RouteDel(rt *netlink.Route) error {
	m.dels = append(m.dels, rt)
	if m.routeDelErr != nil {
		return m.routeDelErr
	}
	return nil
}

func (m *mockRouteHandle) RouteListFiltered(_ int, _ *netlink.Route, _ uint64) ([]netlink.Route, error) {
	if m.routeListFilteredErr != nil {
		return nil, m.routeListFilteredErr
	}
	// return a copy to avoid mutation issues
	out := make([]netlink.Route, len(m.lists))
	copy(out, m.lists)
	return out, nil
}

// helpers
var errArbitrary = errors.New("boom")
