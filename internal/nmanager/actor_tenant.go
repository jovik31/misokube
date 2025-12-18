package nmanager

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	types100 "github.com/containernetworking/cni/pkg/types/100"

	"github/setera/internal/router"
	"github/setera/pkg/network/ipam"
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
					err = a.ensurePodWithExpansion(ctx, m.args, &ipNet, &gatewayIP, &ifName, &res)
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

func (a *tenantActor) ensurePodWithExpansion(ctx context.Context, args router.PodAttachArgs, ipNet *net.IPNet, gatewayIP *net.IP, ifName *string, res **types100.Result) error {
	const maxRetries = 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		netRes, gw, iface, err := a.nm.EnsurePod(ctx, a.tenantID, args)
		if err == nil {
			*ipNet = netRes
			*gatewayIP = gw
			*ifName = iface
			*res = buildPodResult(netRes, gw, iface, args.NetNS)
			return nil
		}
		var noIPs ipam.ErrNoAvailableIPs
		if errors.As(err, &noIPs) {
			if attempt == maxRetries {
				return err
			}
			log.Printf("tenant=%s pod=%s exhausted IPAM (attempt=%d): %v", a.tenantID, args.PodName, attempt, err)
			expandDone := time.Now()
			if expErr := a.nm.requestTenantExpansion(ctx, a.tenantID); expErr != nil {
				return fmt.Errorf("expand tenant: %w", expErr)
			}
			if err := a.reconcileExistingPods(ctx, args); err != nil {
				return fmt.Errorf("reconcile pods post-expand: %w", err)
			}
			log.Printf("tenant=%s expansion completed in %s", a.tenantID, time.Since(expandDone))
			continue
		}
		return err
	}
	return fmt.Errorf("ensure pod failed after retries")
}

func (a *tenantActor) reconcileExistingPods(ctx context.Context, trigger router.PodAttachArgs) error {
	a.nm.mu.RLock()
	rec := a.nm.TenantRecords[a.tenantID]
	a.nm.mu.RUnlock()
	if rec == nil || rec.IPAM == nil {
		return fmt.Errorf("tenant record/ipam missing for %s", a.tenantID)
	}

	allocs := rec.IPAM.ListAllocations()
	log.Printf("tenant=%s reconciliation start pods=%d trigger=%s/%s", a.tenantID, len(allocs), trigger.Namespace, trigger.PodName)
	for allocationKey, info := range allocs {
		if info == nil {
			continue
		}
		nsName, podName := splitPodKey(allocationKey)
		args := router.PodAttachArgs{
			PodName:     podName,
			NetNS:       info.NetNS,
			IfName:      info.IFname,
			ContainerID: info.ID,
			Namespace:   nsName,
		}
		if err := a.nm.UpdatePod(ctx, a.tenantID, args); err != nil {
			log.Printf("tenant=%s pod=%s update failed after expansion: %v", a.tenantID, allocationKey, err)
		} else {
			log.Printf("tenant=%s pod=%s reconciled", a.tenantID, allocationKey)
		}
	}
	log.Printf("tenant=%s reconciliation complete", a.tenantID)
	return nil
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
