package nmanager

import (
	"context"
	"time"
)

type tenantMsgKind int

const (
	msgAllocate tenantMsgKind = iota
	msgRemove
	msgStop
)

type tenantMsg struct {
	kind  tenantMsgKind
	epKey string
	reply chan error
}

// tenantActor implements TenantActor with a per-tenant mailbox and goroutine.
type tenantActor struct {
	tenantID string
	inbox    chan tenantMsg
	nm       *NetworkManagerImpl
}

// StartTenantActor starts the actor loop and returns a TenantActor handle.
func StartTenantActor(ctx context.Context, nm *NetworkManagerImpl, tenantID string, mailboxSize int) TenantActor {
	if mailboxSize <= 0 {
		mailboxSize = 128
	}
	a := &tenantActor{tenantID: tenantID, inbox: make(chan tenantMsg, mailboxSize), nm: nm}
	go a.loop(ctx)
	return a
}

func (a *tenantActor) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-a.inbox:
			switch m.kind {
			case msgAllocate:
				// Call PodOps to perform composite attach
				err := a.nm.AllocatePod(ctx, a.tenantID, m.epKey)
				if m.reply != nil {
					m.reply <- err
				}
			case msgRemove:
				// Call PodOps to perform composite detach
				err := a.nm.RemovePod(ctx, a.tenantID, m.epKey)
				if m.reply != nil {
					m.reply <- err
				}
			case msgStop:
				if m.reply != nil {
					m.reply <- nil
				}
				return
			}
		}
	}
}

// AllocatePod enqueues an allocate message and waits for completion with a bounded timeout.
func (a *tenantActor) AllocatePod(ctx context.Context, epKey string) error {
	reply := make(chan error, 1)
	msg := tenantMsg{kind: msgAllocate, epKey: epKey, reply: reply}
	select {
	case a.inbox <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}
	// Optional timeout to avoid indefinite waits
	timeout := time.Second * 10
	if dl, ok := ctx.Deadline(); ok {
		// derive remaining
		rem := time.Until(dl)
		if rem > 0 {
			timeout = rem
		}
	}
	select {
	case err := <-reply:
		return err
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}

// RemovePod enqueues a remove message and waits for completion with a bounded timeout.
func (a *tenantActor) RemovePod(ctx context.Context, epKey string) error {
	reply := make(chan error, 1)
	msg := tenantMsg{kind: msgRemove, epKey: epKey, reply: reply}
	select {
	case a.inbox <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}
	timeout := time.Second * 10
	if dl, ok := ctx.Deadline(); ok {
		rem := time.Until(dl)
		if rem > 0 {
			timeout = rem
		}
	}
	select {
	case err := <-reply:
		return err
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}

// Stop enqueues a stop message; returns when the actor acknowledges.
func (a *tenantActor) Stop(ctx context.Context) error {
	reply := make(chan error, 1)
	msg := tenantMsg{kind: msgStop, reply: reply}
	select {
	case a.inbox <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
