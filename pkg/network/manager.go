package network

import (

	//std
	"context"
	"fmt"
	"net"
	"sync"

	// internal packages
	"github/setera/pkg/data/trie"
	"github/setera/pkg/network/backend"

	"github.com/vishvananda/netlink"
)

type NetworkManager struct {
	mu            sync.RWMutex
	Trie          *trie.IPTrie             // The trie that records the allocated and non-allocated subnets
	SubnetRecords map[string]*SubnetRecord // tenantID <--> subnetRecord

}

func NewNetworkManager(rootCIDR *net.IPNet) (*NetworkManager, error) {

	ipTrie := trie.NewTrie(rootCIDR)
	ipTrie.Build(30) // Build trie down to /30 subnets

	return &NetworkManager{
		Trie:          ipTrie,
		SubnetRecords: make(map[string]*SubnetRecord),
	}, nil
}

// AllocateTenant carves out the best /30 for tenantID, sets up bridge & VTEP.
func (m *NetworkManager) AllocateSubnet(ctx context.Context, id string) (*SubnetRecord, error) {

	m.mu.Lock()
	defer m.mu.Unlock()

	//trie subnet allocation
	node, err := m.Trie.AllocateSubnet(id)
	if err != nil {
		return nil, fmt.Errorf("trie allocation: %w", err)
	}

	//create subnet record - should be called directly from the function new subnet record

	//bridge creation
	bridge, bridgeIP, err := backend.CreateBridge(id, node.Prefix)
	if err != nil {
		m.Trie.DeallocateSubnet(id)
		return nil, fmt.Errorf("failed to create bridge %s: %w", id, err)
	}

	// 5) create subnet record
	rec := &SubnetRecord{
		Network: node.Prefix,
		Bridge: &BridgeRecord{
			Name:      bridge.Attrs().Name,
			GatewayIP: bridgeIP.String(),
		},
		VTEP: nil, // fill in once you create it
		IPs:  make(map[string]ContainerNetInfo),
	}
	m.SubnetRecords[id] = rec
	return rec, nil
}

// ExpandTenant merges sibling subnets (via trie), then updates bridge IP.
func (m *NetworkManager) ExpandTenant(ctx context.Context, id string) (*SubnetRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1) merge via trie
	if err := m.Trie.MergeSubnet(id); err != nil {
		return nil, fmt.Errorf("trie merge: %w", err)
	}
	node := m.Trie.GetNodeByID(id)
	cidr := node.Prefix

	// 2) update bridge address
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

	// 3) TODO: update VTEP, iptables, routes for new CIDR

	// 4) update record
	rec.Network = cidr
	rec.Bridge.GatewayIP = cidr.IP.Mask(cidr.Mask).String()
	m.SubnetRecords[id] = rec

	return rec, nil
}

// DeleteTenant tears down bridge/VTEP and frees the subnet in the trie.
func (m *NetworkManager) DeletetSubnet(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1) free trie allocation
	if err := m.Trie.DeallocateSubnet(id); err != nil {
		return fmt.Errorf("trie deallocate: %w", err)
	}

	// 2) teardown bridge
	if rec, ok := m.SubnetRecords[id]; ok {
		if link, err := netlink.LinkByName(rec.Bridge.Name); err == nil && link != nil {
			netlink.LinkDel(link)
		}
		// TODO: teardown VTEP
		delete(m.SubnetRecords, id) // remove from records
	}

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
