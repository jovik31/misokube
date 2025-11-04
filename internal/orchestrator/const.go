package orchestrator

import (
	op "github/setera/pkg/operator"
)

// tenant CRD event sources
const (
	SourceTenantCRD    op.Source = "k8s:tenant"
	SourceNodeStoreCRD op.Source = "k8s:nodestore"
)

// k8s informer events
const (
	EventAdd    op.Event = "add"
	EventUpdate op.Event = "update"
	EventDelete op.Event = "delete"
)

// nodestore events
const (
	EventUpdateNodeStore op.Event = "update"
)

// finalizers
const (
	tenantFinalizer string = "setera.com/tenant-finalizer"
)
