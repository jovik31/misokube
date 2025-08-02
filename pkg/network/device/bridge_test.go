//go:build unit
// +build unit

package device

import (
	"errors"
	"net"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"

	//internal
	"github/setera/pkg/network/device"
)

// --- Helpers to override netlink functions in tests ---
var (
	origLinkByName  = device.LinkByNameFunc
	origLinkAdd     = device.LinkAddFunc
	origAddrList    = device.AddrListFunc
	origAddrReplace = device.AddrReplaceFunc
	origLinkSetUp   = device.LinkSetUpFunc
	origLinkSetName = device.LinkSetNameFunc
)

func restoreNetlinkMocks() {
	device.LinkByNameFunc = origLinkByName
	device.LinkAddFunc = origLinkAdd
	device.AddrListFunc = origAddrList
	device.AddrReplaceFunc = origAddrReplace
	device.LinkSetUpFunc = origLinkSetUp
	device.LinkSetNameFunc = origLinkSetName
}

// mustParseCIDR helper
func mustParseCIDR(t *testing.T, s string) *net.IPNet {
	_, cidr, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("invalid CIDR %q: %v", s, err)
	}
	return cidr
}

// --- 1. GenerateBridgeName ---
func TestGenerateBridgeName(t *testing.T) {
	name, err := device.GenerateBridgeName("tenant123")
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(name, "br"))
	assert.Contains(t, name, "tenant123")
}

// --- 2. isLinkNotFound ---
func TestIsLinkNotFound(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{nil, false},
		{syscall.ENOENT, true},
		{errors.New("no such device"), true},
		{errors.New("Link not found"), true},
		{errors.New("other error"), false},
	}

	for _, tc := range tests {
		got := device.IsLinkNotFound(tc.err)
		assert.Equal(t, tc.expected, got, "err=%v", tc.err)
	}
}

// --- 3. maybeReplaceAddr ---
func TestMaybeReplaceAddr(t *testing.T) {
	defer restoreNetlinkMocks()

	// case A: AddrList returns an existing matching address
	device.AddrListFunc = func(link netlink.Link, family int) ([]netlink.Addr, error) {
		return []netlink.Addr{{IPNet: &net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.CIDRMask(24, 32)}}}, nil
	}
	called := false
	device.AddrReplaceFunc = func(link netlink.Link, addr *netlink.Addr) error {
		called = true
		return nil
	}
	link := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: "br-test"}}
	err := device.MaybeReplaceAddr(link, &net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.CIDRMask(24, 32)})
	assert.NoError(t, err)
	assert.False(t, called, "should not replace when matching addr exists")

	// case B: no matching address → should call Replace
	device.AddrListFunc = func(_ netlink.Link, _ int) ([]netlink.Addr, error) {
		return []netlink.Addr{}, nil
	}
	called = false
	err = device.MaybeReplaceAddr(link, &net.IPNet{IP: net.ParseIP("10.0.0.2"), Mask: net.CIDRMask(24, 32)})
	assert.NoError(t, err)
	assert.True(t, called, "should replace when no matching addr")

	// case C: AddrList error
	device.AddrListFunc = func(_ netlink.Link, _ int) ([]netlink.Addr, error) {
		return nil, errors.New("fail list")
	}
	called = false
	err = device.MaybeReplaceAddr(link, &net.IPNet{IP: net.ParseIP("10.0.0.3"), Mask: net.CIDRMask(24, 32)})
	assert.Error(t, err)
}

// --- 4. ensureBridgeState ---
func TestEnsureBridgeState(t *testing.T) {
	defer restoreNetlinkMocks()

	subnet := mustParseCIDR(t, "10.1.1.0/24")
	desired := &net.IPNet{IP: net.ParseIP("10.1.1.1"), Mask: subnet.Mask}

	// Mock AddrList → no existing
	device.AddrListFunc = func(_ netlink.Link, _ int) ([]netlink.Addr, error) {
		return []netlink.Addr{}, nil
	}
	// Capture the replace and set up calls
	replaced := false
	device.AddrReplaceFunc = func(link netlink.Link, addr *netlink.Addr) error {
		if !reflect.DeepEqual(addr.IPNet, desired) {
			t.Errorf("unexpected replace addr: %v", addr.IPNet)
		}
		replaced = true
		return nil
	}
	up := false
	device.LinkSetUpFunc = func(link netlink.Link) error {
		up = true
		return nil
	}
	renamed := false
	device.LinkSetNameFunc = func(link netlink.Link, new string) error {
		renamed = true
		return nil
	}

	link := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: "old-name"}}
	// Test: ensure the name is renamed to desired
	resultLink, ip, err := device.EnsureBridgeState(link, "new-name", subnet)
	assert.NoError(t, err)
	assert.Equal(t, link, resultLink)
	assert.True(t, replaced)
	assert.True(t, up)
	assert.True(t, renamed)
	assert.Equal(t, desired.IP.String(), ip.IP.String())
}

// --- 5. createBridge ---
func TestCreateBridge(t *testing.T) {
	defer restoreNetlinkMocks()

	// Case A: nil subnet → error
	_, _, err := device.CreateBridge("brx", nil)
	assert.Error(t, err)

	// Case B: successful create
	subnet := mustParseCIDR(t, "10.2.2.0/24")
	created := false
	device.LinkAddFunc = func(link netlink.Link) error {
		created = true
		return nil
	}
	device.LinkByNameFunc = func(name string) (netlink.Link, error) {
		// simulate post-creation lookup
		return &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: name}}, nil
	}
	device.AddrReplaceFunc = func(link netlink.Link, addr *netlink.Addr) error {
		return nil
	}
	device.LinkSetUpFunc = func(link netlink.Link) error {
		return nil
	}

	link, ipnet, err := device.CreateBridge("brx", subnet)
	assert.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "brx", link.Attrs().Name)
	assert.Equal(t, "10.2.2.1", ipnet.IP.String())
}

// --- 6. ensureBridge ---
func TestEnsureBridge_New(t *testing.T) {
	defer restoreNetlinkMocks()

	// simulate LinkByName => not found, fallback to CreateBridge
	device.LinkByNameFunc = func(name string) (netlink.Link, error) {
		return nil, syscall.ENOENT
	}
	// delegate to CreateBridge
	created := false
	device.CreateBridgeFunc = func(name string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {
		created = true
		return &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: name}}, &net.IPNet{IP: net.ParseIP("10.3.3.1"), Mask: net.CIDRMask(24, 32)}, nil
	}

	subnet := mustParseCIDR(t, "10.3.3.0/24")
	link, ip, err := device.EnsureBridge("brz", subnet)
	assert.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "brz", link.Attrs().Name)
	assert.Equal(t, "10.3.3.1", ip.IP.String())
}

// --- 7. SetupBridge top-level ---
func TestSetupBridge_IntegrationMock(t *testing.T) {
	defer restoreNetlinkMocks()

	// reuse EnsureBridge
	ensureCalled := false
	device.EnsureBridgeFunc = func(name string, subnet *net.IPNet) (netlink.Link, *net.IPNet, error) {
		ensureCalled = true
		return &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: name}}, &net.IPNet{IP: net.ParseIP("10.4.4.1"), Mask: net.CIDRMask(24, 32)}, nil
	}

	subnet := mustParseCIDR(t, "10.4.4.0/24")
	link, ip, err := device.SetupBridge("mytenant", subnet)
	assert.NoError(t, err)
	assert.True(t, ensureCalled)
	assert.Equal(t, "mytenant", link.Attrs().Name)
	assert.Equal(t, "10.4.4.1", ip.IP.String())
}
