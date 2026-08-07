package daemon

import (
	"context"

	op "github/setera/pkg/operator"
)

// ----- these reconcile funcs handle Tenant CRD events -----
func (o *Operator) reconcileTenantAddUpdate(ctx context.Context, _ op.Source, ref op.ResourceRef) error {

	// Recheck current authoritative state when the queued event is processed.
	// This prevents an old add/update event from recreating a Tenant after a
	// delete request has set deletionTimestamp.
	tenant, err := o.tenantLister.Tenants(ref.Namespace).Get(ref.Name)
	if err != nil || tenant == nil || tenant.DeletionTimestamp != nil {
		if o.dp != nil {
			o.dp.RemoveMap(ref.Name)
			o.dp.RemoveTenant(ref.Namespace, ref.Name)
		}
		return nil
	}
	// call the dispatcher to handle nm changes
	o.logger.Info("Ensuring tenant")
	if o.dp != nil {
		o.dp.EnsureTenant(ref.Namespace, ref.Name)
		o.dp.EnsureMap(ref.Name)
	} else {
		o.logger.Info("No dispatcher configured; skipping EnsureTenant")
	}
	return nil
}

func (o *Operator) reconcileTenantDelete(ctx context.Context, _ op.Source, ref op.ResourceRef) error {
	// forward deletion to dispatcher (idempotent)
	if o.dp != nil {
		o.dp.RemoveMap(ref.Name)
		o.dp.RemoveTenant(ref.Namespace, ref.Name)
	}
	return nil
}
