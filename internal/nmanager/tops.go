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
	if nm.emitter != nil {
		nm.emitter.EnqueueWith("nm:network-manager", "update", op.ResourceRef{Kind: "NodeStore", Namespace: "default", Name: nm.NodeName})
	}
	return nil
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
	if rec.Backend != nil {
		_ = rec.Backend.Delete()
	}

	// Deallocate subnet
	_ = nm.Subnet.Deallocate(tenantID)

	// Cleanup iptables
	if nm.TP != nil {
		_ = nm.TP.DeleteTenantChains(tenantID)
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
	if nm.emitter != nil {
		nm.emitter.EnqueueWith("nm:network-manager", "delete", op.ResourceRef{Kind: "NodeStore", Namespace: "default", Name: nm.NodeName})
	}
	return nil
}
