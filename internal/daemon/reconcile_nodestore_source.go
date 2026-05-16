package daemon

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"

	nmanager "github/setera/internal/nmanager"
	seterav1 "github/setera/pkg/api/setera.com/v1"
	op "github/setera/pkg/operator"

	"k8s.io/apimachinery/pkg/labels"
)

type podMapEntry struct {
	tenant  string
	podName string
	podIP   string
	ifindex int
	ifName  string
}

func (o *Operator) reconcileNodeStoreAdd(ctx context.Context, _ op.Source, ref op.ResourceRef) error {

	nstore, err := o.nodeStoreLister.NodeStores(ref.Namespace).Get(ref.Name)
	if err != nil {
		return err
	}

	//ensure finalizer
	if err := o.ensureNodestoreFinalizer(ctx, nstore); err != nil {
		return err
	}

	// Keep tc_podIDs in sync on add events too. If we wait for an update
	// event only, one node can miss remote pod destinations and drop return
	// traffic in tc_router when dst lookup misses.
	if err := o.syncPodMapForNode(nstore); err != nil {
		o.logger.WithValues("nodestore", nstore.Name).Info("failed to sync pod tc map on add", "err", err)
	}
	return nil
}

func (o *Operator) reconcileNodeStoreUpdate(ctx context.Context, _ op.Source, res op.ResourceRef) error {

	if o.nmOps == nil {
		o.logger.Info("nmOps not configured; skipping nodestore update reconcile")
		return nil
	}

	ns, err := o.nodeStoreLister.NodeStores(res.Namespace).Get(res.Name)
	if err != nil || ns == nil {
		return err
	}

	if err := o.syncPodMapForNode(ns); err != nil {
		o.logger.WithValues("nodestore", ns.Name).Info("failed to sync pod tc map", "err", err)
	}

	if ns.Spec.Name == o.nodeName {
		if o.dp != nil {
			for tenantID := range ns.Status.Tenants {
				o.dp.EnsureTenant(ns.Namespace, tenantID)
				o.dp.EnsureMap(tenantID)
			}
		}
		return nil
	}

	localSnaps, err := o.nmOps.SnapshotAllTenantInfra()
	if err != nil {
		o.logger.Info("failed to snapshot local tenants for peer sync", "err", err)
		return err
	}
	if len(localSnaps) == 0 {
		o.logger.Info("peer sync deferred: local tenant snapshot empty")
		return nil
	}

	if len(ns.Status.Tenants) == 0 {
		o.logger.WithValues("remoteNode", ns.Spec.Name).Info("peer sync deferred: remote NodeStore has no tenant status yet")
		return nil
	}

	var ensured, removed int
	for tenantID, remoteInfo := range ns.Status.Tenants {
		remotePeer, convErr := buildRemoteTenantInfra(ns, remoteInfo)
		if convErr != nil {
			o.logger.WithValues("tenant", tenantID, "remote", ns.Spec.Name).Info("skip peer update: invalid remote snapshot", "err", convErr)
			return convErr
		}
		if _, ok := localSnaps[tenantID]; ok {
			o.ensurePeer(ctx, tenantID, remotePeer)
			ensured++
		}
	}

	for tenantID := range localSnaps {
		if _, ok := ns.Status.Tenants[tenantID]; !ok {
			o.removePeer(ctx, tenantID, nmanager.RemoteTenantInfra{NodeName: ns.Spec.Name})
			removed++
		}
	}

	o.logger.WithValues("remoteNode", ns.Spec.Name).
		Info("peer sync complete", "ensured", ensured, "removed", removed, "tenants", len(ns.Status.Tenants))

	return nil
}

func (o *Operator) syncPodMapForNode(ns *seterav1.NodeStore) error {
	if ns == nil {
		return nil
	}

	o.podMapMu.Lock()
	stores, err := o.nodeStoreLister.List(labels.Everything())
	if err == nil {
		// First key is the name of the node and second key is podIP which gives us tenant name and ifindex
		refreshed := make(map[string]map[uint32]podMapEntry, len(stores))
		for _, s := range stores {
			if s == nil {
				continue
			}
			// For each node we collect the pod entries
			refreshed[s.Spec.Name] = collectNodePodEntries(s, o.nodeName)
		}
		o.podMapByNode = refreshed
	}
	o.podMapByNode[ns.Spec.Name] = collectNodePodEntries(ns, o.nodeName)
	desired := mergePodMapEntries(o.podMapByNode)
	current := o.podMapKnown
	o.podMapMu.Unlock()

	for key, entry := range desired {
		if old, ok := current[key]; ok && old == entry {
			continue
		}
		if o.dp == nil {
			return fmt.Errorf("dispatcher not configured for pod map upsert")
		}
		if entry.ifindex >= 0 {
			o.dp.EnsurePodProg(entry.tenant, entry.podName, entry.ifName)
		}
		o.dp.UpsertPodMap(entry.tenant, entry.podName, entry.podIP, entry.ifindex, entry.ifName)
	}
	for key := range current {
		if _, ok := desired[key]; ok {
			continue
		}
		old := current[key]
		if o.dp == nil {
			return fmt.Errorf("dispatcher not configured for pod map delete")
		}
		o.dp.DeletePodMap(old.podIP)
		if old.ifindex >= 0 {
			o.dp.RemovePodProg(old.tenant, old.podName)
		}
	}

	o.podMapMu.Lock()
	o.podMapKnown = desired
	o.podMapMu.Unlock()
	return nil
}

func collectNodePodEntries(store *seterav1.NodeStore, localNode string) map[uint32]podMapEntry {
	out := make(map[uint32]podMapEntry)
	if store == nil {
		return out
	}
	// Checks if it is the local nodestore
	isLocal := store.Spec.Name == localNode

	//loops each tenant and gets their pods
	for tenantID, tenant := range store.Status.Tenants {
		for _, pod := range tenant.Pods {
			ip := net.ParseIP(pod.IP).To4()
			if ip == nil {
				continue
			}
			key := ipv4ToU32(ip)
			if key == 0 {
				continue
			}
			podName := pod.Name
			ifindex := -1
			ifName := pod.HostVethName
			if isLocal {
				if pod.Ifindex > 0 {
					ifindex = pod.Ifindex
				}
				if ifName == "" {
					ifName = pod.HostVethName
				}
			}
			out[key] = podMapEntry{tenant: tenantID, podName: podName, podIP: pod.IP, ifindex: ifindex, ifName: ifName}
		}
	}
	return out
}

func mergePodMapEntries(byNode map[string]map[uint32]podMapEntry) map[uint32]podMapEntry {
	out := make(map[uint32]podMapEntry)
	for _, entries := range byNode {
		for key, entry := range entries {
			existing, ok := out[key]
			if !ok {
				out[key] = entry
				continue
			}
			// Prefer local entries (ifindex >= 0), and prefer ones with a host veth name.
			replace := false
			if existing.ifindex < 0 && entry.ifindex >= 0 {
				replace = true
			}
			if !replace && existing.ifName == "" && entry.ifName != "" {
				replace = true
			}
			if replace {
				out[key] = entry
			}
		}
	}
	return out
}

func ipv4ToU32(ip net.IP) uint32 {
	b := ip.To4()
	if b == nil {
		return 0
	}
	return binary.NativeEndian.Uint32(b)
}

func (o *Operator) reconcileNodeStoreDelete(ctx context.Context, _ op.Source, res op.ResourceRef) error {
	o.podMapMu.Lock()
	delete(o.podMapByNode, res.Name)
	desired := mergePodMapEntries(o.podMapByNode)
	current := o.podMapKnown
	o.podMapMu.Unlock()

	for key := range current {
		if _, ok := desired[key]; ok {
			continue
		}
		old := current[key]
		if o.dp == nil {
			return fmt.Errorf("dispatcher not configured for pod map delete after nodestore delete")
		}
		o.dp.DeletePodMap(old.podIP)
		if old.ifindex >= 0 {
			o.dp.RemovePodProg(old.tenant, old.podName)
		}
	}

	o.podMapMu.Lock()
	o.podMapKnown = desired
	o.podMapMu.Unlock()
	return nil
}

func buildRemoteTenantInfra(ns *seterav1.NodeStore, info seterav1.TenantInfra) (nmanager.RemoteTenantInfra, error) {
	var remote nmanager.RemoteTenantInfra

	if ns == nil {
		return remote, fmt.Errorf("nodestore nil")
	}
	if info.TenantCIDR == "" {
		return remote, fmt.Errorf("tenant %s missing CIDR", info.Name)
	}

	_, subnet, err := net.ParseCIDR(info.TenantCIDR)
	if err != nil {
		return remote, fmt.Errorf("parse tenant cidr: %w", err)
	}

	vtepIP := net.ParseIP(info.VTEP_IP)
	if vtepIP == nil {
		return remote, fmt.Errorf("invalid vtep ip")
	}

	nodeIP := net.ParseIP(ns.Spec.NodeIP)
	if nodeIP == nil {
		return remote, fmt.Errorf("invalid node ip")
	}
	mac, err := net.ParseMAC(info.VTEP_MAC)
	if err != nil {
		return remote, fmt.Errorf("parse vtep mac: %w", err)
	}

	remote = nmanager.RemoteTenantInfra{
		NodeName: ns.Spec.Name,
		NodeIP:   nodeIP,
		VTEPIP:   vtepIP,
		VTEPMAC:  mac,
		Subnet:   subnet,
		VNI:      uint32(info.VNI),
	}
	return remote, nil
}
