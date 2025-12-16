package nmanager

import (
	"context"
	"log"
	"net"
	"time"

	types100 "github.com/containernetworking/cni/pkg/types/100"

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
	reply chan tenantReply
}

type tenantReply struct {
	result *types100.Result
	err    error
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
				var (
					err       error
					ipNet     net.IPNet
					gatewayIP net.IP
					ifName    string
					res       *types100.Result
				)
				if state == TenantStateClosing {
					err = ErrTenantClosing
				} else {
					ipNet, gatewayIP, ifName, err = a.nm.EnsurePod(ctx, a.tenantID, m.args)
					if err == nil {
						res = buildPodResult(ipNet, gatewayIP, ifName, m.args.NetNS)
					}
				}
				if m.reply != nil {
					m.reply <- tenantReply{result: res, err: err}
				}
			case msgRemove:
				// RemovePod allowed; if already closing, removal is still safe/idempotent
				err := a.nm.RemovePod(ctx, a.tenantID, m.args)
				if m.reply != nil {
					m.reply <- tenantReply{err: err}
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
					m.reply <- tenantReply{err: err}
				}
			case msgStop:
				if m.reply != nil {
					m.reply <- tenantReply{}
				}
				return
			}
		}
	}
}

// EnsurePod enqueues an ensure message and waits for completion with a bounded timeout.
func (a *tenantActor) EnsurePod(ctx context.Context, args router.PodAttachArgs) (*types100.Result, error) {
	reply := make(chan tenantReply, 1)
	msg := tenantMsg{kind: msgEnsure, args: args, reply: reply}
	select {
	case a.inbox <- msg:
	case <-ctx.Done():
		return nil, ctx.Err()
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
	case resp := <-reply:
		return resp.result, resp.err
	case <-time.After(timeout):
		return nil, context.DeadlineExceeded
	}
}

// RemovePod enqueues a remove message and waits for completion with a bounded timeout.
func (a *tenantActor) RemovePod(ctx context.Context, args router.PodAttachArgs) error {
	reply := make(chan tenantReply, 1)
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
	case resp := <-reply:
		return resp.err
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}

// UpdatePod enqueues an update message and waits for completion with a bounded timeout.
func (a *tenantActor) UpdatePod(ctx context.Context, args router.PodAttachArgs) error {
	reply := make(chan tenantReply, 1)
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
	case resp := <-reply:
		return resp.err
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}

// Stop enqueues a stop message; returns when the actor acknowledges.
func (a *tenantActor) Stop(ctx context.Context) error {
	reply := make(chan tenantReply, 1)
	msg := tenantMsg{kind: msgStop, reply: reply}
	select {
	case a.inbox <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case resp := <-reply:
		return resp.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func buildPodResult(ipNet net.IPNet, gateway net.IP, ifName, netNS string) *types100.Result {
	res := &types100.Result{
		Interfaces: []*types100.Interface{
			{
				Name:    ifName,
				Sandbox: netNS,
			},
		},
		IPs: []*types100.IPConfig{
			{
				Address: ipNet,
				Gateway: gateway,
			},
		},
	}
	idx := 0
	res.IPs[0].Interface = &idx
	return res
}
