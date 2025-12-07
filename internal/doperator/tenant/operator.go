package tenant

import (
	"context"
	"log"

	"github/setera/internal/dispatcher"
)

// Operator watches Tenant CRs on the daemon and enqueues ensure/remove ops
// to the Network Manager via the dispatcher when the local node is assigned.
type Operator struct {
	nodeName string
	dispatch *dispatcher.Dispatcher
}

func NewOperator(nodeName string, d *dispatcher.Dispatcher) *Operator {
	return &Operator{nodeName: nodeName, dispatch: d}
}

// OnTenantAssigned is called when a Tenant CR includes this node.
func (op *Operator) OnTenantAssigned(ctx context.Context, tenantID string) {
	if op.dispatch == nil {
		log.Printf("tenant operator: dispatcher not set; skipping ensure for %s", tenantID)
		return
	}
	op.dispatch.Enqueue(dispatcher.Command{TenantID: tenantID, Op: dispatcher.OpEnsure})
}

// OnTenantRemoved is called when a Tenant CR removes this node.
func (op *Operator) OnTenantRemoved(ctx context.Context, tenantID string) {
	if op.dispatch == nil {
		log.Printf("tenant operator: dispatcher not set; skipping remove for %s", tenantID)
		return
	}
	op.dispatch.Enqueue(dispatcher.Command{TenantID: tenantID, Op: dispatcher.OpRemove})
}
