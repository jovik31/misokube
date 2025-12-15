package nmanager

import (
	"context"
	"log"
	"time"

	"github/setera/internal/router"
)

type tenantMsgKind int

const (
	msgEnsure tenantMsgKind = iota
	msgRemove
	msgUpdate
	msgStop
)

type tenantMsg struct {
	kind  tenantMsgKind
	args  router.PodAttachArgs
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
	log.Print("tenant actor started for tenant=", a.tenantID)
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-a.inbox:
			switch m.kind {
			case msgEnsure:
				// Gate pod ops when tenant is closing
				a.nm.mu.RLock()
				rec := a.nm.TenantRecords[a.tenantID]
				state := TenantStateReady
				if rec != nil {
					state = rec.State
				}
				a.nm.mu.RUnlock()
				var err error
				if state == TenantStateClosing {
					err = ErrTenantClosing
				} else {
					err = a.nm.EnsurePod(ctx, a.tenantID, m.args)
				}
				if m.reply != nil {
					m.reply <- err
				}
			case msgRemove:
				// RemovePod allowed; if already closing, removal is still safe/idempotent
				err := a.nm.RemovePod(ctx, a.tenantID, m.args)
				if m.reply != nil {
					m.reply <- err
				}
			case msgUpdate:
				// Allow update only if not closing
				a.nm.mu.RLock()
				rec := a.nm.TenantRecords[a.tenantID]
				state := TenantStateReady
				if rec != nil {
					state = rec.State
				}
				a.nm.mu.RUnlock()
				var err error
				if state == TenantStateClosing {
					err = ErrTenantClosing
				} else {
					err = a.nm.UpdatePod(ctx, a.tenantID, m.args)
				}
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

// EnsurePod enqueues an ensure message and waits for completion with a bounded timeout.
func (a *tenantActor) EnsurePod(ctx context.Context, args router.PodAttachArgs) error {
	reply := make(chan error, 1)
	msg := tenantMsg{kind: msgEnsure, args: args, reply: reply}
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
func (a *tenantActor) RemovePod(ctx context.Context, args router.PodAttachArgs) error {
	reply := make(chan error, 1)
	msg := tenantMsg{kind: msgRemove, args: args, reply: reply}
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

// UpdatePod enqueues an update message and waits for completion with a bounded timeout.
func (a *tenantActor) UpdatePod(ctx context.Context, args router.PodAttachArgs) error {
	reply := make(chan error, 1)
	msg := tenantMsg{kind: msgUpdate, args: args, reply: reply}
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
