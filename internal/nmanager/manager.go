package nmanager

import (
	// std
	"bytes"
	"context"
	"fmt"
	"log"
	"net"

	"github/setera/pkg/network/subnet"
	subtrie "github/setera/pkg/network/subnet/trie"

	// managers
	"github/setera/pkg/network/arp"
	_ "github/setera/pkg/network/arp/netlinkarp"
	"github/setera/pkg/network/fdb"
	_ "github/setera/pkg/network/fdb/netlinkfdb"
	"github/setera/pkg/network/policy"
	_ "github/setera/pkg/network/policy/tenant"
	"github/setera/pkg/network/route"
	_ "github/setera/pkg/network/route/netlinkroute"
)

// NewNetworkManager wires the default registered managers.
// If you prefer explicit DI, add a NewNetworkManagerWithDeps that accepts the interfaces.
func NewNetworkManager(rootCIDR *net.IPNet, nodeName string) (*NetworkManagerImpl, error) {
	return NewNetworkManagerWithDeps(rootCIDR, nodeName, Deps{})
}

// NewNetworkManagerWithDeps wires the network manager with explicitly provided dependencies.
// Any nil dependency falls back to the package default (Manager()).
func NewNetworkManagerWithDeps(rootCIDR *net.IPNet, nodeName string, d Deps) (*NetworkManagerImpl, error) {

	// Default dependencies if not provided
	if d.Route == nil {
		d.Route = route.Manager()
	}
	if d.ARP == nil {
		d.ARP = arp.Manager()
	}
	if d.FDB == nil {
		d.FDB = fdb.Manager()
	}
	if d.TP == nil {
		d.TP = policy.Manager()
	}
	if d.Subnet == nil {
		if sm := subnet.Manager(); sm != nil {
			d.Subnet = sm
		} else {
			// No default SubnetManager registered: create a trie-backed manager and set it as default
			tm := subtrie.NewTrieManager(rootCIDR, 30)
			subnet.RegisterSubnetManager(tm)
			d.Subnet = tm
		}
	} else {
		// If we received a trie-backed manager with a different root, recreate to match this rootCIDR
		if tm, ok := d.Subnet.(*subtrie.TrieManager); ok {
			if !cidrEqual(tm.Root(), rootCIDR) {
				d.Subnet = subtrie.NewTrieManager(rootCIDR, 30)
			}
		}
	}

	// No factories: backend and ipam are provided via package-level constructors.

	nm := &NetworkManagerImpl{
		RootCIDR:      rootCIDR,
		NodeName:      nodeName,
		TenantRecords: make(map[string]*TenantRecord),
		TenantActors:  make(map[string]TenantActor),

		// deps
		Route:  d.Route,
		ARP:    d.ARP,
		FDB:    d.FDB,
		TP:     d.TP,
		Subnet: d.Subnet,
	}
	return nm, nil
}

// AllocateSubnet allocates a /30 from the trie.
func (m *NetworkManagerImpl) AllocateTenant(ctx context.Context, id string) (*net.IPNet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	anet, err := m.Subnet.Allocate(id)
	if err != nil || anet == nil {
		_ = m.Subnet.Deallocate(id)
		if err != nil {
			return nil, fmt.Errorf("allocation: %w", err)
		}
		return nil, fmt.Errorf("no available subnet for tenant %s", id)
	}

	return anet, nil
}

// RegisterTenant: allocate subnet, build L2, create IPAM, program iptables.
func (m *NetworkManagerImpl) RegisterTenant(tenantID string) (*TenantRecord, error) {
	if err := m.EnsureTenant(context.Background(), tenantID); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.TenantRecords[tenantID]
	if !ok {
		return nil, fmt.Errorf("tenant %s not found after ensure", tenantID)
	}
	return rec, nil
}

// ExpandTenant: keeps your trie+bridge behavior; iptables is iface-based so no rule churn needed here.
// ExpandTenant expands a tenant's subnet via SubnetManager and updates backend/ipam accordingly.
// Returns the updated TenantRecord.
func (m *NetworkManagerImpl) ExpandTenant(ctx context.Context, id string) (*TenantRecord, error) {
	m.mu.RLock()
	rec, ok := m.TenantRecords[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no tenant record for %s", id)
	}
	log.Printf("tenant=%s expand: current subnet=%s", id, rec.Subnet.String())
	newNet, err := m.Subnet.Expand(id)
	if err != nil {
		return nil, fmt.Errorf("subnet expand: %w", err)
	}
	log.Printf("tenant=%s expand: subnet manager returned %s", id, newNet.String())
	if rec.Backend != nil {
		log.Printf("tenant=%s expand: updating backend devices", id)
		if err := rec.Backend.Update(newNet, m.NodeName); err != nil {
			return nil, fmt.Errorf("backend update: %w", err)
		}
	}
	if rec.IPAM != nil {
		log.Printf("tenant=%s expand: updating IPAM bitmap", id)
		if err := rec.IPAM.Expand(newNet); err != nil {
			return nil, fmt.Errorf("ipam expand: %w", err)
		}
	}
	m.mu.Lock()
	rec.Subnet = newNet
	m.TenantRecords[id] = rec
	m.mu.Unlock()
	log.Printf("tenant=%s expand: completed new subnet=%s", id, newNet.String())
	return rec, nil
}

// DeletetSubnet: also clears the iptables chain for the tenant.
func (m *NetworkManagerImpl) DeletetSubnet(ctx context.Context, id string) error {
	return m.RemoveTenant(ctx, id)
}

func (m *NetworkManagerImpl) ListTenants(ctx context.Context) map[string]*TenantRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap := make(map[string]*TenantRecord, len(m.TenantRecords))
	for k, v := range m.TenantRecords {
		snap[k] = v
	}
	return snap
}

func (m *NetworkManagerImpl) GetSubnetRecord(id string) (*TenantRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, exists := m.TenantRecords[id]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", id)
	}
	return record, nil
}

// ConfigureRoutes now uses the managers (ARP/FDB/Route) instead of the old routing helpers.
func (m *NetworkManagerImpl) ConfigureRoutes(
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
	}); err != nil {
		return fmt.Errorf("ensure vtep-host route: %w", err)
	} // :contentReference[oaicite:5]{index=5}

	// 3b) tenant CIDR via remote VTEP IP (onlink) on the vxlan device
	if err := m.Route.Ensure(&route.Route{
		Dst:     remoteTenantCIDR,
		Device:  localVtepName,
		Gateway: remoteVtepIP,
	}); err != nil {
		return fmt.Errorf("ensure tenant route: %w", err)
	} // :contentReference[oaicite:6]{index=6}

	return nil
}

// cidrEqual compares two *net.IPNet for exact IP+mask equality.
func cidrEqual(a, b *net.IPNet) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.IP.Equal(b.IP) && bytes.Equal(a.Mask, b.Mask)
}

func ipEqual(a, b net.IP) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0
	}
	return a.Equal(b)
}

func macEqual(a, b net.HardwareAddr) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0
	}
	return bytes.Equal(a, b)
}
