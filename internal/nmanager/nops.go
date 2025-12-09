package nmanager

import (
	"context"
	"fmt"
	"github/setera/pkg/network/arp"
	"github/setera/pkg/network/backend"
	"github/setera/pkg/network/fdb"
	"github/setera/pkg/network/route"
)

var _ NodestoreOps = (*NetworkManagerImpl)(nil)

func (nm *NetworkManagerImpl) SnapshotTenantInfra(tenantId string) (TenantInfraSnapshot, error) {
	rec := nm.TenantRecords[tenantId]
	if rec == nil || rec.Backend == nil || rec.Subnet == nil {
		return TenantInfraSnapshot{}, fmt.Errorf("snapshot: unknown tenant %s", tenantId)
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
		if vd, ok := vtepDev.(interface{ GetVNI() int }); ok {
			snap.VNI = uint32(vd.GetVNI())
		}
	}
	if brDev, ok := backend.Bridge(rec.Backend); ok && brDev != nil {
		snap.Bridge = brDev.GetName()
	}
	return snap, nil
}

/*
	EnsurePeer ensures that a remote tenant infrastructure is peered with the local tenant infrastructure.

involves: ARP, FDB, and route entries on the local VTEP towards the remote node.
*/
func (nm *NetworkManagerImpl) EnsurePeer(ctx context.Context, tenantID string, remote RemoteTenantInfra) error {

	ltr := nm.TenantRecords[tenantID]
	if ltr == nil || ltr.Backend == nil {
		return fmt.Errorf("ensure peer: unknown tenant %s", tenantID)
	}

	/*ARP
	- local vtep
	- remote vtep ip
	- remote vtep mac
	*/
	lvtep, ok := backend.VTEP(ltr.Backend)
	if !ok {
		return fmt.Errorf("ensure peer: tenant %s backend is not VTEP", tenantID)
	}
	if remote.VTEPIP == nil || remote.VTEPMAC == nil {
		return fmt.Errorf("ensure peer: remote VTEP is nil")
	}

	ae := arp.ARPEntry{
		Device: lvtep.GetName(),
		IP:     remote.VTEPIP,
		MAC:    remote.VTEPMAC,
	}

	if err := nm.ARP.Add(ae); err != nil {
		return fmt.Errorf("ensure peer: add arp entry: %w", err)
	}

	/*FDB
	- local vtep
	- remote node IP
	- remote vtep mac
	*/

	fe := fdb.FDBEntry{
		Device: lvtep.GetName(),
		Mac:    remote.VTEPMAC,
		IP:     remote.NodeIP,
	}

	if err := nm.FDB.Add(fe); err != nil {
		return fmt.Errorf("ensure peer: add fdb entry: %w", err)
	}

	/*ROUTE
	- local vtep
	- remote tenant subnet
	- remote vtep ip (next hop)

	*/
	re := &route.Route{
		Device:  lvtep.GetName(),
		Dst:     remote.Subnet,
		Gateway: remote.VTEPIP,
	}

	if err := nm.Route.Ensure(re); err != nil {
		return fmt.Errorf("ensure peer: add route entry: %w", err)
	}
	return nil
}

/* RemovePeer removes ARP, FDB, and route entries towards a specific remote node. */
func (nm *NetworkManagerImpl) RemovePeer(ctx context.Context, tenantID string, remote RemoteTenantInfra) error {

	ltr := nm.TenantRecords[tenantID]
	if ltr == nil || ltr.Backend == nil {
		return fmt.Errorf("remove peer: unknown tenant %s", tenantID)
	}

	lvtep, ok := backend.VTEP(ltr.Backend)
	if !ok {
		return fmt.Errorf("remove peer: tenant %s backend is not VTEP", tenantID)
	}
	if remote.VTEPIP == nil || remote.VTEPMAC == nil {
		return fmt.Errorf("remove peer: remote VTEP is nil")
	}

	ae := arp.ARPEntry{
		Device: lvtep.GetName(),
		IP:     remote.VTEPIP,
		MAC:    remote.VTEPMAC,
	}

	if err := nm.ARP.Delete(ae); err != nil {
		return fmt.Errorf("remove peer: delete arp entry: %w", err)
	}

	fe := fdb.FDBEntry{
		Device: lvtep.GetName(),
		Mac:    remote.VTEPMAC,
		IP:     remote.NodeIP,
	}

	if err := nm.FDB.Delete(fe); err != nil {
		return fmt.Errorf("remove peer: delete fdb entry: %w", err)
	}

	re := &route.Route{
		Device:  lvtep.GetName(),
		Dst:     remote.Subnet,
		Gateway: remote.VTEPIP,
	}

	if err := nm.Route.Delete(re); err != nil {
		return fmt.Errorf("remove peer: delete route entry: %w", err)
	}

	return nil
}

/* FlushTenant flushes ARP/FDB/routes for the tenant on local devices (used on teardown). */
func (nm *NetworkManagerImpl) FlushTenant(ctx context.Context, tenantID string) error {

	// fetch tenant record
	ltr := nm.TenantRecords[tenantID]
	if ltr == nil || ltr.Backend == nil {
		return fmt.Errorf("flush tenant: unknown tenant %s", tenantID)
	}

	lvtep, ok := backend.VTEP(ltr.Backend)
	if !ok {
		return fmt.Errorf("flush tenant: tenant %s backend is not VTEP", tenantID)
	}
	if lvtep == nil {
		return fmt.Errorf("flush tenant: local VTEP is nil")
	}

	// delete all ARP entries on the local VTEP device

	// delete all FDB entries on the local VTEP device

	// delete all routes on the local VTEP device

	return nil
}
// SnapshotAllTenantInfra returns snapshots for all local tenants keyed by tenantID.
func (nm *NetworkManagerImpl) SnapshotAllTenantInfra() (map[string]TenantInfraSnapshot, error) {
    out := make(map[string]TenantInfraSnapshot)
    nm.mu.RLock()
    defer nm.mu.RUnlock()
    for tid, rec := range nm.TenantRecords {
        if rec == nil || rec.Backend == nil || rec.Subnet == nil {
            continue
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
            if vd, ok := vtepDev.(interface{ GetVNI() int }); ok {
                snap.VNI = uint32(vd.GetVNI())
            }
        }
        if brDev, ok := backend.Bridge(rec.Backend); ok && brDev != nil {
            snap.Bridge = brDev.GetName()
        }
        out[tid] = snap
    }
    return out, nil
}
