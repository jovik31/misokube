package dispatcher

import (
	"github/setera/internal/ebpfmanager"
	"github/setera/internal/nmanager"
	"time"
)

type Op string

const (
	OpEnsure        Op = "ensure"
	OpRemove        Op = "remove"
	OpEnsurePeer    Op = "ensure-peer"
	OpRemovePeer    Op = "remove-peer"
	OpEnsurePodProg Op = "ensure-pod-prog"
	OpRemovePodProg Op = "remove-pod-prog"
	OpEnsureMap     Op = "ensure-map"
	OpRemoveMap     Op = "remove-map"
	OpUpsertPodMap  Op = "upsert-pod-map"
	OpDeletePodMap  Op = "delete-pod-map"
)

type Command struct {
	TenantID  string
	Op        Op
	OpID      string
	Remote    nmanager.RemoteTenantInfra
	PodName   string
	IfName    string
	PodIP     string
	Ifindex   int
	timestamp time.Time
}

type Event struct {
	TenantID  string
	Op        Op
	OpID      string
	err       error
	timestamp time.Time
}

type Dispatcher struct {
	nmt    nmanager.TenantOps
	nmn    nmanager.NodestoreOps
	ebpfmt ebpfmanager.TenantOps
	ebpfmp ebpfmanager.PodOps
	inbox  chan Command
	sink   chan Event
}

type Option func(*Dispatcher)
