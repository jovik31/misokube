package operator

import (
	"context"
	"fmt"
)

// ReconcileFunc handles a single source/event for a specific resource.
type ReconcileFunc func(ctx context.Context, src Source, res ResourceRef) error

// Router selects reconcile funcs by (source,event). Static (no runtime mutation).
type Router struct {
	ControllerName string
	table          map[Source]map[Event]ReconcileFunc
	def            ReconcileFunc
}

// NewRouter builds a router from a source→event→reconcile map and optional default.
func NewRouter(name string, matrix map[Source]map[Event]ReconcileFunc, def ReconcileFunc) *Router {
	// Shallow copy to avoid external mutation
	cp := make(map[Source]map[Event]ReconcileFunc, len(matrix))
	for s, row := range matrix {
		if row == nil {
			continue
		}
		rc := make(map[Event]ReconcileFunc, len(row))
		for e, fn := range row {
			if fn != nil {
				rc[e] = fn
			}
		}
		if len(rc) > 0 {
			cp[s] = rc
		}
	}
	return &Router{
		ControllerName: name,
		table:          cp,
		def:            def,
	}
}

func (r *Router) Name() string { return r.ControllerName }

func (r *Router) ReconcileEvent(ctx context.Context, src Source, ev Event, res ResourceRef) error {
	if row := r.table[src]; row != nil {
		if h := row[ev]; h != nil {
			return h(ctx, src, res)
		}
	}
	if r.def != nil {
		return r.def(ctx, src, res)
	}
	return fmt.Errorf("no handler for source=%q event=%q", src, ev)
}
