package daemon

import (
	"context"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	"github/setera/pkg/k8s"
	op "github/setera/pkg/operator"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ----- these reconcile funcs handle Tenant CRD events -----
func (o *Operator) reconcileTenantAddUpdate(ctx context.Context, _ op.Source, ref op.ResourceRef) error {

	// call the dispatcher to handle nm changes
	o.logger.Info("Ensuring tenant")
	if o.dp != nil {
		o.dp.EnsureTenant(ref.Namespace, ref.Name)
	} else {
		o.logger.Info("No dispatcher configured; skipping EnsureTenant")
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

// ----- these reconcile funcs handle NodeStore CRD events -----
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

	// used to handle remote nodestore updates:
	// we need to ensure peers, for that we do the following:
	// 1. check what tenants are assigned to the remote nodestore
	// 2. If the tenant is also assigned to the local nodestore, send an ensurePeer request to the dispatcher
	return nil
}

func (o *Operator) reconcileNodeStoreDelete(ctx context.Context, _ op.Source, res op.ResourceRef) error {

	// on deletion - only cleans up local nodestore
	// call removeTenants for all tenants assigned to this nodestore
	// remove finalizer
	// delete handled by k8s garbage collection
	return nil
}

// ----- these reconcile funcs handle Network Manager events related to Tenant node assignments -----
// ----- update the local nodestore with network manager changes -----
func (o *Operator) reconcileNodestoreTenantAdd(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	// Nothing to do on addition; handled in update.
	return nil
}

func (o *Operator) reconcileNodestoreTenantUpdate(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	// Mirror NM snapshot (all tenants) into local NodeStore status.
	if o.nmOps == nil || o.setera == nil || o.nodeName == "" {
		o.logger.WithValues("node", o.nodeName).Info("NM ops or client not configured; skipping NodeStore mirror")
	}
	ns, err := o.setera.SeteraV1().NodeStores(metav1.NamespaceNone).Get(ctx, o.nodeName, metav1.GetOptions{})
	if err != nil || ns == nil {
		o.logger.WithValues("node", o.nodeName).Info("failed to fetch local NodeStore; skipping", "err", err)
		return nil
	}
	snaps, err := o.nmOps.SnapshotAllTenantInfra()
	if err != nil {
		o.logger.WithValues("node", o.nodeName).Info("snapshot all tenants failed", "err", err)
		return nil
	}
	if len(snaps) == 0 {
		o.logger.WithValues("node", o.nodeName).Info("no tenant snapshots; clearing NodeStore tenants")
	}
	newTenants := make(map[string]seterav1.TenantInfra, len(snaps))
	for tenant, snap := range snaps {
		o.logger.WithValues("node", o.nodeName, "tenant", tenant, "pods", len(snap.Pods)).Info("mirroring tenant snapshot")
		ti := seterav1.TenantInfra{Name: tenant}
		if snap.Subnet != nil {
			ti.TenantCIDR = snap.Subnet.String()
		}

		ti.VNI = int(snap.VNI)
		ti.VTEP_NAME = snap.VTEPDev
		if snap.VTEPIP != nil {
			ti.VTEP_IP = snap.VTEPIP.String()
		}
		if snap.VTEPMAC != nil {
			ti.VTEP_MAC = snap.VTEPMAC.String()
		}
		ti.BRIDGE_NAME = snap.Bridge
		if snap.BridgeIP != nil {
			ti.BRIDGE_IP = snap.BridgeIP.String()
		}
		if snap.BridgeMAC != nil {
			ti.BRIDGE_MAC = snap.BridgeMAC.String()
		}

		ti.Pods = make([]seterav1.Pod_Info, 0, len(snap.Pods))
		for _, pod := range snap.Pods {
			info := seterav1.Pod_Info{Name: pod.Name}
			if pod.IP != nil {
				info.IP = pod.IP.String()
			}
			ti.Pods = append(ti.Pods, info)
		}
		newTenants[tenant] = ti
	}

	for tenant := range newTenants {
		err := k8s.StoreTenantLabel(o.kubeclient, TenantLabelKey, o.nodeName, tenant)
		if err != nil {
			o.logger.WithValues("node", o.nodeName).Info("failed to store tenant label on node", "tenant", tenant, "err", err)
		}

	}

	// copy nodestore
	mod := ns.DeepCopy()
	mod.Status.Tenants = newTenants

	if _, err := o.setera.SeteraV1().NodeStores(metav1.NamespaceNone).UpdateStatus(ctx, mod, metav1.UpdateOptions{}); err != nil {
		o.logger.WithValues("node", o.nodeName).Info("failed to update NodeStore status", "err", err)
		return err
	} else {
		o.logger.WithValues("node", o.nodeName).Info("updated NodeStore tenants from NM snapshot", "count", len(newTenants))
	}
	return nil
}

func (o *Operator) reconcileNodestoreTenantDelete(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	// On delete, overwrite status from full snapshot; removed tenants won't be present.
	if o.nmOps == nil || o.setera == nil || o.nodeName == "" {
		o.logger.WithValues("node", o.nodeName).Info("NM ops or client not configured; skipping NodeStore delete mirror")
		return nil
	}
	ns, err := o.setera.SeteraV1().NodeStores(metav1.NamespaceNone).Get(ctx, o.nodeName, metav1.GetOptions{})
	if err != nil || ns == nil {
		o.logger.WithValues("node", o.nodeName).Info("failed to fetch local NodeStore; skipping delete", "err", err)
		return nil
	}
	snaps, err := o.nmOps.SnapshotAllTenantInfra()
	if err != nil {
		o.logger.WithValues("node", o.nodeName).Info("snapshot all tenants failed on delete", "err", err)
		return nil
	}
	newTenants := make(map[string]seterav1.TenantInfra, len(snaps))
	for tenant, snap := range snaps {
		ti := seterav1.TenantInfra{Name: tenant}
		if snap.Subnet != nil {
			ti.TenantCIDR = snap.Subnet.String()
		}
		ti.VNI = int(snap.VNI)
		ti.VTEP_NAME = snap.VTEPDev
		if snap.VTEPIP != nil {
			ti.VTEP_IP = snap.VTEPIP.String()
		}
		if snap.VTEPMAC != nil {
			ti.VTEP_MAC = snap.VTEPMAC.String()
		}
		ti.BRIDGE_NAME = snap.Bridge
		if snap.BridgeIP != nil {
			ti.BRIDGE_IP = snap.BridgeIP.String()
		}
		if snap.BridgeMAC != nil {
			ti.BRIDGE_MAC = snap.BridgeMAC.String()
		}
		ti.Pods = make([]seterav1.Pod_Info, 0, len(snap.Pods))
		for _, pod := range snap.Pods {
			info := seterav1.Pod_Info{Name: pod.Name}
			if pod.IP != nil {
				info.IP = pod.IP.String()
			}
			ti.Pods = append(ti.Pods, info)
		}
		newTenants[tenant] = ti
	}

	o.logger.Info("This is the node name :", o.nodeName)

	ns.Status.Tenants = newTenants
	if _, err := o.setera.SeteraV1().NodeStores(metav1.NamespaceNone).UpdateStatus(ctx, ns, metav1.UpdateOptions{}); err != nil {
		o.logger.WithValues("node", o.nodeName).Info("failed to update NodeStore status on delete", "err", err)
	} else {
		o.logger.WithValues("node", o.nodeName).Info("updated NodeStore tenants after delete", "count", len(newTenants))
	}
	return nil

}
