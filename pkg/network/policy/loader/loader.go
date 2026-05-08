package firewall

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// Action constants matching the BPF program defines.
const (
	tcPodIDsMapPath          = "/sys/fs/bpf/setera/tc/tc_podIDs"
	ActionDrop        uint32 = 0
	ActionAllow       uint32 = 1
	ActionLog         uint32 = 2
	tcFlagConntrack   uint32 = 1 << 0
	tcPodIDsValueSize        = 68
)

type tcPodIDValue struct {
	Tenant      [64]byte
	VethIfindex uint32
}

// Rule represents a firewall rule that can be inserted into the BPF maps.
type Rule struct {
	SrcIP     net.IP
	DstIP     net.IP
	SrcPort   uint16
	DstPort   uint16
	Protocol  uint8
	SrcPrefix uint8
	DstPrefix uint8
	Action    uint32
}

// ruleKeyHash is the concrete key type used for tracking installed rules.
type ruleKeyHash struct {
	SrcIp     uint32
	DstIp     uint32
	SrcPort   uint16
	DstPort   uint16
	Protocol  uint8
	SrcPrefix uint8
	DstPrefix uint8
	Pad       uint8
}

type lpmV4Key struct {
	Prefixlen uint32
	Addr      uint32
}

func isSrcCIDRRule(r Rule) bool {
	return r.SrcIP != nil && r.SrcPrefix > 0 && r.SrcPrefix < 32 && r.DstIP == nil && r.DstPort == 0 && r.SrcPort == 0 && r.Protocol == 0
}

func isDstCIDRRule(r Rule) bool {
	return r.DstIP != nil && r.DstPrefix > 0 && r.DstPrefix < 32 && r.SrcIP == nil && r.DstPort == 0 && r.SrcPort == 0 && r.Protocol == 0
}

func maskIPv4(addr uint32, prefix uint8) uint32 {
	if prefix == 0 {
		return 0
	}
	if prefix >= 32 {
		return addr
	}
	buf := make([]byte, 4)
	binary.NativeEndian.PutUint32(buf, addr)
	remaining := int(prefix)
	for i := 0; i < len(buf); i++ {
		switch {
		case remaining >= 8:
			remaining -= 8
		case remaining > 0:
			buf[i] &= byte(0xff << (8 - remaining))
			remaining = 0
		default:
			buf[i] = 0
		}
	}
	return binary.NativeEndian.Uint32(buf)
}

func srcCIDRKey(r Rule) lpmV4Key {
	addr := maskIPv4(ipToU32(r.SrcIP), r.SrcPrefix)
	return lpmV4Key{Prefixlen: uint32(r.SrcPrefix), Addr: addr}
}

func dstCIDRKey(r Rule) lpmV4Key {
	addr := maskIPv4(ipToU32(r.DstIP), r.DstPrefix)
	return lpmV4Key{Prefixlen: uint32(r.DstPrefix), Addr: addr}
}

// XDPFirewall manages the XDP firewall program lifecycle.
type XDPFirewall struct {
	objs  xdpFirewallObjects
	link  link.Link
	iface string
	rules map[ruleKeyHash]uint32
}

// TCFirewall manages the TC firewall program lifecycle.
type TCFirewall struct {
	objs      tcFirewallObjects
	ingressFD int
	egressFD  int
	iface     string
	qdisc     *netlink.GenericQdisc
	rules     map[ruleKeyHash]uint32
}

// NodeRouter manages the node-level TC program lifecycle.
type NodeRouter struct {
	objs      nodeRouterObjects
	ingressFD int
	egressFD  int
	iface     string
	qdisc     *netlink.GenericQdisc
	rules     map[ruleKeyHash]uint32
}

var tcLoadMu sync.Mutex

var tcPodIDsShared struct {
	mu    sync.Mutex
	mapFD *ebpf.Map
}

func getOrCreateSharedTcPodIDsMap() (*ebpf.Map, error) {
	tcPodIDsShared.mu.Lock()
	defer tcPodIDsShared.mu.Unlock()

	if tcPodIDsShared.mapFD != nil {
		return tcPodIDsShared.mapFD, nil
	}

	m, err := ebpf.LoadPinnedMap(tcPodIDsMapPath, nil)
	if err == nil {
		tcPodIDsShared.mapFD = m
		return m, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("open pinned map %s: %w", tcPodIDsMapPath, err)
	}

	m, err = ebpf.NewMap(&ebpf.MapSpec{
		Name:       "tc_podIDs",
		Type:       ebpf.Hash,
		KeySize:    4,
		ValueSize:  tcPodIDsValueSize,
		MaxEntries: 256,
	})
	if err != nil {
		return nil, fmt.Errorf("create shared tc_podIDs map: %w", err)
	}
	if err := m.Pin(tcPodIDsMapPath); err != nil {
		if !errors.Is(err, os.ErrExist) {
			m.Close()
			return nil, fmt.Errorf("pin shared tc_podIDs map: %w", err)
		}
		m.Close()
		m, err = ebpf.LoadPinnedMap(tcPodIDsMapPath, nil)
		if err != nil {
			return nil, fmt.Errorf("reopen shared tc_podIDs map: %w", err)
		}
	}

	tcPodIDsShared.mapFD = m
	return m, nil
}

func loadTcFirewallObjectsWithSharedPodMap(obj interface{}, opts *ebpf.CollectionOptions) error {
	return loadTcFirewallObjectsWithTenant(obj, opts, "")
}

func loadTcFirewallObjectsWithTenant(obj interface{}, opts *ebpf.CollectionOptions, tenant string) error {
	spec, err := loadTcFirewall()
	if err != nil {
		return err
	}
	if ms, ok := spec.Maps["tc_podIDs"]; ok && ms != nil {
		// Map is always injected via replacement, so don't attempt pin-by-name for this map.
		ms.Pinning = ebpf.PinNone
	}
	if tenant != "" {
		if len(tenant) > 63 {
			tenant = tenant[:63]
		}
		if vs, ok := spec.Variables["my_tenant"]; ok && vs != nil {
			var t [64]byte
			copy(t[:], tenant)
			if err := vs.Set(t); err != nil {
				return fmt.Errorf("set tc variable my_tenant: %w", err)
			}
		}
	}
	return spec.LoadAndAssign(obj, opts)
}

func loadNodeRouterObjectsWithTenant(obj interface{}, opts *ebpf.CollectionOptions, tenant string) error {
	spec, err := loadNodeRouter()
	if err != nil {
		return err
	}
	if ms, ok := spec.Maps["tc_podIDs"]; ok && ms != nil {
		ms.Pinning = ebpf.PinNone
	}
	if tenant != "" {
		if len(tenant) > 63 {
			tenant = tenant[:63]
		}
		if vs, ok := spec.Variables["my_tenant"]; ok && vs != nil {
			var t [64]byte
			copy(t[:], tenant)
			if err := vs.Set(t); err != nil {
				return fmt.Errorf("set node router variable my_tenant: %w", err)
			}
		}
	}
	return spec.LoadAndAssign(obj, opts)
}

// NewXDPFirewall loads and attaches the XDP firewall to the given interface.
func NewXDPFirewall(ifaceName string) (*XDPFirewall, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("lookup interface %q: %w", ifaceName, err)
	}

	var objs xdpFirewallObjects
	if err := loadXdpFirewallObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("load XDP objects: %w", err)
	}

	l, err := link.AttachXDP(link.XDPOptions{
		Program:   objs.XdpFirewall,
		Interface: iface.Index,
	})
	if err != nil {
		objs.Close()
		return nil, fmt.Errorf("attach XDP to %q: %w", ifaceName, err)
	}

	return &XDPFirewall{
		objs:  objs,
		link:  l,
		iface: ifaceName,
		rules: make(map[ruleKeyHash]uint32),
	}, nil
}

/* AddRule inserts a firewall rule into the XDP BPF map.
func (fw *XDPFirewall) AddRule(r Rule) error {
	key := fwRuleKey(r)
	val := xdpFirewallFwRuleVal{
		Action: r.Action,
	}
	switch {
	case isSrcCIDRRule(r):
		if err := fw.objs.XdpFwRulesCIDR_src.Update(srcCIDRKey(r), val, ebpf.UpdateAny); err != nil {
			return err
		}
	case isDstCIDRRule(r):
		if err := fw.objs.XdpFwRulesCIDR_dst.Update(dstCIDRKey(r), val, ebpf.UpdateAny); err != nil {
			return err
		}
	default:
		if err := fw.objs.XdpFwRules.Update(key, val, ebpf.UpdateAny); err != nil {
			return err
		}
	}
	fw.rules[key] = r.Action
	return nil
}

// DeleteRule removes a firewall rule from the XDP BPF map.
func (fw *XDPFirewall) DeleteRule(r Rule) error {
	key := fwRuleKey(r)
	switch {
	case isSrcCIDRRule(r):
		if err := fw.objs.XdpFwRulesCIDR_src.Delete(srcCIDRKey(r)); err != nil {
			return err
		}
	case isDstCIDRRule(r):
		if err := fw.objs.XdpFwRulesCIDR_dst.Delete(dstCIDRKey(r)); err != nil {
			return err
		}
	default:
		if err := fw.objs.XdpFwRules.Delete(key); err != nil {
			return err
		}
	}
	delete(fw.rules, key)
	return nil
}

// SyncRules reconciles the BPF map with newRules without restarting.
func (fw *XDPFirewall) SyncRules(newRules []Rule) error {
	for key, action := range fw.rules {
		if err := fw.DeleteRule(ruleFromKeyHash(key, action)); err != nil {
			return fmt.Errorf("delete old rule: %w", err)
		}
	}
	for _, r := range newRules {
		if err := fw.AddRule(r); err != nil {
			return fmt.Errorf("sync rule: %w", err)
		}
	}
	return nil
}

// SetDefaultAction configures the default action (allow/drop) when no rule matches.
func (fw *XDPFirewall) SetDefaultAction(action uint32) error {
	key := uint32(0)
	val := xdpFirewallIfaceConfig{
		DefaultAction: action,
	}
	return fw.objs.XdpIfaceCfg.Update(key, val, ebpf.UpdateAny)
}*/

// GetStats returns aggregated packet statistics.
func (fw *XDPFirewall) GetStats() (*PktStats, error) {
	return getPercpuStats(fw.objs.XdpStats, 0)
}

// Close detaches the XDP program and releases resources.
func (fw *XDPFirewall) Close() error {
	if fw.link != nil {
		fw.link.Close()
	}
	return fw.objs.Close()
}

// NewTCFirewall loads and attaches the TC firewall to the given interface
// (both ingress and egress).
func NewTCFirewall(ifaceName string, tenantName ...string) (*TCFirewall, error) {
	tcLoadMu.Lock()
	defer tcLoadMu.Unlock()

	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("lookup netlink %q: %w", ifaceName, err)
	}

	fmt.Printf("Attaching TC firewall to interface %q (index %d)\n", ifaceName, link.Attrs().Index)

	var objs tcFirewallObjects

	const pinRoot = "/sys/fs/bpf/setera/tc"
	if err := ensureBPFFSMounted("/sys/fs/bpf"); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(pinRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pin directory: %w", err)
	}

	sharedPodIDs, err := getOrCreateSharedTcPodIDsMap()
	if err != nil {
		return nil, err
	}

	opts := &ebpf.CollectionOptions{
		Maps: ebpf.MapOptions{
			PinPath: pinRoot,
		},
		MapReplacements: map[string]*ebpf.Map{
			"tc_podIDs": sharedPodIDs,
		},
	}

	//fmt.Printf("Loading TC firewall BPF objects (pinning under %s)\n", opts.Maps.PinPath)

	tenant := ""
	if len(tenantName) > 0 {
		tenant = tenantName[0]
	}

	if err := loadTcFirewallObjectsWithTenant(&objs, opts, tenant); err != nil {
		fmt.Printf("loadTcFirewallObjects failed (pinPath=%s): %v\n", opts.Maps.PinPath, err)
		return nil, fmt.Errorf("load TC objects: %w", err)
	}
	if objs.TcPodIDs != nil {
		fmt.Printf("TC router loaded with tc_podIDs map\n")
	}

	// Add clsact qdisc (required for TC BPF)
	qdisc := &netlink.GenericQdisc{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(0xffff, 0),
			Parent:    netlink.HANDLE_CLSACT,
		},
		QdiscType: "clsact",
	}
	if err := netlink.QdiscAdd(qdisc); err != nil {
		// Ignore "already exists"
		if err.Error() != "file exists" {
			objs.Close()
			return nil, fmt.Errorf("add clsact qdisc: %w", err)
		}
	}

	// Attach ingress filter
	ingressFilter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.HANDLE_MIN_INGRESS,
			Handle:    1,
			Protocol:  0x0003, // ETH_P_ALL
			Priority:  1,
		},
		Fd:           objs.TcFirewallIngress.FD(),
		Name:         "setera_tc_ingress",
		DirectAction: true,
	}
	if err := netlink.FilterReplace(ingressFilter); err != nil {
		objs.Close()
		return nil, fmt.Errorf("attach TC ingress: %w", err)
	}

	// Attach egress filter
	egressFilter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.HANDLE_MIN_EGRESS,
			Handle:    1,
			Protocol:  0x0003,
			Priority:  1,
		},
		Fd:           objs.TcFirewallEgress.FD(),
		Name:         "setera_tc_egress",
		DirectAction: true,
	}
	if err := netlink.FilterReplace(egressFilter); err != nil {
		objs.Close()
		return nil, fmt.Errorf("attach TC egress: %w", err)
	}

	return &TCFirewall{
		objs:      objs,
		ingressFD: objs.TcFirewallIngress.FD(),
		egressFD:  objs.TcFirewallEgress.FD(),
		iface:     ifaceName,
		qdisc:     qdisc,
		rules:     make(map[ruleKeyHash]uint32),
	}, nil
}

// NewNodeRouter loads and attaches the node router to the given interface
// (both ingress and egress).
func NewNodeRouter(ifaceName string, tenantName ...string) (*NodeRouter, error) {
	tcLoadMu.Lock()
	defer tcLoadMu.Unlock()

	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("lookup netlink %q: %w", ifaceName, err)
	}

	fmt.Printf("Attaching node router to interface %q (index %d)\n", ifaceName, link.Attrs().Index)

	var objs nodeRouterObjects

	const pinRoot = "/sys/fs/bpf/setera/tc"
	if err := ensureBPFFSMounted("/sys/fs/bpf"); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(pinRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pin directory: %w", err)
	}

	sharedPodIDs, err := getOrCreateSharedTcPodIDsMap()
	if err != nil {
		return nil, err
	}

	opts := &ebpf.CollectionOptions{
		Maps: ebpf.MapOptions{
			PinPath: pinRoot,
		},
		MapReplacements: map[string]*ebpf.Map{
			"tc_podIDs": sharedPodIDs,
		},
	}

	tenant := "default"
	if len(tenantName) > 0 {
		tenant = tenantName[0]
	}

	if err := loadNodeRouterObjectsWithTenant(&objs, opts, tenant); err != nil {
		fmt.Printf("loadNodeRouterObjects failed (pinPath=%s): %v\n", opts.Maps.PinPath, err)
		return nil, fmt.Errorf("load node router objects: %w", err)
	}
	if objs.TcPodIDs != nil {
		fmt.Printf("node router loaded with tc_podIDs map\n")
	}

	qdisc := &netlink.GenericQdisc{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(0xffff, 0),
			Parent:    netlink.HANDLE_CLSACT,
		},
		QdiscType: "clsact",
	}
	if err := netlink.QdiscAdd(qdisc); err != nil {
		if err.Error() != "file exists" {
			objs.Close()
			return nil, fmt.Errorf("add clsact qdisc: %w", err)
		}
	}

	ingressFilter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.HANDLE_MIN_INGRESS,
			Handle:    1,
			Protocol:  0x0003,
			Priority:  1,
		},
		Fd:           objs.TcNodeIngress.FD(),
		Name:         "setera_node_ingress",
		DirectAction: true,
	}
	if err := netlink.FilterReplace(ingressFilter); err != nil {
		objs.Close()
		return nil, fmt.Errorf("attach node ingress: %w", err)
	}

	egressFilter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.HANDLE_MIN_EGRESS,
			Handle:    1,
			Protocol:  0x0003,
			Priority:  1,
		},
		Fd:           objs.TcNodeEgress.FD(),
		Name:         "setera_node_egress",
		DirectAction: true,
	}
	if err := netlink.FilterReplace(egressFilter); err != nil {
		objs.Close()
		return nil, fmt.Errorf("attach node egress: %w", err)
	}

	return &NodeRouter{
		objs:      objs,
		ingressFD: objs.TcNodeIngress.FD(),
		egressFD:  objs.TcNodeEgress.FD(),
		iface:     ifaceName,
		qdisc:     qdisc,
		rules:     make(map[ruleKeyHash]uint32),
	}, nil
}

func ensureBPFFSMounted(path string) error {
	if path == "" {
		return fmt.Errorf("bpffs path is empty")
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("create bpffs mount path %s: %w", path, err)
	}

	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return fmt.Errorf("statfs %s: %w", path, err)
	}
	if st.Type == unix.BPF_FS_MAGIC {
		return nil
	}

	if err := unix.Mount("bpf", path, "bpf", 0, ""); err != nil {
		if errors.Is(err, unix.EBUSY) {
			if err := unix.Statfs(path, &st); err == nil && st.Type == unix.BPF_FS_MAGIC {
				return nil
			}
		}
		return fmt.Errorf("%s is not bpffs (fstype=%#x) and mount failed: %w; ensure bpffs is mounted on the node or grant CAP_SYS_ADMIN", path, st.Type, err)
	}

	if err := unix.Statfs(path, &st); err != nil {
		return fmt.Errorf("statfs after mount %s: %w", path, err)
	}
	if st.Type != unix.BPF_FS_MAGIC {
		return fmt.Errorf("%s is mounted with unexpected fstype=%#x (expected bpffs)", path, st.Type)
	}
	return nil
}

// AddInterfaceTenantBinding stores a mapping from ifindex -> tenant name in
// the tc_iface_tenant BPF map. The tenant string is written as a
// null-terminated, zero-padded 64-byte value to match the C definition.
func (fw *TCFirewall) AddInterfaceTenantBinding(ifindex int, tenant string) error {
	_ = ifindex
	_ = tenant
	// tc_router relies on tenant+ifindex lookups in tc_podIDs instead of
	// interface->tenant map bindings, so this is intentionally a no-op.
	return nil
}

// RemoveInterfaceTenantBinding deletes the ifindex -> tenant mapping.
func (fw *TCFirewall) RemoveInterfaceTenantBinding(ifindex int) error {
	_ = ifindex
	// tc_router relies on tc_podIDs entries for routing/isolation decisions.
	return nil
}

// UpsertPodTenantVeth writes a pod destination mapping into tc_podIDs.
// Key: pod IPv4 destination address; value: tenant + host veth ifindex.
func WritePodTenantVeth(ip net.IP, tenant string, ifindex int) error {
	if ip == nil {
		return fmt.Errorf("pod ip is nil")
	}
	if tenant == "" {
		return fmt.Errorf("tenant is empty")
	}
	m, err := openTcPodIDsMap()
	if err != nil {
		return err
	}
	if m == nil {
		return nil
	}
	defer m.Close()

	key := ipToU32(ip)
	if key == 0 {
		return fmt.Errorf("invalid IPv4 pod ip %v", ip)
	}
	if len(tenant) > 63 {
		tenant = tenant[:63]
	}
	val := tcPodIDValue{}
	copy(val.Tenant[:], tenant)
	if ifindex <= 0 {
		val.VethIfindex = ^uint32(0)
	} else {
		val.VethIfindex = uint32(ifindex)
	}
	return m.Update(key, val, ebpf.UpdateAny)
}

// DeletePodTenantVeth removes a pod destination mapping from tc_podIDs.
func DeletePodTenantVeth(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("pod ip is nil")
	}
	m, err := openTcPodIDsMap()
	if err != nil {
		return err
	}
	if m == nil {
		return nil
	}
	defer m.Close()

	key := ipToU32(ip)
	if key == 0 {
		return fmt.Errorf("invalid IPv4 pod ip %v", ip)
	}
	if err := m.Delete(key); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		return err
	}
	return nil
}

func openTcPodIDsMap() (*ebpf.Map, error) {
	m, err := ebpf.LoadPinnedMap(tcPodIDsMapPath, nil)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return getOrCreateSharedTcPodIDsMap()
		}
		return nil, err
	}
	return m, nil
}

/* AddRule inserts a firewall rule into the TC BPF map.
func (fw *TCFirewall) AddRule(r Rule) error {
	key := fwRuleKey(r)
	val := tcFirewallFwRuleVal{
		Action: r.Action,
	}
	switch {
	case isSrcCIDRRule(r):
		if err := fw.objs.TcFwRulesCIDR_src.Update(srcCIDRKey(r), val, ebpf.UpdateAny); err != nil {
			return err
		}
	case isDstCIDRRule(r):
		if err := fw.objs.TcFwRulesCIDR_dst.Update(dstCIDRKey(r), val, ebpf.UpdateAny); err != nil {
			return err
		}
	default:
		if err := fw.objs.TcFwRules.Update(key, val, ebpf.UpdateAny); err != nil {
			return err
		}
	}
	fw.rules[key] = r.Action
	return nil
}

// DeleteRule removes a firewall rule from the TC BPF map.
func (fw *TCFirewall) DeleteRule(r Rule) error {
	key := fwRuleKey(r)
	switch {
	case isSrcCIDRRule(r):
		if err := fw.objs.TcFwRulesCIDR_src.Delete(srcCIDRKey(r)); err != nil {
			return err
		}
	case isDstCIDRRule(r):
		if err := fw.objs.TcFwRulesCIDR_dst.Delete(dstCIDRKey(r)); err != nil {
			return err
		}
	default:
		if err := fw.objs.TcFwRules.Delete(key); err != nil {
			return err
		}
	}
	delete(fw.rules, key)
	return nil
}

// SyncRules reconciles the BPF map with newRules without restarting.
func (fw *TCFirewall) SyncRules(newRules []Rule) error {
	for key, action := range fw.rules {
		if err := fw.DeleteRule(ruleFromKeyHash(key, action)); err != nil {
			return fmt.Errorf("delete old rule: %w", err)
		}
	}
	for _, r := range newRules {
		if err := fw.AddRule(r); err != nil {
			return fmt.Errorf("sync rule: %w", err)
		}
	}
	return nil
}*/

// SetDefaultAction sets the default policy (allow/drop).
func (fw *TCFirewall) SetDefaultAction(action uint32, conntrackEnabled bool) error {
	key := uint32(0)
	flags := uint32(0)
	if conntrackEnabled {
		flags |= tcFlagConntrack
	}
	val := tcFirewallIfaceConfig{
		DefaultAction: action,
		Flags:         flags,
	}
	return fw.objs.TcIfaceCfg.Update(key, val, ebpf.UpdateAny)
}

// SetInterfaceActionByIndex sets a direct action for packets targeting ifindex.
func (fw *TCFirewall) SetInterfaceActionByIndex(ifindex int, action uint32) error {
	// Per-interface direct actions are currently not supported in the TC
	// firewall. This method is kept for API compatibility but is a no-op.
	if ifindex <= 0 {
		return fmt.Errorf("invalid ifindex %d", ifindex)
	}
	return nil
}

// SetInterfaceAction resolves ifaceName and sets a direct action for it.
func (fw *TCFirewall) SetInterfaceAction(ifaceName string, action uint32) error {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("lookup interface %q: %w", ifaceName, err)
	}
	return fw.SetInterfaceActionByIndex(iface.Index, action)
}

// DeleteInterfaceActionByIndex removes ifindex -> action mapping.
func (fw *TCFirewall) DeleteInterfaceActionByIndex(ifindex int) error {
	// Per-interface direct actions are currently not supported; keep the
	// method for API compatibility but make it a no-op.
	if ifindex <= 0 {
		return fmt.Errorf("invalid ifindex %d", ifindex)
	}
	return nil
}

// GetIngressStats returns aggregated ingress packet statistics.
func (fw *TCFirewall) GetIngressStats() (*PktStats, error) {
	return getPercpuStats(fw.objs.TcStats, 0)
}

// GetEgressStats returns aggregated egress packet statistics.
func (fw *TCFirewall) GetEgressStats() (*PktStats, error) {
	return getPercpuStats(fw.objs.TcStats, 1)
}

// Close detaches TC programs and cleans up.
func (fw *TCFirewall) Close() error {
	if fw.qdisc != nil {
		netlink.QdiscDel(fw.qdisc)
	}
	return fw.objs.Close()
}

// Close detaches node router programs and cleans up.
func (fw *NodeRouter) Close() error {
	if fw.qdisc != nil {
		netlink.QdiscDel(fw.qdisc)
	}
	return fw.objs.Close()
}

// ---- Shared helpers ----

// PktStats aggregates per-CPU packet counters.
type PktStats struct {
	RxPackets uint64
	RxBytes   uint64
	TxPackets uint64
	TxBytes   uint64
	Dropped   uint64
}

func fwRuleKey(r Rule) ruleKeyHash {
	srcIP := ipToU32(r.SrcIP)
	dstIP := ipToU32(r.DstIP)
	srcPrefix := r.SrcPrefix
	dstPrefix := r.DstPrefix
	if srcPrefix == 0 && srcIP != 0 {
		srcPrefix = 32
	}
	if dstPrefix == 0 && dstIP != 0 {
		dstPrefix = 32
	}

	return ruleKeyHash{
		SrcIp:     srcIP,
		DstIp:     dstIP,
		SrcPort:   r.SrcPort,
		DstPort:   r.DstPort,
		Protocol:  r.Protocol,
		SrcPrefix: srcPrefix,
		DstPrefix: dstPrefix,
	}
}

func ipToU32(ip net.IP) uint32 {
	if ip == nil {
		return 0
	}
	ip = ip.To4()
	if ip == nil {
		return 0
	}
	// Use NativeEndian so the uint32 serializes to the same raw bytes
	// that the BPF program reads from iph->saddr / iph->daddr.
	return binary.NativeEndian.Uint32(ip)
}

func u32ToIP(v uint32) net.IP {
	buf := make([]byte, 4)
	binary.NativeEndian.PutUint32(buf, v)
	return net.IPv4(buf[0], buf[1], buf[2], buf[3])
}

func ruleFromKeyHash(key ruleKeyHash, action uint32) Rule {
	r := Rule{
		SrcPort:   key.SrcPort,
		DstPort:   key.DstPort,
		Protocol:  key.Protocol,
		SrcPrefix: key.SrcPrefix,
		DstPrefix: key.DstPrefix,
		Action:    action,
	}
	if key.SrcIp != 0 {
		r.SrcIP = u32ToIP(key.SrcIp)
	}
	if key.DstIp != 0 {
		r.DstIP = u32ToIP(key.DstIp)
	}
	return r
}

func getPercpuStats(m *ebpf.Map, key uint32) (*PktStats, error) {
	var values []xdpFirewallPktStats
	if err := m.Lookup(key, &values); err != nil {
		return nil, fmt.Errorf("lookup stats: %w", err)
	}

	agg := &PktStats{}
	for _, v := range values {
		agg.RxPackets += v.RxPackets
		agg.RxBytes += v.RxBytes
		agg.TxPackets += v.TxPackets
		agg.TxBytes += v.TxBytes
		agg.Dropped += v.Dropped
	}
	return agg, nil
}
