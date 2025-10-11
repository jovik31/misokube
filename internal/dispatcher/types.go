package dispatcher

import (
	"github/setera/internal/nmanager"
	"time"
)

type Op string

const (
	OpEnsure Op = "ensure"
	OpRemove Op = "remove"
)

type Command struct {
	TenantID  string
	Op        Op
	OpID      string
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
	nm    nmanager.TenantOps
	inbox chan Command
	sink  chan Event
}

type Option func(*Dispatcher)
