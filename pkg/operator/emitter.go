package operator

import (
	"context"
	"time"
)

// Emitter can enqueue work into an operator.
type Emitter interface {
	EnqueueWith(src Source, ev Event, res ResourceRef)
}

// BindChannel wires any typed channel into the operator.
// mapFn returns (source, event, resource, ok). If ok=false the item is dropped.
func BindChannel[T any](ctx context.Context, em Emitter, ch <-chan T, mapFn func(T) (Source, Event, ResourceRef, bool)) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case v, ok := <-ch:
				if !ok {
					return
				}
				if src, ev, res, ok := mapFn(v); ok {
					em.EnqueueWith(src, ev, res)
				}
			}
		}
	}()
}

// Periodic enqueues resources with the given source/event at a fixed interval.
func Periodic(ctx context.Context, em Emitter, every time.Duration, src Source, ev Event, resources ...ResourceRef) {
	if every <= 0 || len(resources) == 0 {
		return
	}
	t := time.NewTicker(every)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				for _, r := range resources {
					em.EnqueueWith(src, ev, r)
				}
			}
		}
	}()
}
