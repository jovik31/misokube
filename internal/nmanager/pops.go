package nmanager

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"

	"github/setera/internal/router"

	"github/setera/pkg/network/backend"
	"github/setera/pkg/network/device"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

var _ PodOps = (*NetworkManagerImpl)(nil)

// Pod lifecycle operations exposed to CNI path via tenant actors.
func (nm *NetworkManagerImpl) EnsurePod(ctx context.Context, tenantID string, args router.PodAttachArgs) (net.IPNet, net.IP, string, error) {
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
	ci, err := rec.IPAM.Allocate(key, args.ContainerID, args.IfName, args.NetNS)
	if err != nil {
		log.Printf("tenant=%s pod=%s ipam allocate failed: %v", tenantID, args.PodName, err)
		return net.IPNet{}, nil, "", err
	}

	// get tenant bridge name
	br, ok := backend.Bridge(rec.Backend)
	if !ok || br == nil {
		return net.IPNet{}, nil, "", fmt.Errorf("bridge device not found for tenant %s", tenantID)
	}

	podIPNet := &net.IPNet{
		IP:   ci.IP,
		Mask: rec.Subnet.Mask,
	}

	// Attach pod veth to tenant bridge
	err = device.SetupVeth(args.NetNS, br.GetName(), 1500, args.IfName, podIPNet, br.GetIP().IP)
	if err != nil {
		// On failure, release IP
		log.Print("failed to setup veth", err)
		_ = rec.IPAM.Free(ci.IP)
		return net.IPNet{}, nil, "", fmt.Errorf("setup veth: %w", err)
	}

	log.Print("pod ensured: ", tenantID, args.PodName, ci.IP.String())

	return *podIPNet, br.GetIP().IP, args.IfName, nil
}

func (nm *NetworkManagerImpl) RemovePod(ctx context.Context, tenantID string, args router.PodAttachArgs) error {
	nm.mu.RLock()
	rec, ok := nm.TenantRecords[tenantID]
	nm.mu.RUnlock()
	if !ok || rec == nil {
		return ErrTenantActorNotFound
	}
	// TODO: detach veth, release IP via rec.IPAM, cleanup routes via nm.Route.
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
	br, ok := backend.Bridge(rec.Backend)
	if !ok || br == nil {
		return fmt.Errorf("bridge device not found for tenant %s", tenantID)
	}
	podIPNet := &net.IPNet{
		IP:   ci.IP,
		Mask: rec.Subnet.Mask,
	}
	gateway := br.GetIP().IP

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
