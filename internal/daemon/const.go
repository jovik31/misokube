package daemon

import (
    op "github/setera/pkg/operator"
)

// event sources for daemon-side operators
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
