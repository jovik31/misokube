//go:build linux

package network

import (
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

type fakeNetNS struct{}

func (fakeNetNS) Do(fn func(ns.NetNS) error) error {
	return fn(nil)
}

func (fakeNetNS) Set() error   { return nil }
func (fakeNetNS) Path() string { return "/fake/netns" }
func (fakeNetNS) Fd() uintptr  { return 0 }
func (fakeNetNS) Close() error { return nil }

type networkHooks struct {
	getNS         func(string) (ns.NetNS, error)
	setupVethPair func(string, int, ns.NetNS) (string, string, error)
	linkByName    func(string) (netlink.Link, error)
	addrAdd       func(netlink.Link, *netlink.Addr) error
	addrReplace   func(netlink.Link, *netlink.Addr) error
	addrList      func(netlink.Link, int) ([]netlink.Addr, error)
	linkSetMTU    func(netlink.Link, int) error
	linkSetUp     func(netlink.Link) error
	linkDelete    func(netlink.Link) error
	routeReplace  func(*netlink.Route) error
	neighborSet   func(*netlink.Neigh) error
}

func saveNetworkHooks() networkHooks {
	return networkHooks{
		getNS:         getNS,
		setupVethPair: setupVethPair,
		linkByName:    linkByName,
		addrAdd:       addrAdd,
		addrReplace:   addrReplace,
		addrList:      addrList,
		linkSetMTU:    linkSetMTU,
		linkSetUp:     linkSetUp,
		linkDelete:    linkDelete,
		routeReplace:  routeReplace,
		neighborSet:   neighborSet,
	}
}

func restoreNetworkHooks(h networkHooks) {
	getNS = h.getNS
	setupVethPair = h.setupVethPair
	linkByName = h.linkByName
	addrAdd = h.addrAdd
	addrReplace = h.addrReplace
	addrList = h.addrList
	linkSetMTU = h.linkSetMTU
	linkSetUp = h.linkSetUp
	linkDelete = h.linkDelete
	routeReplace = h.routeReplace
	neighborSet = h.neighborSet
}

func TestSetupVethValidation(t *testing.T) {
	network := NewLinux()
	podIP := netip.MustParseAddr("10.244.0.10")
	gateway := netip.MustParseAddr("169.254.1.1")

	tests := []struct {
		name      string
		netnsPath string
		ifName    string
		podIP     netip.Addr
		gateway   netip.Addr
		mtu       int
	}{
		{name: "empty netns", ifName: "eth0", podIP: podIP, gateway: gateway, mtu: 1500},
		{name: "empty interface", netnsPath: "/netns", podIP: podIP, gateway: gateway, mtu: 1500},
		{name: "long interface", netnsPath: "/netns", ifName: "this-interface-is-too-long", podIP: podIP, gateway: gateway, mtu: 1500},
		{name: "invalid pod address", netnsPath: "/netns", ifName: "eth0", gateway: gateway, mtu: 1500},
		{name: "ipv6 pod address", netnsPath: "/netns", ifName: "eth0", podIP: netip.MustParseAddr("fd00::10"), gateway: gateway, mtu: 1500},
		{name: "invalid gateway", netnsPath: "/netns", ifName: "eth0", podIP: podIP, mtu: 1500},
		{name: "ipv6 gateway", netnsPath: "/netns", ifName: "eth0", podIP: podIP, gateway: netip.MustParseAddr("fd00::1"), mtu: 1500},
		{name: "invalid mtu", netnsPath: "/netns", ifName: "eth0", podIP: podIP, gateway: gateway},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := network.SetupVeth(
				tt.netnsPath,
				tt.ifName,
				tt.podIP,
				tt.gateway,
				tt.mtu,
			)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSetupVethReturnsHostIfIndex(t *testing.T) {
	hooks := saveNetworkHooks()
	defer restoreNetworkHooks(hooks)

	getNS = func(string) (ns.NetNS, error) {
		return fakeNetNS{}, nil
	}
	setupVethPair = func(string, int, ns.NetNS) (string, string, error) {
		return "veth-host", "eth0", nil
	}

	containerLink := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
		Name:  "eth0",
		Index: 11,
	}}
	hostLink := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
		Name:         "veth-host",
		Index:        42,
		HardwareAddr: net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
	}}

	linkByName = func(name string) (netlink.Link, error) {
		switch name {
		case "eth0":
			return containerLink, nil
		case "veth-host":
			return hostLink, nil
		default:
			return nil, syscall.ENOENT
		}
	}

	var podAddress string
	addrAdd = func(_ netlink.Link, addr *netlink.Addr) error {
		podAddress = addr.IPNet.String()
		return nil
	}

	var hostAddress string
	addrReplace = func(_ netlink.Link, addr *netlink.Addr) error {
		hostAddress = addr.IPNet.String()
		return nil
	}

	linkSetMTU = func(netlink.Link, int) error { return nil }
	linkSetUp = func(netlink.Link) error { return nil }

	var routes []netlink.Route
	routeReplace = func(route *netlink.Route) error {
		routes = append(routes, *route)
		return nil
	}

	neighborCalled := false
	neighborSet = func(neigh *netlink.Neigh) error {
		neighborCalled = true
		if neigh.LinkIndex != 11 {
			t.Fatalf("neighbor ifindex = %d, want 11", neigh.LinkIndex)
		}
		if got := neigh.IP.String(); got != "169.254.1.1" {
			t.Fatalf("neighbor IP = %s, want 169.254.1.1", got)
		}
		return nil
	}

	linkDelete = func(netlink.Link) error {
		t.Fatal("unexpected cleanup")
		return nil
	}

	got, err := NewLinux().SetupVeth(
		"/fake/netns",
		"eth0",
		netip.MustParseAddr("10.244.0.10"),
		netip.MustParseAddr("169.254.1.1"),
		1450,
	)
	if err != nil {
		t.Fatal(err)
	}

	if got.HostName != "veth-host" {
		t.Fatalf("host name = %q, want %q", got.HostName, "veth-host")
	}
	if got.HostIfIndex != 42 {
		t.Fatalf("host ifindex = %d, want 42", got.HostIfIndex)
	}
	if podAddress != "10.244.0.10/32" {
		t.Fatalf("pod address = %s, want 10.244.0.10/32", podAddress)
	}
	if hostAddress != "169.254.1.1/32" {
		t.Fatalf("host address = %s, want 169.254.1.1/32", hostAddress)
	}
	if len(routes) != 3 {
		t.Fatalf("route count = %d, want 3", len(routes))
	}
	if !neighborCalled {
		t.Fatal("static gateway neighbor was not configured")
	}
}

func TestSetupVethCleansUpPartialVeth(t *testing.T) {
	hooks := saveNetworkHooks()
	defer restoreNetworkHooks(hooks)

	getNS = func(string) (ns.NetNS, error) {
		return fakeNetNS{}, nil
	}
	setupVethPair = func(string, int, ns.NetNS) (string, string, error) {
		return "veth-host", "eth0", nil
	}

	containerLink := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 11}}
	hostLink := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "veth-host", Index: 42}}

	linkByName = func(name string) (netlink.Link, error) {
		switch name {
		case "eth0":
			return containerLink, nil
		case "veth-host":
			return hostLink, nil
		default:
			return nil, syscall.ENOENT
		}
	}

	setupErr := errors.New("address setup failed")
	addrAdd = func(netlink.Link, *netlink.Addr) error {
		return setupErr
	}

	deleted := false
	linkDelete = func(link netlink.Link) error {
		deleted = link.Attrs().Name == "veth-host"
		return nil
	}

	_, err := NewLinux().SetupVeth(
		"/fake/netns",
		"eth0",
		netip.MustParseAddr("10.244.0.10"),
		netip.MustParseAddr("169.254.1.1"),
		1450,
	)
	if !errors.Is(err, setupErr) {
		t.Fatalf("got %v, want wrapped setup error", err)
	}
	if !deleted {
		t.Fatal("partial host veth was not deleted")
	}
}

func TestDeleteVethMissingNamespaceIsIdempotent(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-netns")

	if err := NewLinux().DeleteVeth(missing, "eth0"); err != nil {
		t.Fatalf("delete missing namespace: %v", err)
	}
}

func TestDeleteVeth(t *testing.T) {
	hooks := saveNetworkHooks()
	defer restoreNetworkHooks(hooks)

	netnsPath := filepath.Join(t.TempDir(), "netns")
	if err := os.WriteFile(netnsPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	getNS = func(string) (ns.NetNS, error) {
		return fakeNetNS{}, nil
	}
	linkByName = func(name string) (netlink.Link, error) {
		return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}, nil
	}

	deleted := false
	linkDelete = func(netlink.Link) error {
		deleted = true
		return nil
	}

	if err := NewLinux().DeleteVeth(netnsPath, "eth0"); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("veth was not deleted")
	}
}

func TestCheckVeth(t *testing.T) {
	hooks := saveNetworkHooks()
	defer restoreNetworkHooks(hooks)

	getNS = func(string) (ns.NetNS, error) {
		return fakeNetNS{}, nil
	}
	linkByName = func(name string) (netlink.Link, error) {
		return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}, nil
	}
	addrList = func(netlink.Link, int) ([]netlink.Addr, error) {
		_, ipNet, err := net.ParseCIDR("10.244.0.10/32")
		if err != nil {
			t.Fatal(err)
		}
		ipNet.IP = net.ParseIP("10.244.0.10")
		return []netlink.Addr{{IPNet: ipNet}}, nil
	}

	if err := NewLinux().CheckVeth(
		"/fake/netns",
		"eth0",
		netip.MustParseAddr("10.244.0.10"),
	); err != nil {
		t.Fatal(err)
	}
}
