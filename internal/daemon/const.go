package daemon

import (
	op "github/setera/pkg/operator"
)

// event sources for daemon-side operators
const (
	SourceTenantCRD      op.Source = "k8s:tenant"
	SourceNodeStoreCRD   op.Source = "k8s:nodestore"
	SourceNetworkManager op.Source = "nm:network-manager"
)

// k8s informer events
const (
	EventAdd    op.Event = "add"
	EventUpdate op.Event = "update"
	EventDelete op.Event = "delete"
)

// finalizers
const (
	nodestoreFinalizer string = "setera.com/nodestore-finalizer"
)

const (
	TenantLabelKey string = "setera.com.tenant"
)
