package dispatcher

import (
	"github/setera/internal/nmanager"
	"time"
)

type Op string

const (
	OpEnsure     Op = "ensure"
	OpRemove     Op = "remove"
	OpEnsurePeer Op = "ensure-peer"
	OpRemovePeer Op = "remove-peer"
)

type Command struct {
	TenantID  string
	Op        Op
	OpID      string
	Remote    nmanager.RemoteTenantInfra
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
	nmt   nmanager.TenantOps
	nmn   nmanager.NodestoreOps
	inbox chan Command
	sink  chan Event
}

type Option func(*Dispatcher)
