package nmanager

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github/setera/internal/router"

	"github/setera/pkg/network/device"
	"github/setera/pkg/network/ipam"
	op "github/setera/pkg/operator"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

var _ PodOps = (*NetworkManagerImpl)(nil)

// Pod lifecycle operations exposed to CNI path via tenant actors.
func (nm *NetworkManagerImpl) EnsurePod(ctx context.Context, tenantID string, args router.PodAttachArgs) (net.IPNet, net.IP, string, error) {
	start := time.Now()
	// Minimal implementation: allocate an IP for this endpoint via IPAM.
	// Full veth/bridge/routes will be added in subsequent steps.
	nm.mu.RLock()
	rec, ok := nm.TenantRecords[tenantID]
	nm.mu.RUnlock()
	if !ok || rec == nil {
		return net.IPNet{}, nil, "", ErrTenantActorNotFound
	}
	if rec.State == TenantStateClosing {
		return net.IPNet{}, nil, "", ErrTenantClosing
	}

	if rec.IPAM == nil {
		return net.IPNet{}, nil, "", fmt.Errorf("ipam not initialized for tenant %s", tenantID)
	}
	// Use full CNI args for IPAM allocation.
	key := podKey(args.Namespace, args.PodName)
	allocStart := time.Now()
	ci, err := rec.IPAM.Allocate(key, args.ContainerID, args.IfName, args.NetNS)
	if err != nil {
		log.Printf("tenant=%s pod=%s ipam allocate failed: %v", tenantID, args.PodName, err)
		return net.IPNet{}, nil, "", err
	}
	log.Printf("tenant=%s pod=%s ipam allocate ok dur=%s", tenantID, args.PodName, time.Since(allocStart))

	gateway := podHostGateway(ci.IP)
	podIPNet := &net.IPNet{
		IP:   ci.IP,
		Mask: net.CIDRMask(32, 32),
	}

	// Attach pod veth directly to host namespace (no bridge master).
	vethStart := time.Now()
	hostVethName, err := device.SetupVethDirect(args.NetNS, 1500, args.IfName, podIPNet, gateway)
	if err != nil {
		// On failure, release IP
		log.Print("failed to setup veth", err)
		_ = rec.IPAM.Free(ci.IP)
		return net.IPNet{}, nil, "", fmt.Errorf("setup veth: %w", err)
	}
	log.Printf("tenant=%s pod=%s setup veth ok dur=%s", tenantID, args.PodName, time.Since(vethStart))

	// Store the created host veth name in IPAM allocation for later use by eBPF attachment
	if setErr := rec.IPAM.SetHostVethName(key, hostVethName); setErr != nil {
		log.Printf("WARNING: failed to set host veth name in IPAM for pod %s: %v", key, setErr)
	}

	if err := recIfindexForPod(ci, args); err != nil {
		log.Printf("WARNING: tenant=%s pod=%s host veth ifindex lookup failed: %v; marking pod as remote (ifindex=-1)", tenantID, args.PodName, err)
		// Leave Ifindex as -1 (already set in IPAM allocation);
		// this pod will be routed via kernel routing instead of local peer redirect.
	}

	log.Printf("pod ensured: tenant=%s pod=%s ip=%s total_dur=%s", tenantID, args.PodName, ci.IP.String(), time.Since(start))

	nm.emitNodeStoreEvent(op.EventUpdate)

	return *podIPNet, gateway, args.IfName, nil
}

func recIfindexForPod(ci *ipam.ContainerNetInfo, args router.PodAttachArgs) error {
	if ci == nil {
		return fmt.Errorf("nil allocation")
	}
	hostIfName := ci.HostVethName
	if hostIfName == "" {
		return fmt.Errorf("missing host veth name for pod %s", ci.IP.String())
	}

	hostLink, err := netlink.LinkByName(hostIfName)
	if err != nil {
		return fmt.Errorf("lookup host veth %s: %w", hostIfName, err)
	}
	hostAttrs := hostLink.Attrs()
	if hostAttrs == nil {
		return fmt.Errorf("host veth attrs missing for %s", hostIfName)
	}
	if hostAttrs.Index <= 0 {
		return fmt.Errorf("invalid host veth ifindex %d for %s", hostAttrs.Index, hostIfName)
	}
	log.Printf("DEBUG recIfindexForPod: host veth %s resolves to ifindex=%d for pod IP=%s", hostIfName, hostAttrs.Index, ci.IP.String())
	ci.Ifindex = hostAttrs.Index
	return nil
}

func (nm *NetworkManagerImpl) RemovePod(ctx context.Context, tenantID string, args router.PodAttachArgs) error {
	nm.mu.RLock()
	rec, ok := nm.TenantRecords[tenantID]
	nm.mu.RUnlock()
	if !ok || rec == nil {
		// CNI DEL is idempotent. The Tenant may already have been removed.
		return nil
	}
	if rec.IPAM == nil {
		return nil
	}

	key := podKey(args.Namespace, args.PodName)
	allocation, found := rec.IPAM.GetAllocation(key)
	if !found || allocation == nil {
		return nil
	}

	// Ignore a delayed DEL from an older sandbox after Kubernetes recreated
	// the same namespace/name with a different container ID.
	if args.ContainerID != "" && allocation.ID != "" && allocation.ID != args.ContainerID {
		log.Printf(
			"tenant=%s pod=%s ignoring stale DEL container=%s current=%s",
			tenantID,
			key,
			args.ContainerID,
			allocation.ID,
		)
		return nil
	}

	if err := rec.IPAM.Free(allocation.IP); err != nil {
		return fmt.Errorf("release pod IP tenant=%s pod=%s ip=%s: %w", tenantID, key, allocation.IP, err)
	}
	log.Printf("pod removed: tenant=%s pod=%s ip=%s", tenantID, key, allocation.IP)

	// NodeStore mirroring removes the pod from tc_podIDs and detaches the
	// local pod program by diffing the new snapshot against the old one.
	nm.emitNodeStoreEvent(op.EventUpdate)
	return nil
}

// UpdatePod applies changes required during tenant expansion or migration.
func (nm *NetworkManagerImpl) UpdatePod(ctx context.Context, tenantID string, args router.PodAttachArgs) error {
	nm.mu.RLock()
	rec, ok := nm.TenantRecords[tenantID]
	nm.mu.RUnlock()
	if !ok || rec == nil {
		return ErrTenantActorNotFound
	}
	if rec.IPAM == nil {
		return fmt.Errorf("ipam not initialized for tenant %s", tenantID)
	}
	key := podKey(args.Namespace, args.PodName)
	ci, ok := rec.IPAM.GetAllocation(key)
	if !ok || ci == nil {
		return fmt.Errorf("ipam allocation not found for pod %s", key)
	}
	podIPNet := &net.IPNet{
		IP:   ci.IP,
		Mask: net.CIDRMask(32, 32),
	}
	gateway := podHostGateway(ci.IP)

	nsHandle, err := ns.GetNS(ci.NetNS)
	if err != nil {
		return fmt.Errorf("open netns %s: %w", ci.NetNS, err)
	}
	defer nsHandle.Close()

	return nsHandle.Do(func(_ ns.NetNS) error {
		log.Printf("tenant=%s pod=%s update begin", tenantID, args.PodName)
		link, err := netlink.LinkByName(ci.IFname)
		if err != nil {
			return fmt.Errorf("lookup pod iface %s: %w", ci.IFname, err)
		}
		if err := flushLinkIPv4Addrs(link); err != nil {
			return fmt.Errorf("flush pod iface addresses: %w", err)
		}
		if err := netlink.LinkSetMTU(link, 1500); err != nil {
			return fmt.Errorf("set mtu: %w", err)
		}
		addr := &netlink.Addr{IPNet: podIPNet}
		if err := netlink.AddrReplace(link, addr); err != nil {
			return fmt.Errorf("replace addr %s: %w", podIPNet, err)
		}
		if err := netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("link up: %w", err)
		}
		if err := replaceDefaultRoute(link, gateway); err != nil {
			return err
		}
		log.Printf("tenant=%s pod=%s update done ip=%s mask=%s", tenantID, args.PodName, podIPNet.IP, podIPNet.Mask.String())
		return nil
	})
}

func podKey(namespace, pod string) string {
	if namespace == "" {
		return pod
	}
	return namespace + "/" + pod
}

func splitPodKey(key string) (namespace, pod string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", key
}

func flushLinkIPv4Addrs(link netlink.Link) error {
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return err
	}
	for _, addr := range addrs {
		addrCopy := addr
		if err := netlink.AddrDel(link, &addrCopy); err != nil && !isNetlinkNotFound(err) {
			return err
		}
	}
	return nil
}

func replaceDefaultRoute(link netlink.Link, gw net.IP) error {
	routes, err := netlink.RouteList(link, netlink.FAMILY_V4)
	if err == nil {
		for _, r := range routes {
			if r.Dst == nil {
				routeCopy := r
				if err := netlink.RouteDel(&routeCopy); err != nil && !isNetlinkNotFound(err) {
					return fmt.Errorf("delete default route: %w", err)
				}
			}
		}
	}
	gwHost := &net.IPNet{IP: gw.To4(), Mask: net.CIDRMask(32, 32)}
	gwRt := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: gwHost, Scope: netlink.SCOPE_LINK}
	if err := netlink.RouteReplace(gwRt); err != nil {
		return fmt.Errorf("route replace on-link gateway %s: %w", gw, err)
	}
	rt := &netlink.Route{LinkIndex: link.Attrs().Index, Gw: gw}
	if err := netlink.RouteReplace(rt); err != nil {
		return fmt.Errorf("route replace via %s: %w", gw, err)
	}
	return nil
}

func isNetlinkNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") ||
		strings.Contains(s, "no such process") ||
		strings.Contains(s, "cannot assign requested address")
}

func podHostGateway(podIP net.IP) net.IP {
	b := podIP.To4()
	if b == nil {
		return net.IPv4(169, 254, 0, 1)
	}
	return net.IPv4(169, 254, b[2], b[3])
}
