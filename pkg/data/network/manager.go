package network

import (

	//std
	"context"
	"fmt"
	"net"
	"sync"

	// internal packages
	"github/setera/pkg/data/trie"
)

type NetworkManager struct {
	mu            sync.RWMutex
	Trie          *trie.IPTrie                    // The trie that records the allocated and non-allocated subnets
	SubnetRecords map[string]*SubnetRecord        // tenantID <--> subnetRecord
	watchers      []chan map[string]*SubnetRecord // channels to notify watchers of subnet changes
}

func NewNetworkManager(rootCIDR *net.IPNet) (*NetworkManager, error) {

	ipTrie := trie.NewTrie(rootCIDR)
	ipTrie.Build(30) // Build trie down to /30 subnets

	return &NetworkManager{
		Trie:          ipTrie,
		SubnetRecords: make(map[string]*SubnetRecord),
		watchers:      []chan map[string]*SubnetRecord{},
	}, nil
}

// AllocateTenant carves out the best /30 for tenantID, sets up bridge & VTEP.
func (m *NetworkManager) AllocateTenant(ctx context.Context, tenantID string) (*SubnetRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1) carve out subnet
	node, err := m.Trie.AllocateTenantSubnet(tenantID)
	if err != nil {
		return nil, fmt.Errorf("trie allocation: %w", err)
	}
	cidr := node.Prefix

	// 2) create bridge
	brName := "br-" + tenantID
	bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: brName}}
	if err := netlink.LinkAdd(bridge); err != nil && err != netlink.ErrLinkExists {
		return nil, fmt.Errorf("add bridge %s: %w", brName, err)
	}

	// 3) assign the gateway IP (first IP of the CIDR) to the bridge
	ip := cidr.IP.Mask(cidr.Mask)
	gateway := ip.String()
	addr := &netlink.Addr{IPNet: cidr}
	if err := netlink.AddrAdd(bridge, addr); err != nil {
		return nil, fmt.Errorf("addr add %s: %w", cidr, err)
	}
	if err := netlink.LinkSetUp(bridge); err != nil {
		return nil, fmt.Errorf("link up %s: %w", brName, err)
	}

	// 4) TODO: create Vxlan/VTEP device, attach to bridge, set up overlay

	// 5) record state
	rec := &SubnetRecord{
		Network: cidr,
		Bridge: &BridgeRecord{
			Name:      brName,
			GatewayIP: gateway,
		},
		VTEP: nil, // fill in once you create it
		IPs:  make(map[string]ContainerNetInfo),
	}
	m.SubnetRecords[tenantID] = rec
	m.notifyWatchers()
	return rec, nil
}

// ExpandTenant merges sibling subnets (via trie), then updates bridge IP.
func (m *NetworkManager) ExpandTenant(ctx context.Context, tenantID string) (*SubnetRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1) merge via trie
	if err := m.Trie.MergeSubnet(tenantID); err != nil {
		return nil, fmt.Errorf("trie merge: %w", err)
	}
	node := m.Trie.GetNodeByTenantID(tenantID)
	cidr := node.Prefix

	// 2) update bridge address
	rec, ok := m.SubnetRecords[tenantID]
	if !ok {
		return nil, fmt.Errorf("no subnet record for %s", tenantID)
	}
	link, err := netlink.LinkByName(rec.Bridge.Name)
	if err != nil {
		return nil, fmt.Errorf("lookup bridge %s: %w", rec.Bridge.Name, err)
	}
	if err := netlink.AddrReplace(link, &netlink.Addr{IPNet: cidr}); err != nil {
		return nil, fmt.Errorf("addr replace %s: %w", cidr, err)
	}

	// 3) TODO: update VTEP, iptables, routes for new CIDR

	// 4) update record
	rec.Network = cidr
	rec.Bridge.GatewayIP = cidr.IP.Mask(cidr.Mask).String()
	m.SubnetRecords[tenantID] = rec
	m.notifyWatchers()
	return rec, nil
}

// DeleteTenant tears down bridge/VTEP and frees the subnet in the trie.
func (m *NetworkManager) DeleteTenant(ctx context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1) free trie allocation
	if err := m.Trie.DeallocateTenant(tenantID); err != nil {
		return fmt.Errorf("trie deallocate: %w", err)
	}

	// 2) teardown bridge
	if rec, ok := m.SubnetRecords[tenantID]; ok {
		if link, err := netlink.LinkByName(rec.Bridge.Name); err == nil && link != nil {
			netlink.LinkDel(link)
		}
		// TODO: teardown VTEP
		delete(m.SubnetRecords, tenantID)
	}

	m.notifyWatchers()
	return nil
}

// ListTenants returns a snapshot of all current SubnetRecords.
func (m *NetworkManager) ListTenants(ctx context.Context) map[string]*SubnetRecord {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := make(map[string]*SubnetRecord, len(m.SubnetRecords))
	for k, v := range m.SubnetRecords {
		snap[k] = v
	}
	return snap
}

// WatchState returns a channel that will receive the full map on every change.
func (m *NetworkManager) WatchState(ctx context.Context) (<-chan map[string]*SubnetRecord, error) {
	ch := make(chan map[string]*SubnetRecord, 1)
	m.mu.Lock()
	m.watchers = append(m.watchers, ch)
	m.mu.Unlock()

	// send initial snapshot
	ch <- m.ListTenants(ctx)
	return ch, nil
}

func (m *NetworkManager) notifyWatchers() {
	snap := make(map[string]*SubnetRecord, len(m.SubnetRecords))
	for k, v := range m.SubnetRecords {
		snap[k] = v
	}

	for _, ch := range m.watchers {
		select {
		case ch <- snap:
		default:
		}
	}
}
