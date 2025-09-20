package network

import (
	// std
	"context"
	"fmt"
	"github/setera/pkg/network/subnet"
	"net"

	// internal
	"github/setera/pkg/data/trie"
	"github/setera/pkg/network/backend"
	"github/setera/pkg/network/ipam"

	// managers
	"github/setera/pkg/network/arp"
	"github/setera/pkg/network/fdb"
	"github/setera/pkg/network/iptable"
	"github/setera/pkg/network/route"

	"github.com/vishvananda/netlink"
)

type NetworkManager struct {
	RootCIDR *net.IPNet
	NodeName string

	TenantRecords map[string]*TenantRecord

	// deps
	Subnet   subnet.Manager
	Route    route.RouteManager
	ARP      arp.ARPManager
	FDB      fdb.FDBManager
	IPTables iptable.IPtableManager
}

type TenantRecord struct {
	Subnet  *net.IPNet
	Bridge  *BridgeRecord
	Vtep    *VxlanRecord
	Backend backend.Backend
	IPAM    ipam.IPAM
}

type BridgeRecord struct {
	Name       string
	IPAddress  *net.IPNet
	MACAddress net.HardwareAddr
}

type VxlanRecord struct {
	Name string
	IP   *net.IPNet
	MAC  net.HardwareAddr
	VNI  int
}

// NewNetworkManager wires the default registered managers.
// If you prefer explicit DI, add a NewNetworkManagerWithDeps that accepts the interfaces.
func NewNetworkManager(rootCIDR *net.IPNet, nodeName string) (*NetworkManager, error) {
	ipTrie := trie.NewTrie(rootCIDR)
	ipTrie.Build(30)

	nm := &NetworkManager{
		RootCIDR:      rootCIDR,
		NodeName:      nodeName,
		Trie:          ipTrie,
		TenantRecords: make(map[string]*TenantRecord),

		// pull the defaults (registered at package init of each impl)
		Route:    route.Manager(),   // default set by your netlink impl’s init() :contentReference[oaicite:0]{index=0}
		ARP:      arp.Manager(),     // default set by netlink ARP impl’s init() :contentReference[oaicite:1]{index=1}
		FDB:      fdb.Manager(),     // default set by netlink FDB impl’s init() :contentReference[oaicite:2]{index=2}
		IPTables: iptable.Manager(), // default set when iptablesmgr init created v4 manager
	}
	return nm, nil
}

// AllocateSubnet allocates a /30 from the trie.
func (m *NetworkManager) AllocateTenant(ctx context.Context, id string) (*net.IPNet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	node, err := m.Trie.AllocateSubnet(id)
	if err != nil || node == nil {
		m.Trie.DeallocateSubnet(id)
		if err != nil {
			return nil, fmt.Errorf("trie allocation: %w", err)
		}
		return nil, fmt.Errorf("no available subnet for tenant %s", id)
	}
	return node.Prefix, nil
}

// ConfigBridge: create/configure bridge via your backend.
func (m *NetworkManager) ConfigBridge(ctx context.Context, network *net.IPNet, id string) (string, *net.IPNet, net.HardwareAddr, error) {
	bridge, bridgeIP, err := backend.SetupBridge(id, network)
	if err != nil {
		m.Trie.DeallocateSubnet(id)
		return "", nil, nil, fmt.Errorf("failed to create bridge %s: %w", id, err)
	}
	return bridge.Attrs().Name, bridgeIP, bridge.Attrs().HardwareAddr, nil
}

// ConfigVxlan: create/configure VTEP via your backend.
func (m *NetworkManager) ConfigVxlan(ctx context.Context, network *net.IPNet, id string) (string, int, *net.IPNet, net.HardwareAddr, error) {
	vtep, vtepIP, err := backend.SetupVxlan(network, id, m.NodeName)
	if err != nil {
		m.Trie.DeallocateSubnet(id)
		return "", 0, nil, nil, fmt.Errorf("failed to create VTEP %s: %w", id, err)
	}
	return vtep.Attrs().Name, vtep.VxlanId, vtepIP, vtep.Attrs().HardwareAddr, nil
}

// ConfigIPAM: create per-tenant IPAM.
func (m *NetworkManager) ConfigIPAM(ctx context.Context, network *net.IPNet, id string) (ipam.IPAM, error) {
	ipamInstance, err := ipam.NewBitmapIPAM(network)
	if err != nil {
		m.Trie.DeallocateSubnet(id)
		return nil, fmt.Errorf("failed to create IPAM for tenant %s: %w", id, err)
	}
	return ipamInstance, nil
}

// AllocatePod – left as a TODO (depends on your CNI plumbing/veth helper).
func (m *NetworkManager) AllocatePod(tenant, containerID, ifName string) error {
	// 1) tenant record -> IPAM allocate
	// 2) create veth, move peer to pod ns, connect host end to bridge
	// 3) add default route inside pod via bridge IP using Route manager (ns variant)
	return nil
}

// RegisterTenant: allocate subnet, build L2, create IPAM, program iptables.
func (m *NetworkManager) RegisterTenant(tenantID string) (*SubnetRecord, error) {
	tenantSubnet, err := m.AllocateSubnet(context.Background(), tenantID)
	if err != nil {
		return nil, fmt.Errorf("allocate subnet for tenant %s: %w", tenantID, err)
	}

	bridgeName, bridgeIP, bridgeMac, err := m.ConfigBridge(context.Background(), tenantSubnet, tenantID)
	if err != nil {
		return nil, fmt.Errorf("config bridge for tenant %s: %w", tenantID, err)
	}
	bridgeRecord := &BridgeRecord{
		Name:       bridgeName,
		IPaddress:  bridgeIP.String(),
		MACaddress: bridgeMac.String(),
	}

	vtepName, vni, vtepIP, vtepMac, err := m.ConfigVxlan(context.Background(), tenantSubnet, tenantID)
	if err != nil {
		m.Trie.DeallocateSubnet(tenantID)
		return nil, fmt.Errorf("config VTEP for tenant %s: %w", tenantID, err)
	}
	vtepRecord := &VxlanRecord{
		Name: vtepName,
		IP:   vtepIP.String(),
		MAC:  vtepMac.String(),
		VNI:  vni,
	}

	ipamInstance, err := m.ConfigIPAM(context.Background(), tenantSubnet, tenantID)
	if err != nil {
		m.Trie.DeallocateSubnet(tenantID)
		return nil, fmt.Errorf("config IPAM for tenant %s: %w", tenantID, err)
	}

	// Program iptables isolation for this tenant (interface-based).
	// Accept intra-tenant (bridge <-> vxlan), default-drop, allow egress to uplinks if you pass them here.
	if m.IPTables != nil {
		if err := m.IPTables.EnsureTenantChains(tenantID); err != nil {
			return nil, err
		}
		if err := m.IPTables.EnsureTenantIsolationByIface(tenantID, bridgeName, vtepName /* uplinks... */); err != nil {
			return nil, err
		}
		_ = m.IPTables.EnsureForwardFastPath()
	}

	subnetRecord := &SubnetRecord{
		Network: tenantSubnet.String(),
		Bridge:  bridgeRecord,
		VTEP:    vtepRecord,
		IPAM:    ipamInstance,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.SubnetRecords[tenantID]; exists {
		return nil, fmt.Errorf("tenant %s already registered", tenantID)
	}
	m.SubnetRecords[tenantID] = subnetRecord
	return subnetRecord, nil
}

// ExpandTenant: keeps your trie+bridge behavior; iptables is iface-based so no rule churn needed here.
func (m *NetworkManager) ExpandTenant(ctx context.Context, id string) (*SubnetRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.Trie.MergeSubnet(id); err != nil {
		return nil, fmt.Errorf("trie merge: %w", err)
	}
	node := m.Trie.GetNodeByID(id)
	cidr := node.Prefix

	rec, ok := m.SubnetRecords[id]
	if !ok {
		return nil, fmt.Errorf("no subnet record for %s", id)
	}
	link, err := netlink.LinkByName(rec.Bridge.Name)
	if err != nil {
		return nil, fmt.Errorf("lookup bridge %s: %w", rec.Bridge.Name, err)
	}
	if err := netlink.AddrReplace(link, &netlink.Addr{IPNet: cidr}); err != nil {
		return nil, fmt.Errorf("addr replace %s: %w", cidr, err)
	}

	rec.Network = cidr.String()
	rec.Bridge.IPaddress = cidr.IP.Mask(cidr.Mask).String()
	m.SubnetRecords[id] = rec

	// If you keep any prefix-based iptables rules, update them here. (Not needed with iface isolation.)
	return rec, nil
}

// DeletetSubnet: also clears the iptables chain for the tenant.
func (m *NetworkManager) DeletetSubnet(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.Trie.DeallocateSubnet(id); err != nil {
		return fmt.Errorf("trie deallocate: %w", err)
	}
	if rec, ok := m.SubnetRecords[id]; ok {
		if m.IPTables != nil {
			_ = m.IPTables.DeleteTenantChains(id)
		}
		if link, err := netlink.LinkByName(rec.Bridge.Name); err == nil && link != nil {
			_ = netlink.LinkDel(link)
		}
		// TODO: delete VTEP link
		delete(m.SubnetRecords, id)
	}
	return nil
}

func (m *NetworkManager) ListTenants(ctx context.Context) map[string]*SubnetRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := make(map[string]*SubnetRecord, len(m.SubnetRecords))
	for k, v := range m.SubnetRecords {
		snap[k] = v
	}
	return snap
}

func (m *NetworkManager) GetSubnetRecord(id string) (*SubnetRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, exists := m.SubnetRecords[id]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", id)
	}
	return record, nil
}

// ConfigureRoutes now uses the managers (ARP/FDB/Route) instead of the old routing helpers.
func (m *NetworkManager) ConfigureRoutes(
	localVtepName string,
	remoteTenantCIDR *net.IPNet,
	remoteNodeIP *net.IPNet,
	remoteVtepIP net.IP,
	remoteVtepMac net.HardwareAddr,
) error {

	// 1) ARP (neighbor) entry for remote VTEP on the local VTEP device.
	if err := m.ARP.Add(arp.ARPEntry{
		Device: localVtepName,
		IP:     remoteVtepIP,
		MAC:    remoteVtepMac,
	}); err != nil {
		return fmt.Errorf("arp add: %w", err)
	} // :contentReference[oaicite:3]{index=3}

	// 2) FDB entry mapping remote host IP -> remote VTEP MAC on the vxlan device.
	if err := m.FDB.Add(fdb.FDBEntry{
		Device: localVtepName,
		IP:     remoteNodeIP.IP,
		Mac:    remoteVtepMac,
	}); err != nil {
		return fmt.Errorf("fdb add: %w", err)
	} // :contentReference[oaicite:4]{index=4}

	// 3) Routes:
	// 3a) onlink /32 (or /128) host route to the remote VTEP IP on the vxlan device
	mask := 32
	if remoteVtepIP.To4() == nil {
		mask = 128
	}
	vtepHost := &net.IPNet{IP: remoteVtepIP, Mask: net.CIDRMask(mask, 8*len(remoteVtepIP))}
	if err := m.Route.Ensure(&route.Route{
		Dst:    vtepHost,
		Device: localVtepName,
		Onlink: true,
	}); err != nil {
		return fmt.Errorf("ensure vtep-host route: %w", err)
	} // :contentReference[oaicite:5]{index=5}

	// 3b) tenant CIDR via remote VTEP IP (onlink) on the vxlan device
	if err := m.Route.Ensure(&route.Route{
		Dst:     remoteTenantCIDR,
		Device:  localVtepName,
		Gateway: remoteVtepIP,
		Onlink:  true,
	}); err != nil {
		return fmt.Errorf("ensure tenant route: %w", err)
	} // :contentReference[oaicite:6]{index=6}

	return nil
}
