package orchestrator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	seterav1 "github/setera/pkg/api/setera.com/v1"
)

func ns(name string) *seterav1.NodeStore {
	// We only rely on .Name in recompute; Spec.Name is optional and may be empty.
	return &seterav1.NodeStore{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func ni(name string) seterav1.NodeInfo {
	return seterav1.NodeInfo{Name: name}
}

func TestRecompute_GrowAwaiting(t *testing.T) {
	op := &Operator{}

	ten := &seterav1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "ns1"},
		Spec:       seterav1.TenantSpec{Zones: 3},
		Status: seterav1.TenantStatus{
			AssignedNodes:             []seterav1.NodeInfo{ni("n1")},
			AwaitingNodeConfiguration: []string{"n2"},
		},
	}
	stores := []*seterav1.NodeStore{ns("n1"), ns("n2"), ns("n3"), ns("n4"), ns("n5")}

	newAwait, newAssign, changed := op.recomputeAwaitingAndAssignedForZones(ten, stores)
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if len(newAssign) != 1 {
		t.Fatalf("expected assigned unchanged len=1, got %d", len(newAssign))
	}
	if len(newAwait) != 2 {
		t.Fatalf("expected awaiting len=2, got %d", len(newAwait))
	}
	// must still contain the original awaiting "n2", must not include already assigned "n1"
	if !containsStr(newAwait, "n2") {
		t.Fatalf("expected awaiting to contain n2, got %v", newAwait)
	}
	if containsStr(newAwait, "n1") {
		t.Fatalf("awaiting must not include already assigned n1, got %v", newAwait)
	}
}

func TestRecompute_ShrinkAssigned(t *testing.T) {
	op := &Operator{}

	ten := &seterav1.Tenant{
		Spec: seterav1.TenantSpec{Zones: 1},
		Status: seterav1.TenantStatus{
			AssignedNodes:             []seterav1.NodeInfo{ni("n1"), ni("n2")},
			AwaitingNodeConfiguration: []string{"n3"},
		},
	}
	stores := []*seterav1.NodeStore{ns("n1"), ns("n2"), ns("n3")}

	newAwait, newAssign, changed := op.recomputeAwaitingAndAssignedForZones(ten, stores)
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if len(newAssign) != 0 {
		t.Fatalf("expected assigned trimmed to 0, got %d", len(newAssign))
	}
	// Awaiting untouched in shrink path
	if len(newAwait) != 1 || newAwait[0] != "n3" {
		t.Fatalf("expected awaiting unchanged [n3], got %v", newAwait)
	}
}

func TestRecompute_DedupAwaiting(t *testing.T) {
	op := &Operator{}

	ten := &seterav1.Tenant{
		Spec: seterav1.TenantSpec{Zones: 4},
		Status: seterav1.TenantStatus{
			AssignedNodes:             []seterav1.NodeInfo{ni("n1"), ni("n2")},
			AwaitingNodeConfiguration: []string{"n1", "n3"}, // n1 duplicated
		},
	}
	stores := []*seterav1.NodeStore{ns("n1"), ns("n2"), ns("n3"), ns("n4")}

	newAwait, newAssign, changed := op.recomputeAwaitingAndAssignedForZones(ten, stores)
	if !changed {
		t.Fatalf("expected changed=true (dedup awaiting)")
	}
	if len(newAssign) != 2 {
		t.Fatalf("expected assigned unchanged len=2, got %d", len(newAssign))
	}
	if containsStr(newAwait, "n1") {
		t.Fatalf("awaiting must not include already assigned n1, got %v", newAwait)
	}
	if !containsStr(newAwait, "n3") {
		t.Fatalf("expected awaiting to keep n3, got %v", newAwait)
	}
}

func TestRecompute_Idempotent(t *testing.T) {
	op := &Operator{}

	ten := &seterav1.Tenant{
		Spec: seterav1.TenantSpec{Zones: 3},
		Status: seterav1.TenantStatus{
			AssignedNodes:             []seterav1.NodeInfo{ni("n1")},
			AwaitingNodeConfiguration: []string{"n2", "n3"},
		},
	}
	stores := []*seterav1.NodeStore{ns("n1"), ns("n2"), ns("n3"), ns("n4")}

	newAwait, newAssign, changed := op.recomputeAwaitingAndAssignedForZones(ten, stores)
	if changed {
		t.Fatalf("expected changed=false (already matches zones)")
	}
	if !sameStrSlice(newAwait, ten.Status.AwaitingNodeConfiguration) {
		t.Fatalf("awaiting should remain unchanged, got %v", newAwait)
	}
	if !sameNodeInfos(newAssign, ten.Status.AssignedNodes) {
		t.Fatalf("assigned should remain unchanged, got %v", newAssign)
	}
}

func TestRecompute_NotEnoughStores(t *testing.T) {
	op := &Operator{}

	ten := &seterav1.Tenant{
		Spec: seterav1.TenantSpec{Zones: 5},
		Status: seterav1.TenantStatus{
			AssignedNodes:             []seterav1.NodeInfo{ni("n1")},
			AwaitingNodeConfiguration: []string{}, // start empty
		},
	}
	// Only two additional stores available => cannot fully reach zones
	stores := []*seterav1.NodeStore{ns("n1"), ns("n2"), ns("n3")}

	newAwait, newAssign, changed := op.recomputeAwaitingAndAssignedForZones(ten, stores)
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if len(newAssign) != 1 {
		t.Fatalf("expected assigned unchanged len=1, got %d", len(newAssign))
	}
	if len(newAwait) != 2 { // only n2 and n3 can be added
		t.Fatalf("expected awaiting len=2 due to limited stores, got %d", len(newAwait))
	}
	if containsStr(newAwait, "n1") {
		t.Fatalf("awaiting must not include already assigned n1, got %v", newAwait)
	}
}

func containsStr(xs []string, v string) bool {
	for _, s := range xs {
		if s == v {
			return true
		}
	}
	return false
}

func sameNodeInfos(a, b []seterav1.NodeInfo) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]seterav1.NodeInfo{}
	for _, x := range a {
		m[x.Name] = x
	}
	for _, y := range b {
		if x, ok := m[y.Name]; !ok || x != y {
			return false
		}
	}
	return true
}
