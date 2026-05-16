package daemon

import (
	"context"
	"net"

	"github/setera/internal/nmanager"
	seterav1 "github/setera/pkg/api/setera.com/v1"
	op "github/setera/pkg/operator"
)

// reconcile from tenant source trigger - add event
func (o *Operator) reconcileTenantSourceAdd(ctx context.Context, _ op.Source, ref op.ResourceRef) error {

	o.logger.Info("Tenant source add event received; no action taken")
	if o.dp != nil {
		o.dp.EnsureTenant(ref.Namespace, ref.Name)
	} else {
		o.logger.Info("No dispatcher configured; skipping EnsureTenant")
	}
	return nil
}

// ----- these reconcile funcs handle Tenant CRD events -----
func (o *Operator) reconcileTenantSourceUpdate(ctx context.Context, _ op.Source, ref op.ResourceRef) error {

	// check if local node is now assigned to tenant
	tenant, err := o.tenantLister.Tenants(ref.Namespace).Get(ref.Name)
	if err != nil || tenant == nil {
		return err
	}
	nodeName := o.nodeName
	if nodeName == "" {
		o.logger.Info("Local node name not configured; skipping tenant update reconcile")
		return nil
	}

	// if local node is already assigned, no action needed
	if o.tenantAssignedContains(tenant, nodeName) {
		return nil
	}

	// if not assigned, but now in awaiting set, ensure assignment via dispatcher
	if o.tenantAwaitingContains(tenant, nodeName) {
		o.logger.WithValues("tenant", ref.Name, "node", nodeName).Info("local node now awaiting configuration; ensuring assignment")
		if o.dp != nil {
			o.dp.EnsureTenant(ref.Namespace, ref.Name)
		} else {
			o.logger.Info("No dispatcher configured; skipping EnsureTenant")
		}
		return nil
	}

	// its a tenant update that does not involve this node - add default routes to all the private tenants in the remote node's infra
	o.logger.WithValues("tenant", ref.Name, "node", nodeName).Info("local node not assigned or awaiting; reconciling default routes")

	// enqueue a DefaultRoutes reconciliation on dispatcher

	// create list of remote tenant infra for assigned nodes

	for _, assignedNode := range tenant.Status.AssignedNodes {
		if assignedNode.Name == nodeName {
			continue
		}

		_, pCIDR, err := net.ParseCIDR(assignedNode.TenantCIDR)
		if err != nil {
			o.logger.WithValues("tenant", ref.Name, "node", assignedNode.Name).Info("failed to parse tenant CIDR; skipping remote", "cidr", assignedNode.TenantCIDR, "err", err)
			continue
		}
		remote := nmanager.RemoteTenantInfra{
			NodeName: assignedNode.Name,
			VTEPIP:   net.ParseIP(assignedNode.VtepIP),
			VTEPMAC:  net.HardwareAddr(assignedNode.VtepMAC),
			Subnet:   pCIDR,
		}
		o.dp.EnsureDefaultProxy(ref.Name, remote)

	}

	return nil
}

func (o *Operator) reconcileTenantDelete(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	// forward deletion to dispatcher (idempotent)
	if o.dp != nil {
		o.dp.RemoveTenant(ref.Namespace, ref.Name)
	}
	return nil
}
func (o *Operator) tenantAwaitingContains(t *seterav1.Tenant, node string) bool {
	if t == nil {
		return false
	}
	for _, n := range t.Status.AwaitingNodeConfiguration {
		if n == node {
			return true
		}
	}
	return false
}

func (o *Operator) tenantAssignedContains(t *seterav1.Tenant, node string) bool {
	if t == nil {
		return false
	}
	for _, n := range t.Status.AssignedNodes {
		if n.Name == node {
			return true
		}
	}
	return false
}
