package operator

// Source identifies where the event came from (e.g., "k8s:nodestore", "nm:dispatcher").
type Source string

// Event is a user-defined label for an event (e.g., "add", "update", "delete", "ensure").
type Event string

// ResourceRef identifies the affected resource (K8s or domain-specific).
// Leave Namespace empty for cluster-scoped resources.
type ResourceRef struct {
	Group     string
	Version   string
	Kind      string
	Namespace string
	Name      string
}

// WorkItem is what the queue carries.
type WorkItem struct {
	Source   Source
	Event    Event
	Resource ResourceRef
}
