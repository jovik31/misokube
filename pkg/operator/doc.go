// Package operator provides a generic, event-aware base operator built on top of
// client-go informers and a rate-limiting workqueue.
//
// Key concepts:
//   - WorkItem: carries Source, Event, ResourceRef
//   - baseOperator: manages informers, queue, workers and enqueues WorkItems
//   - Router + ReconcileFunc: selects reconcile logic by source/event (static)
//   - Emitter helpers: bind external channels and periodic events
//   - Informer handlers: MakeInformerHandlers, FilteredInformerHandlers,
//     AddInformerHandlers, AddK8SInformerHandlers
package operator
