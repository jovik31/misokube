package dispatcher

import (
	"context"
	"fmt"
	"github/setera/internal/ebpfmanager"
	"github/setera/internal/nmanager"
	"log"
	"time"
)

func WithInboxSize(n int) Option {
	return func(d *Dispatcher) {

		if n > 0 {
			d.inbox = make(chan Command, n)
		}
	}
}

func WithSinkSize(n int) Option {
	return func(d *Dispatcher) {

		if n > 0 {
			d.sink = make(chan Event, n)
		}
	}
}

func New(nmt nmanager.TenantOps, nmn nmanager.NodestoreOps, ebpfmt ebpfmanager.TenantOps, ebpfmp ebpfmanager.PodOps, opts ...Option) *Dispatcher {
	d := &Dispatcher{
		nmt:    nmt,
		nmn:    nmn,
		ebpfmt: ebpfmt,
		ebpfmp: ebpfmp,
		inbox:  make(chan Command, 128),
		sink:   make(chan Event, 128),
	}
	for _, o := range opts {
		o(d)
	}

	// ensure inbox and sink are initialized
	if d.inbox == nil {
		d.inbox = make(chan Command, 128)
	}
	if d.sink == nil {
		d.sink = make(chan Event, 128)
	}

	return d
}

// WithPodOps injects a PodOps handler so dispatcher can route pod operations.
// (reverted) PodOps injection removed; dispatcher handles tenant ops only.

func (d *Dispatcher) Enqueue(cmd Command) {
	if cmd.timestamp.IsZero() {
		cmd.timestamp = time.Now()
	}
	log.Printf("dispatcher: enqueue op=%s tenant=%s", cmd.Op, cmd.TenantID)
	d.inbox <- cmd
}

func (d *Dispatcher) EnqeueueCtx(ctx context.Context, cmd Command) error {

	if cmd.timestamp.IsZero() {
		cmd.timestamp = time.Now()
	}

	select {
	case d.inbox <- cmd:
		log.Printf("dispatcher: enqueue ctx op=%s tenant=%s", cmd.Op, cmd.TenantID)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) Events() <-chan Event { return d.sink }

func (d *Dispatcher) Run(ctx context.Context) error {

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case cmd := <-d.inbox:
			log.Printf("dispatcher: processing op=%s tenant=%s", cmd.Op, cmd.TenantID)
			ev := Event{
				TenantID:  cmd.TenantID,
				Op:        cmd.Op,
				OpID:      cmd.OpID,
				timestamp: time.Now(),
			}

			var err error
			switch cmd.Op {
			case OpEnsure:
				if err = d.nmt.EnsureTenant(context.Background(), cmd.TenantID); err == nil && d.ebpfmt != nil {
					err = d.ebpfmt.EnsureTenantMaps(context.Background(), cmd.TenantID)
				}
			case OpRemove:
				if d.ebpfmt != nil {
					if err = d.ebpfmt.RemoveTenantMaps(context.Background(), cmd.TenantID); err != nil {
						break
					}
				}
				err = d.nmt.RemoveTenant(context.Background(), cmd.TenantID)
			case OpEnsurePeer:
				err = d.nmn.EnsurePeer(context.Background(), cmd.TenantID, cmd.Remote)
			case OpRemovePeer:
				err = d.nmn.RemovePeer(context.Background(), cmd.TenantID, cmd.Remote)
			case OpEnsurePodProg:
				if d.ebpfmp == nil {
					err = fmt.Errorf("pod eBPF ops not configured")
					break
				}
				err = d.ebpfmp.EnsurePodEndpoint(context.Background(), cmd.TenantID, cmd.PodName, cmd.IfName)
			case OpRemovePodProg:
				if d.ebpfmp == nil {
					err = fmt.Errorf("pod eBPF ops not configured")
					break
				}
				err = d.ebpfmp.RemovePodEndpoint(context.Background(), cmd.TenantID, cmd.PodName)
			case OpEnsureMap:
				if d.ebpfmt == nil {
					err = fmt.Errorf("tenant eBPF ops not configured")
					break
				}
				err = d.ebpfmt.EnsureTenantMaps(context.Background(), cmd.TenantID)
			case OpRemoveMap:
				if d.ebpfmt == nil {
					err = fmt.Errorf("tenant eBPF ops not configured")
					break
				}
				err = d.ebpfmt.RemoveTenantMaps(context.Background(), cmd.TenantID)
			case OpUpsertPodMap:
				if d.ebpfmp == nil {
					err = fmt.Errorf("pod eBPF ops not configured")
					break
				}
				err = d.ebpfmp.UpsertPodMapEntry(context.Background(), cmd.TenantID, cmd.PodName, cmd.PodIP, cmd.Ifindex)
			case OpDeletePodMap:
				if d.ebpfmp == nil {
					err = fmt.Errorf("pod eBPF ops not configured")
					break
				}
				err = d.ebpfmp.DeletePodMapEntry(context.Background(), cmd.PodIP)
			default:
				err = fmt.Errorf("unknown op %q", cmd.Op)
			}
			if err != nil {
				log.Printf("dispatcher: op=%s tenant=%s failed: %v", cmd.Op, cmd.TenantID, err)
			}
			ev.err = err
			ev.timestamp = time.Now()

			// non-blocking send to sink
			select {
			case d.sink <- ev:
			default:
			}
		}
	}

}
