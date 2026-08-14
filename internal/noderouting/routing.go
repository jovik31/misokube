package noderouting

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"

	"github/setera/pkg/ebpf"
	"github/setera/pkg/network"
)

// RemoteNode contains the transport information required to reach one remote
// Node. Tenant membership deliberately does not participate in node routing.
type RemoteNode struct {
	Name       string
	PodCIDR    netip.Prefix
	UnderlayIP netip.Addr
	VTEPIP     netip.Addr
	VTEPMAC    net.HardwareAddr
}

type networkOps interface {
	EnsureVXLAN(network.VXLANConfig) (network.VXLANLink, error)

	ReplaceRoute(network.Route) error
	DeleteRoute(network.Route) error
	ListRoutes(int) ([]network.Route, error)

	SetNeighbor(network.Neighbor) error
	DeleteNeighbor(network.Neighbor) error
	ListNeighbors(int) ([]network.Neighbor, error)

	SetFDB(network.FDBEntry) error
	DeleteFDB(network.FDBEntry) error
	ListFDB(int) ([]network.FDBEntry, error)
}

type nodeProgram interface {
	Close() error
}

type nodeProgramAttacher func(string) (nodeProgram, error)

// Routing owns the node-wide VXLAN transport and its node-level eBPF program.
// Kubernetes-specific watching and metadata publication live in nodewatcher.
type Routing struct {
	mu sync.Mutex

	network networkOps
	config  Config

	attachNodeProgram nodeProgramAttacher
	setVXLANIfIndex   func(int) error
	program           nodeProgram
	local             *LocalNode
}

// New creates node routing with one shared node-wide VXLAN interface.
func New(networkOps networkOps, config Config) (*Routing, error) {
	if networkOps == nil {
		return nil, fmt.Errorf("noderouting: network operations are nil")
	}
	if strings.TrimSpace(config.InterfaceName) == "" {
		return nil, fmt.Errorf("noderouting: VXLAN interface name is empty")
	}
	if config.VNI <= 0 || config.VNI > 1<<24-1 {
		return nil, fmt.Errorf("noderouting: invalid VNI %d", config.VNI)
	}
	if config.Port <= 0 || config.Port > 65535 {
		return nil, fmt.Errorf("noderouting: invalid VXLAN port %d", config.Port)
	}
	if config.MTU <= 0 {
		return nil, fmt.Errorf("noderouting: invalid MTU %d", config.MTU)
	}

	return &Routing{
		network: networkOps,
		config:  config,
		attachNodeProgram: func(ifName string) (nodeProgram, error) {
			return ebpf.AttachNodeProgram(ifName)
		},
		setVXLANIfIndex: ebpf.SetVXLANIfIndex,
	}, nil
}

// EnsureLocal creates/reuses the local VXLAN VTEP, binds it to the Node
// InternalIP used as the underlay source, publishes its ifindex for Pod TC
// programs, and attaches the node eBPF program. It is safe to call again with
// the same PodCIDR and underlay address.
func (r *Routing) EnsureLocal(podCIDR netip.Prefix, underlayIP netip.Addr) (LocalNode, error) {
	if r == nil {
		return LocalNode{}, fmt.Errorf("noderouting: routing is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ensureLocalLocked(podCIDR, underlayIP)
}

// ReconcileRemoteNodes makes the dedicated VXLAN interface match the complete
// desired remote-node set.
//
// Programming order is FDB -> neighbor -> route, so a route is exposed only
// after its L2/VXLAN resolution exists. Stale cleanup happens in the reverse
// direction. The sweep uses actual kernel state, so a fresh daemon removes
// entries left behind by a previous crashed process.
func (r *Routing) ReconcileRemoteNodes(ctx context.Context, desired []RemoteNode) error {
	if r == nil {
		return fmt.Errorf("noderouting: routing is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.local == nil {
		return fmt.Errorf("noderouting: local VTEP is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	nodes, err := normalizeRemoteNodes(desired)
	if err != nil {
		return err
	}

	ifIndex := r.local.IfIndex
	desiredRoutes := make(map[string]network.Route, len(nodes))
	desiredNeighbors := make(map[string]network.Neighbor, len(nodes))
	desiredFDB := make(map[string]network.FDBEntry, len(nodes))

	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return err
		}

		fdb := fdbFor(ifIndex, node)
		neighbor := neighborFor(ifIndex, node)
		route := routeFor(ifIndex, node)

		if err := r.network.SetFDB(fdb); err != nil {
			return fmt.Errorf("noderouting: set FDB for Node %s: %w", node.Name, err)
		}
		if err := r.network.SetNeighbor(neighbor); err != nil {
			return fmt.Errorf("noderouting: set neighbor for Node %s: %w", node.Name, err)
		}
		if err := r.network.ReplaceRoute(route); err != nil {
			return fmt.Errorf("noderouting: set route for Node %s: %w", node.Name, err)
		}

		desiredFDB[fdbKey(fdb)] = fdb
		desiredNeighbors[neighborKey(neighbor)] = neighbor
		desiredRoutes[routeKey(route)] = route
	}

	var cleanupErr error

	actualRoutes, err := r.network.ListRoutes(ifIndex)
	if err != nil {
		return fmt.Errorf("noderouting: list VXLAN routes: %w", err)
	}
	for _, route := range actualRoutes {
		// The VTEP's own /32 lives in the local routing table. Setera remote
		// routes in the main table are the routes with an explicit gateway.
		if !route.Gateway.IsValid() {
			continue
		}
		if _, ok := desiredRoutes[routeKey(route)]; ok {
			continue
		}
		if err := r.network.DeleteRoute(route); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete stale route %s: %w", route.Prefix, err))
		}
	}

	actualNeighbors, err := r.network.ListNeighbors(ifIndex)
	if err != nil {
		return errors.Join(cleanupErr, fmt.Errorf("noderouting: list VXLAN neighbors: %w", err))
	}
	for _, neighbor := range actualNeighbors {
		if _, ok := desiredNeighbors[neighborKey(neighbor)]; ok {
			continue
		}
		if err := r.network.DeleteNeighbor(neighbor); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete stale neighbor %s: %w", neighbor.IP, err))
		}
	}

	actualFDB, err := r.network.ListFDB(ifIndex)
	if err != nil {
		return errors.Join(cleanupErr, fmt.Errorf("noderouting: list VXLAN FDB: %w", err))
	}
	for _, entry := range actualFDB {
		if _, ok := desiredFDB[fdbKey(entry)]; ok {
			continue
		}
		if err := r.network.DeleteFDB(entry); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete stale FDB %s: %w", entry.RemoteIP, err))
		}
	}

	return cleanupErr
}

// Close explicitly detaches the node eBPF program. The daemon does not call
// this on ordinary shutdown so an already-established node datapath survives a
// process restart; this method exists for tests and explicit teardown.
func (r *Routing) Close() error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.program == nil {
		return nil
	}
	err := r.program.Close()
	r.program = nil
	return err
}

func normalizeRemoteNodes(nodes []RemoteNode) ([]RemoteNode, error) {
	out := make([]RemoteNode, 0, len(nodes))
	byName := make(map[string]RemoteNode, len(nodes))
	byPrefix := make(map[string]string, len(nodes))

	for _, node := range nodes {
		normalized, err := normalizeRemoteNode(node)
		if err != nil {
			return nil, err
		}

		if old, ok := byName[normalized.Name]; ok {
			if !remoteNodeEqual(old, normalized) {
				return nil, fmt.Errorf("noderouting: conflicting desired state for Node %s", normalized.Name)
			}
			continue
		}

		prefixKey := normalized.PodCIDR.String()
		if oldName, ok := byPrefix[prefixKey]; ok && oldName != normalized.Name {
			return nil, fmt.Errorf(
				"noderouting: Nodes %s and %s both own PodCIDR %s",
				oldName,
				normalized.Name,
				normalized.PodCIDR,
			)
		}

		byName[normalized.Name] = normalized
		byPrefix[prefixKey] = normalized.Name
		out = append(out, normalized)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func normalizeRemoteNode(node RemoteNode) (RemoteNode, error) {
	node.Name = strings.TrimSpace(node.Name)
	if node.Name == "" {
		return RemoteNode{}, fmt.Errorf("noderouting: remote Node name is empty")
	}

	node.PodCIDR = node.PodCIDR.Masked()
	expectedVTEP, err := VTEPAddress(node.PodCIDR)
	if err != nil {
		return RemoteNode{}, fmt.Errorf("noderouting: Node %s: %w", node.Name, err)
	}

	node.UnderlayIP = node.UnderlayIP.Unmap()
	if !node.UnderlayIP.IsValid() || !node.UnderlayIP.Is4() || node.UnderlayIP.Zone() != "" || node.UnderlayIP.IsUnspecified() {
		return RemoteNode{}, fmt.Errorf("noderouting: Node %s has invalid IPv4 underlay IP %s", node.Name, node.UnderlayIP)
	}

	node.VTEPIP = node.VTEPIP.Unmap()
	if !node.VTEPIP.IsValid() || !node.VTEPIP.Is4() || node.VTEPIP.Zone() != "" {
		return RemoteNode{}, fmt.Errorf("noderouting: Node %s has invalid IPv4 VTEP IP %s", node.Name, node.VTEPIP)
	}
	if node.VTEPIP != expectedVTEP.Addr() {
		return RemoteNode{}, fmt.Errorf(
			"noderouting: Node %s VTEP IP %s does not match first PodCIDR address %s",
			node.Name,
			node.VTEPIP,
			expectedVTEP.Addr(),
		)
	}

	if len(node.VTEPMAC) != 6 {
		return RemoteNode{}, fmt.Errorf("noderouting: Node %s has invalid VTEP MAC %q", node.Name, node.VTEPMAC)
	}
	node.VTEPMAC = append(net.HardwareAddr(nil), node.VTEPMAC...)

	return node, nil
}

func remoteNodeEqual(a, b RemoteNode) bool {
	return a.Name == b.Name &&
		a.PodCIDR == b.PodCIDR &&
		a.UnderlayIP == b.UnderlayIP &&
		a.VTEPIP == b.VTEPIP &&
		a.VTEPMAC.String() == b.VTEPMAC.String()
}