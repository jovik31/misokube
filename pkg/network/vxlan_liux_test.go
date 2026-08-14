//go:build linux

package network

import (
	"net"
	"net/netip"
	"syscall"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestEnsureVXLANReusesMatchingLink(t *testing.T) {
	oldLinkByName := linkByName
	oldLinkListAll := linkListAll
	oldAddrList := addrList
	oldLinkSetMTU := linkSetMTU
	oldAddrReplace := addrReplace
	oldLinkSetUp := linkSetUp
	defer func() {
		linkByName = oldLinkByName
		linkListAll = oldLinkListAll
		addrList = oldAddrList
		linkSetMTU = oldLinkSetMTU
		addrReplace = oldAddrReplace
		linkSetUp = oldLinkSetUp
	}()

	mac, err := net.ParseMAC("02:00:00:00:01:01")
	if err != nil {
		t.Fatal(err)
	}
	underlay := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 2}}
	link := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:         "setera-vxlan0",
			Index:        17,
			HardwareAddr: mac,
		},
		VxlanId:      100,
		VtepDevIndex: 2,
		SrcAddr:      net.ParseIP("172.18.0.3").To4(),
		Port:         4789,
	}

	linkListAll = func() ([]netlink.Link, error) { return []netlink.Link{underlay}, nil }
	addrList = func(got netlink.Link, family int) ([]netlink.Addr, error) {
		if got.Attrs().Index != 2 || family != netlink.FAMILY_V4 {
			t.Fatalf("unexpected underlay address lookup link=%d family=%d", got.Attrs().Index, family)
		}
		return []netlink.Addr{{IPNet: &net.IPNet{IP: net.ParseIP("172.18.0.3").To4(), Mask: net.CIDRMask(24, 32)}}}, nil
	}
	linkByName = func(string) (netlink.Link, error) { return link, nil }
	linkSetMTU = func(netlink.Link, int) error { return nil }
	linkSetUp = func(netlink.Link) error { return nil }

	var gotAddress string
	addrReplace = func(_ netlink.Link, addr *netlink.Addr) error {
		gotAddress = addr.IPNet.String()
		return nil
	}

	got, err := NewLinux().EnsureVXLAN(VXLANConfig{
		Name:       "setera-vxlan0",
		VNI:        100,
		Port:       4789,
		MTU:        1450,
		Address:    netip.MustParsePrefix("10.244.1.1/32"),
		UnderlayIP: netip.MustParseAddr("172.18.0.3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IfIndex != 17 || got.MAC.String() != mac.String() {
		t.Fatalf("got %+v", got)
	}
	if gotAddress != "10.244.1.1/32" {
		t.Fatalf("address = %q, want 10.244.1.1/32", gotAddress)
	}
}

func TestEnsureVXLANCreatesWithExplicitUnderlay(t *testing.T) {
	oldLinkByName := linkByName
	oldLinkListAll := linkListAll
	oldAddrList := addrList
	oldLinkAdd := linkAdd
	oldLinkSetMTU := linkSetMTU
	oldAddrReplace := addrReplace
	oldLinkSetUp := linkSetUp
	defer func() {
		linkByName = oldLinkByName
		linkListAll = oldLinkListAll
		addrList = oldAddrList
		linkAdd = oldLinkAdd
		linkSetMTU = oldLinkSetMTU
		addrReplace = oldAddrReplace
		linkSetUp = oldLinkSetUp
	}()

	underlay := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 2}}
	linkListAll = func() ([]netlink.Link, error) { return []netlink.Link{underlay}, nil }
	addrList = func(got netlink.Link, family int) ([]netlink.Addr, error) {
		if got.Attrs().Index != 2 || family != netlink.FAMILY_V4 {
			t.Fatalf("unexpected underlay address lookup link=%d family=%d", got.Attrs().Index, family)
		}
		return []netlink.Addr{{IPNet: &net.IPNet{IP: net.ParseIP("172.18.0.3").To4(), Mask: net.CIDRMask(24, 32)}}}, nil
	}

	var created *netlink.Vxlan
	lookupCount := 0
	linkByName = func(name string) (netlink.Link, error) {
		lookupCount++
		if lookupCount == 1 {
			return nil, syscall.ENOENT
		}
		if created == nil {
			t.Fatal("VXLAN was not created before second lookup")
		}
		return created, nil
	}
	linkAdd = func(link netlink.Link) error {
		vxlan, ok := link.(*netlink.Vxlan)
		if !ok {
			t.Fatalf("created link type = %T, want *netlink.Vxlan", link)
		}
		created = vxlan
		created.LinkAttrs.Index = 17
		created.LinkAttrs.HardwareAddr = mustParseMACForNetworkTest(t, "02:00:00:00:01:01")
		return nil
	}
	linkSetMTU = func(netlink.Link, int) error { return nil }
	addrReplace = func(netlink.Link, *netlink.Addr) error { return nil }
	linkSetUp = func(netlink.Link) error { return nil }

	_, err := NewLinux().EnsureVXLAN(VXLANConfig{
		Name:       "setera-vxlan0",
		VNI:        100,
		Port:       4789,
		MTU:        1450,
		Address:    netip.MustParsePrefix("10.244.1.1/32"),
		UnderlayIP: netip.MustParseAddr("172.18.0.3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created == nil {
		t.Fatal("VXLAN was not created")
	}
	if lookupCount != 3 {
		t.Fatalf("link lookup count = %d, want 3 including post-up refresh", lookupCount)
	}
	if created.VtepDevIndex != 2 {
		t.Fatalf("VtepDevIndex = %d, want 2", created.VtepDevIndex)
	}
	if created.SrcAddr == nil || !created.SrcAddr.Equal(net.ParseIP("172.18.0.3")) {
		t.Fatalf("SrcAddr = %v, want 172.18.0.3", created.SrcAddr)
	}
}

func mustParseMACForNetworkTest(t *testing.T, value string) net.HardwareAddr {
	t.Helper()
	mac, err := net.ParseMAC(value)
	if err != nil {
		t.Fatal(err)
	}
	return mac
}

func TestEnsureVXLANRequiresHostPrefix(t *testing.T) {
	_, err := NewLinux().EnsureVXLAN(VXLANConfig{
		Name:       "setera-vxlan0",
		VNI:        100,
		Port:       4789,
		MTU:        1450,
		Address:    netip.MustParsePrefix("10.244.1.1/24"),
		UnderlayIP: netip.MustParseAddr("172.18.0.3"),
	})
	if err == nil {
		t.Fatal("expected /32 validation error")
	}
}

func TestEnsureVXLANRequiresUnderlayIP(t *testing.T) {
	_, err := NewLinux().EnsureVXLAN(VXLANConfig{
		Name:    "setera-vxlan0",
		VNI:     100,
		Port:    4789,
		MTU:     1450,
		Address: netip.MustParsePrefix("10.244.1.1/32"),
	})
	if err == nil {
		t.Fatal("expected underlay IP validation error")
	}
}
