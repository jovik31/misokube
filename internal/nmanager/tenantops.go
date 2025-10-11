package nmanager

import (
	"context"
	"fmt"

	"github/setera/pkg/network/backend"
	"github/setera/pkg/network/ipam"
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

	// Fast path: already exists
	nm.mu.RLock()
	if _, ok := nm.TenantRecords[tenantID]; ok {
		nm.mu.RUnlock()
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
	}
	nm.mu.Unlock()
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

	// Best effort: delete backend devices first
	if rec.Backend != nil {
		_ = rec.Backend.Delete()
	}

	// Deallocate subnet
	_ = nm.Subnet.Deallocate(tenantID)

	// Cleanup iptables
	if nm.IPTables != nil {
		_ = nm.IPTables.DeleteTenantChains(tenantID)
	}

	// Drop record
	nm.mu.Lock()
	delete(nm.TenantRecords, tenantID)
	nm.mu.Unlock()
	return nil
}
