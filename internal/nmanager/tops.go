package nmanager

import (
	"context"
	"fmt"
	"log"

	"github/setera/pkg/network/backend"
	"github/setera/pkg/network/ipam"
	op "github/setera/pkg/operator"
)

var _ TenantOps = (*NetworkManagerImpl)(nil)

// Exposed contracts for tenant lifecycle management. -> Called by the Tenant Operator
// EnsureTenant is idempotent: if the tenant already exists, it returns nil.
// It allocates a subnet via the configured SubnetManager, optionally initializes
// a backend via BackendFactory, and creates a per-tenant IPAM via IPAMFactory
// (or falls back to ConfigIPAM). On any failure, it rolls back allocated state.
func (nm *NetworkManagerImpl) EnsureTenant(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		return fmt.Errorf("ensure tenant: empty tenantID")
	}
	log.Printf("nm: EnsureTenant called for tenant=%s", tenantID)

	// Fast path: already exists
	nm.mu.RLock()
	if _, ok := nm.TenantRecords[tenantID]; ok {
		nm.mu.RUnlock()
		log.Printf("nm: tenant %s already ensured", tenantID)
		return nil
	}
	nm.mu.RUnlock()

	// Allocate subnet first
	subnetNet, err := nm.Subnet.Allocate(tenantID)
	if err != nil {
		return fmt.Errorf("allocate subnet for %s: %w", tenantID, err)
	}

	// Backend – use package constructor
	be := backend.NewBridgeVTEPBackend()
	if be == nil {
		_ = nm.Subnet.Deallocate(tenantID)
		return fmt.Errorf("new backend returned nil")
	}
	if err := be.Create(tenantID, subnetNet, nm.NodeName); err != nil {
		_ = nm.Subnet.Deallocate(tenantID)
		return fmt.Errorf("backend create: %w", err)
	}

	// Tenant policy setup
	if nm.TP != nil {
		brName := ""
		if brDev, ok := backend.Bridge(be); ok && brDev != nil {
			brName = brDev.GetName()
		}
		vxName := ""
		if vxDev, ok := backend.VTEP(be); ok && vxDev != nil {
			vxName = vxDev.GetName()
		}
		if tenantID == "default" {
			if err := nm.TP.EnsureDefaultTenant(brName, vxName); err != nil {
				_ = be.Delete()
				_ = nm.Subnet.Deallocate(tenantID)
				return fmt.Errorf("ensure default tenant policy: %w", err)
			}
		} else {
			extraIfaces := nm.defaultTenantIfaces()
			if err := nm.TP.EnsureTenantIsolation(tenantID, brName, vxName, extraIfaces...); err != nil {
				_ = be.Delete()
				_ = nm.Subnet.Deallocate(tenantID)
				return fmt.Errorf("ensure tenant isolation: %w", err)
			}
		}
	}

	// IPAM – use package constructor
	ipm, err := ipam.NewBitmapIPAM(subnetNet)
	if err != nil {
		_ = be.Delete()
		_ = nm.Subnet.Deallocate(tenantID)
		return fmt.Errorf("ipam create: %w", err)
	}

	// Commit record (double-check for races)
	nm.mu.Lock()
	if _, exists := nm.TenantRecords[tenantID]; exists {
		nm.mu.Unlock()
		_ = be.Delete()
		_ = nm.Subnet.Deallocate(tenantID)
		return nil
	}
	nm.TenantRecords[tenantID] = &TenantRecord{
		Subnet:  subnetNet,
		Backend: be,
		IPAM:    ipm,
		State:   TenantStateReady,
	}
	nm.mu.Unlock()

	// Start per-tenant actor and register it
	nm.mu.Lock()
	if nm.TenantActors == nil {
		nm.TenantActors = make(map[string]TenantActor)
	}
	if _, ok := nm.TenantActors[tenantID]; !ok {
		act := StartTenantActor(ctx, nm, tenantID, 128)
		nm.TenantActors[tenantID] = act
		log.Printf("nm: tenant actor registered tenant=%s", tenantID)
	}
	nm.mu.Unlock()

	// Emit update event to local operator (non-blocking) for the local NodeStore
	nm.emitNodeStoreEvent(op.EventUpdate)
	return nil
}

// defaultTenantIfaces returns the bridge/vxlan interface names for the default tenant if present.
// Used to permit private tenants to reach the default tenant network.
func (nm *NetworkManagerImpl) defaultTenantIfaces() []string {
	nm.mu.RLock()
	rec, ok := nm.TenantRecords["default"]
	nm.mu.RUnlock()
	if !ok || rec == nil || rec.Backend == nil {
		return nil
	}
	var ifaces []string
	if brDev, ok := backend.Bridge(rec.Backend); ok && brDev != nil {
		ifaces = append(ifaces, brDev.GetName())
	}
	if vxDev, ok := backend.VTEP(rec.Backend); ok && vxDev != nil {
		ifaces = append(ifaces, vxDev.GetName())
	}
	return ifaces
}

func (nm *NetworkManagerImpl) RemoveTenant(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		return fmt.Errorf("remove tenant: empty tenantID")
	}

	// Fetch record
	nm.mu.RLock()
	rec, ok := nm.TenantRecords[tenantID]
	nm.mu.RUnlock()
	if !ok {
		// idempotent
		return nil
	}

	// Mark as closing to gate pod operations while teardown proceeds
	nm.mu.Lock()
	if rec2, ok := nm.TenantRecords[tenantID]; ok && rec2 != nil {
		rec2.State = TenantStateClosing
	}
	nm.mu.Unlock()

	// Best effort: delete backend devices first
	var brName, vxName string
	if rec.Backend != nil {
		if brDev, ok := backend.Bridge(rec.Backend); ok && brDev != nil {
			brName = brDev.GetName()
		}
		if vxDev, ok := backend.VTEP(rec.Backend); ok && vxDev != nil {
			vxName = vxDev.GetName()
		}
		_ = rec.Backend.Delete()
	}

	// Deallocate subnet
	_ = nm.Subnet.Deallocate(tenantID)

	// Cleanup iptables
	if nm.TP != nil {
		_ = nm.TP.DeleteTenantChains(tenantID, brName, vxName)
	}

	// Stop and drop actor (best-effort)
	nm.mu.Lock()
	actor, ok := nm.TenantActors[tenantID]
	nm.mu.Unlock()
	if ok && actor != nil {
		_ = actor.Stop(ctx)
		nm.mu.Lock()
		delete(nm.TenantActors, tenantID)
		nm.mu.Unlock()
	}
	// Drop record
	nm.mu.Lock()
	delete(nm.TenantRecords, tenantID)
	nm.mu.Unlock()

	// Emit delete event to local operator for the local NodeStore
	nm.emitNodeStoreEvent(op.EventDelete)
	return nil
}

func (nm *NetworkManagerImpl) EnsureDefaultTenantProxy(ctx context.Context, tenantID string, remote RemoteTenantInfra) error {

	// check if the remote infra exists and is equal to stored
	if localTenantRoutes, ok := nm.DefaultRoutes[tenantID][remote.NodeName]; !ok {

		if remotePeersEqual(localTenantRoutes, remote) {
			// already exists; no-op
			return nil
		}
	}

	// configure routes on local default tenant

	return nil
}

func (nm *NetworkManagerImpl) RemoveDefaultTenantProxy(ctx context.Context, tenantID string, remote RemoteTenantInfra) error {

	return nil

}
