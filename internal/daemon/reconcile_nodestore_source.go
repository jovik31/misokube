package daemon

import (
	"context"
	"fmt"
	"net"

	nmanager "github/setera/internal/nmanager"
	seterav1 "github/setera/pkg/api/setera.com/v1"
	op "github/setera/pkg/operator"
)

func (o *Operator) reconcileNodeStoreAdd(ctx context.Context, _ op.Source, ref op.ResourceRef) error {

	nstore, err := o.nodeStoreLister.NodeStores(ref.Namespace).Get(ref.Name)
	if err != nil {
		return err
	}

	//ensure finalizer
	if err := o.ensureNodestoreFinalizer(ctx, nstore); err != nil {
		return err
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

	if ns.Spec.Name == o.nodeName {
		return nil
	}

	localSnaps, err := o.nmOps.SnapshotAllTenantInfra()
	if err != nil {
		o.logger.Info("failed to snapshot local tenants for peer sync", "err", err)
		return err
	}
	if len(localSnaps) == 0 {
		o.logger.Info("peer sync deferred: local tenant snapshot empty")
		return fmt.Errorf("local tenant snapshot empty")
	}

	if len(ns.Status.Tenants) == 0 {
		o.logger.WithValues("remoteNode", ns.Spec.Name).Info("peer sync deferred: remote NodeStore has no tenant status yet")
		return fmt.Errorf("remote nodestore %s has empty tenant status", ns.Spec.Name)
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

func (o *Operator) reconcileNodeStoreDelete(ctx context.Context, _ op.Source, res op.ResourceRef) error {

	// on deletion - only cleans up local nodestore
	// call removeTenants for all tenants assigned to this nodestore
	// remove finalizer
	// delete handled by k8s garbage collection
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
