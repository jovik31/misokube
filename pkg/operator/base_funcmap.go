package operator

import "context"

// FuncMapReconciler dispatches events to a map of handlers.
type FuncMapReconciler struct {
	ControllerName string
	Handlers       map[Event]func(ctx context.Context, key string) error
	Default        func(ctx context.Context, ev Event, key string) error
}

func (f *FuncMapReconciler) Name() string { return f.ControllerName }

func (f *FuncMapReconciler) ReconcileEvent(ctx context.Context, ev Event, key string) error {
	if h, ok := f.Handlers[ev]; ok && h != nil {
		return h(ctx, key)
	}
	if f.Default != nil {
		return f.Default(ctx, ev, key)
	}
	return nil
}

// SingleReconciler is a simple reconciler that ignores events.
type SingleReconciler interface {
	Name() string
	Reconcile(ctx context.Context, key string) error
}

// SingleAdapter adapts a SingleReconciler to EventReconciler.
type SingleAdapter struct {
	Single SingleReconciler
	// Optional: Allowed limits which events are processed; empty means all.
	Allowed map[Event]struct{}
}

func (a *SingleAdapter) Name() string { return a.Single.Name() }

func (a *SingleAdapter) ReconcileEvent(ctx context.Context, ev Event, key string) error {
	if len(a.Allowed) > 0 {
		if _, ok := a.Allowed[ev]; !ok {
			return nil
		}
	}
	return a.Single.Reconcile(ctx, key)
}
