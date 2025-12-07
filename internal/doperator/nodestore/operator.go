package nodestore

import (
	"context"
	"log"
)

// Operator owns the local NodeStore CR: writes local tenant infra state and
// reacts to remote NodeStore updates to perform peer wiring via Network Manager.
type Operator struct {
	nodeName string
}

func NewOperator(nodeName string) *Operator {
	return &Operator{nodeName: nodeName}
}

// UpdateLocal writes a minimal snapshot of local tenant infra to the NodeStore status.
// TODO: implement using pkg/k8s helpers and generated clients.
func (op *Operator) UpdateLocal(ctx context.Context, tenantID string) error {
	log.Printf("nodestore operator: update local status for tenant %s on node %s", tenantID, op.nodeName)
	return nil
}

// OnRemoteUpdate reacts to a remote node's NodeStore changes and performs peer wiring.
// TODO: implement ARP/FDB/route wiring through Network Manager NodestoreOps.
func (op *Operator) OnRemoteUpdate(ctx context.Context, tenantID string, remoteNode string) error {
	log.Printf("nodestore operator: remote update for tenant %s from node %s", tenantID, remoteNode)
	return nil
}
