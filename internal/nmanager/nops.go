package nmanager

import (
	"bytes"
	"context"
	"fmt"
	"net"

	"github/setera/pkg/network/arp"
	"github/setera/pkg/network/backend"
	"github/setera/pkg/network/device"
	"github/setera/pkg/network/fdb"
	"github/setera/pkg/network/route"
)

var _ NodestoreOps = (*NetworkManagerImpl)(nil)

func (nm *NetworkManagerImpl) SubnetFreeCount() int {
	if nm.Subnet == nil {
		return 0
	}
	return nm.Subnet.FreeCount()
}

func (nm *NetworkManagerImpl) SubnetTotalCount() int {
	if nm.Subnet == nil {
		return 0
	}
	return nm.Subnet.TotalCount()
}

func (nm *NetworkManagerImpl) SnapshotTenantInfra(tenantId string) (TenantInfraSnapshot, error) {
	nm.mu.RLock()
	rec := nm.TenantRecords[tenantId]
	nm.mu.RUnlock()
	if rec == nil {
		return TenantInfraSnapshot{}, fmt.Errorf("snapshot: unknown tenant %s", tenantId)
	}
	return buildTenantSnapshot(rec)
}

/*
	EnsurePeer ensures that a remote tenant infrastructure is peered with the local tenant infrastructure.

involves: ARP, FDB, and route entries on the local VTEP towards the remote node.
*/
func (nm *NetworkManagerImpl) EnsurePeer(ctx context.Context, tenantID string, remote RemoteTenantInfra) error {

	if remote.NodeName == "" {
		return fmt.Errorf("ensure peer: remote node name empty")
	}
	ltr, err := nm.getTenantRecord(tenantID)
	if err != nil {
		return fmt.Errorf("ensure peer: %w", err)
	}
	if err := nm.requireManagers(); err != nil {
		return fmt.Errorf("ensure peer: %w", err)
	}

	prev, hadPrev, skip := nm.lookupPeerState(tenantID, remote.NodeName, remote)
	if skip {
		return nil
	}

	if hadPrev {
		if err := nm.removePeerEntries(ltr, prev); err != nil {
			return fmt.Errorf("ensure peer: cleanup previous peer: %w", err)
		}
		nm.deletePeerState(tenantID, prev.NodeName)
	}

	if err := nm.ensurePeerEntries(ltr, remote); err != nil {
		return err
	}

	nm.storePeerState(tenantID, remote)
	return nil
}

/* RemovePeer removes ARP, FDB, and route entries towards a specific remote node. */
func (nm *NetworkManagerImpl) RemovePeer(ctx context.Context, tenantID string, remote RemoteTenantInfra) error {

	if remote.NodeName == "" {
		return fmt.Errorf("remove peer: remote node name empty")
	}

	ltr, err := nm.getTenantRecord(tenantID)
	if err != nil {
		// tenant already gone locally
		return nil
	}
	if err := nm.requireManagers(); err != nil {
		return fmt.Errorf("remove peer: %w", err)
	}

	stored, ok := nm.getStoredPeer(tenantID, remote.NodeName)
	if !ok {
		return nil
	}

	if err := nm.removePeerEntries(ltr, stored); err != nil {
		return err
	}
	nm.deletePeerState(tenantID, stored.NodeName)
	return nil
}

/* FlushTenant flushes ARP/FDB/routes for the tenant on local devices (used on teardown). */
func (nm *NetworkManagerImpl) FlushTenant(ctx context.Context, tenantID string) error {

	ltr, err := nm.getTenantRecord(tenantID)
	if err != nil {
		return nil
	}
	if err := nm.requireManagers(); err != nil {
		return fmt.Errorf("flush tenant: %w", err)
	}

	nm.mu.Lock()
	peers := make([]RemoteTenantInfra, 0, len(ltr.Peers))
	for _, peer := range ltr.Peers {
		peers = append(peers, peer)
	}
	ltr.Peers = make(map[string]RemoteTenantInfra)
	nm.mu.Unlock()

	for _, peer := range peers {
		if err := nm.removePeerEntries(ltr, peer); err != nil {
			return err
		}
	}

	return nil
}

// SnapshotAllTenantInfra returns snapshots for all local tenants keyed by tenantID.
func (nm *NetworkManagerImpl) SnapshotAllTenantInfra() (map[string]TenantInfraSnapshot, error) {
	out := make(map[string]TenantInfraSnapshot)
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	for tid, rec := range nm.TenantRecords {
		snap, err := buildTenantSnapshot(rec)
		if err != nil {
			continue
		}
		out[tid] = snap
	}
	return out, nil
}

func buildTenantSnapshot(rec *TenantRecord) (TenantInfraSnapshot, error) {
	if rec == nil || rec.Backend == nil || rec.Subnet == nil {
		return TenantInfraSnapshot{}, fmt.Errorf("snapshot: tenant not initialized")
	}

	snap := TenantInfraSnapshot{
		Subnet: rec.Subnet,
		MTU:    1500,
	}

	if vtepDev, ok := backend.VTEP(rec.Backend); ok && vtepDev != nil {
		snap.VTEPDev = vtepDev.GetName()
		if ip := vtepDev.GetIP(); ip != nil {
			snap.VTEPIP = ip.IP
		}
		snap.VTEPMAC = vtepDev.GetMAC()

		if vt, ok := vtepDev.(device.VTEPDevice); ok {
			snap.VNI = uint32(vt.GetVNI())
		}
	}

	if brDev, ok := backend.Bridge(rec.Backend); ok && brDev != nil {
		snap.Bridge = brDev.GetName()
		if ip := brDev.GetIP(); ip != nil {
			snap.BridgeIP = ip.IP
		}
		snap.BridgeMAC = brDev.GetMAC()
	}

	if rec.IPAM != nil {
		allocs := rec.IPAM.ListAllocations()
		snap.Pods = make([]TenantPodInfo, 0, len(allocs))
		for key, info := range allocs {
			if info == nil || info.IP == nil {
				continue
			}
			ns, name := splitPodKey(key)
			if ns != "" {
				name = ns + "/" + name
			}
			snap.Pods = append(snap.Pods, TenantPodInfo{
				Name: name,
				IP:   info.IP,
			})
		}
	}

	return snap, nil
}

func (nm *NetworkManagerImpl) ensurePeerEntries(ltr *TenantRecord, remote RemoteTenantInfra) error {
	lvtep, ok := backend.VTEP(ltr.Backend)
	if !ok {
		return fmt.Errorf("tenant backend is not VTEP")
	}
	if remote.VTEPIP == nil || remote.VTEPMAC == nil || remote.Subnet == nil {
		return fmt.Errorf("remote VTEP info missing")
	}

	ae := arp.ARPEntry{
		Device: lvtep.GetName(),
		IP:     remote.VTEPIP,
		MAC:    remote.VTEPMAC,
	}
	if err := nm.ARP.Add(ae); err != nil {
		return fmt.Errorf("add arp entry: %w", err)
	}

	fe := fdb.FDBEntry{
		Device: lvtep.GetName(),
		Mac:    remote.VTEPMAC,
		IP:     remote.NodeIP,
	}
	if err := nm.FDB.Add(fe); err != nil {
		return fmt.Errorf("add fdb entry: %w", err)
	}

	re := &route.Route{
		Device:  lvtep.GetName(),
		Dst:     remote.Subnet,
		Gateway: remote.VTEPIP,
	}
	if err := nm.Route.Update(re); err != nil {
		return fmt.Errorf("add route entry: %w", err)
	}
	return nil
}

func (nm *NetworkManagerImpl) removePeerEntries(ltr *TenantRecord, remote RemoteTenantInfra) error {
	lvtep, ok := backend.VTEP(ltr.Backend)
	if !ok {
		return fmt.Errorf("tenant backend is not VTEP")
	}
	if remote.VTEPIP == nil || remote.VTEPMAC == nil || remote.Subnet == nil {
		return fmt.Errorf("remote VTEP info missing")
	}

	ae := arp.ARPEntry{
		Device: lvtep.GetName(),
		IP:     remote.VTEPIP,
		MAC:    remote.VTEPMAC,
	}
	if err := nm.ARP.Delete(ae); err != nil {
		return fmt.Errorf("delete arp entry: %w", err)
	}

	fe := fdb.FDBEntry{
		Device: lvtep.GetName(),
		Mac:    remote.VTEPMAC,
		IP:     remote.NodeIP,
	}
	if err := nm.FDB.Delete(fe); err != nil {
		return fmt.Errorf("delete fdb entry: %w", err)
	}

	re := &route.Route{
		Device:  lvtep.GetName(),
		Dst:     remote.Subnet,
		Gateway: remote.VTEPIP,
	}
	if err := nm.Route.Delete(re); err != nil {
		return fmt.Errorf("delete route entry: %w", err)
	}
	return nil
}

func (nm *NetworkManagerImpl) getTenantRecord(tenantID string) (*TenantRecord, error) {
	nm.mu.RLock()
	rec := nm.TenantRecords[tenantID]
	nm.mu.RUnlock()
	if rec == nil || rec.Backend == nil {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}
	return rec, nil
}

func (nm *NetworkManagerImpl) requireManagers() error {
	if nm.ARP == nil || nm.FDB == nil || nm.Route == nil {
		return fmt.Errorf("managers not initialized")
	}
	return nil
}

func (nm *NetworkManagerImpl) lookupPeerState(tenantID, remoteNode string, desired RemoteTenantInfra) (RemoteTenantInfra, bool, bool) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	rec := nm.TenantRecords[tenantID]
	if rec == nil {
		return RemoteTenantInfra{}, false, false
	}
	if rec.Peers == nil {
		rec.Peers = make(map[string]RemoteTenantInfra)
	}
	prev, ok := rec.Peers[remoteNode]
	if ok && remotePeersEqual(prev, desired) {
		return prev, ok, true
	}
	return prev, ok, false
}

func (nm *NetworkManagerImpl) storePeerState(tenantID string, remote RemoteTenantInfra) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	rec := nm.TenantRecords[tenantID]
	if rec == nil {
		return
	}
	if rec.Peers == nil {
		rec.Peers = make(map[string]RemoteTenantInfra)
	}
	rec.Peers[remote.NodeName] = remote
}

func (nm *NetworkManagerImpl) deletePeerState(tenantID, remoteNode string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	rec := nm.TenantRecords[tenantID]
	if rec == nil || rec.Peers == nil {
		return
	}
	delete(rec.Peers, remoteNode)
}

func (nm *NetworkManagerImpl) getStoredPeer(tenantID, remoteNode string) (RemoteTenantInfra, bool) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	rec := nm.TenantRecords[tenantID]
	if rec == nil || rec.Peers == nil {
		return RemoteTenantInfra{}, false
	}
	peer, ok := rec.Peers[remoteNode]
	return peer, ok
}

func remotePeersEqual(a, b RemoteTenantInfra) bool {
	if a.NodeName != b.NodeName || a.VNI != b.VNI {
		return false
	}
	if !cidrEqual(a.Subnet, b.Subnet) {
		return false
	}
	if !ipEqual(a.NodeIP, b.NodeIP) || !ipEqual(a.VTEPIP, b.VTEPIP) {
		return false
	}
	if !macEqual(a.VTEPMAC, b.VTEPMAC) {
		return false
	}
	return true
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
