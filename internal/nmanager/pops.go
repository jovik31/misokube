package nmanager

import (
	"context"
	"fmt"
	"log"
	"net"

	"github/setera/internal/router"

	"github/setera/pkg/network/backend"
	"github/setera/pkg/network/device"
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
	ci, err := rec.IPAM.Allocate(args.ContainerID, args.IfName, args.NetNS, args.PodName)
	if err != nil {
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
	// TODO: reconfigure routes/ips if needed during expansion/migration.
	return nil
}
