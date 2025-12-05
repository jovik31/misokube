package orchestrator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	seterav1 "github/setera/pkg/api/setera.com/v1"
)

func ns(name string) *seterav1.NodeStore {
	return &seterav1.NodeStore{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func ni(name string) seterav1.NodeInfo {
	return seterav1.NodeInfo{Name: name}
}

func TestZones_GrowAwaiting(t *testing.T) {
	op := &Operator{}

	ten := &seterav1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
		Spec:       seterav1.TenantSpec{Name: "acme", Zones: 3},
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
	if containsStr(newAwait, "n1") {
		t.Fatalf("awaiting must not include already assigned n1, got %v", newAwait)
	}
	if !containsStr(newAwait, "n2") {
		t.Fatalf("awaiting should keep existing n2, got %v", newAwait)
	}
}

func TestZones_ShrinkAssignedOnly(t *testing.T) {
	op := &Operator{}

	ten := &seterav1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
		Spec:       seterav1.TenantSpec{Name: "acme", Zones: 1},
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
	// We only trim assigned, never awaiting
	if len(newAwait) != 1 || newAwait[0] != "n3" {
		t.Fatalf("awaiting must remain unchanged, got %v", newAwait)
	}
	if len(newAssign) != 0 {
		t.Fatalf("assigned must be trimmed to 0, got %d", len(newAssign))
	}
}

func TestZones_DedupAwaitingVsAssigned(t *testing.T) {
	op := &Operator{}

	ten := &seterav1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
		Spec:       seterav1.TenantSpec{Name: "acme", Zones: 3},
		Status: seterav1.TenantStatus{
			AssignedNodes:             []seterav1.NodeInfo{ni("n1")},
			AwaitingNodeConfiguration: []string{"n1", "n2"},
		},
	}
	stores := []*seterav1.NodeStore{ns("n1"), ns("n2"), ns("n3")}

	newAwait, newAssign, changed := op.recomputeAwaitingAndAssignedForZones(ten, stores)
	if !changed {
		t.Fatalf("expected changed=true due to dedup")
	}
	if containsStr(newAwait, "n1") {
		t.Fatalf("awaiting must not duplicate assigned node n1, got %v", newAwait)
	}
	if len(newAssign) != 1 || newAssign[0].Name != "n1" {
		t.Fatalf("assigned should remain [n1], got %v", newAssign)
	}
}

func TestZones_IdempotentWhenSatisfied(t *testing.T) {
	op := &Operator{}

	ten := &seterav1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
		Spec:       seterav1.TenantSpec{Name: "acme", Zones: 3},
		Status: seterav1.TenantStatus{
			AssignedNodes:             []seterav1.NodeInfo{ni("n1")},
			AwaitingNodeConfiguration: []string{"n2", "n3"},
		},
	}
	stores := []*seterav1.NodeStore{ns("n1"), ns("n2"), ns("n3"), ns("n4")}

	newAwait, newAssign, changed := op.recomputeAwaitingAndAssignedForZones(ten, stores)
	if changed {
		t.Fatalf("expected changed=false when already matching zones")
	}
	if !sameStrSlice(newAwait, ten.Status.AwaitingNodeConfiguration) {
		t.Fatalf("awaiting should be unchanged, got %v", newAwait)
	}
	if !equalNodeInfosByValue(newAssign, ten.Status.AssignedNodes) {
		t.Fatalf("assigned should be unchanged, got %v", newAssign)
	}
}

func TestZones_BestEffortWhenInsufficientNodes(t *testing.T) {
	op := &Operator{}

	ten := &seterav1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
		Spec:       seterav1.TenantSpec{Name: "acme", Zones: 5},
		Status: seterav1.TenantStatus{
			AssignedNodes:             []seterav1.NodeInfo{ni("n1")},
			AwaitingNodeConfiguration: nil,
		},
	}
	// Only two more available (n2,n3)
	stores := []*seterav1.NodeStore{ns("n1"), ns("n2"), ns("n3")}

	newAwait, newAssign, changed := op.recomputeAwaitingAndAssignedForZones(ten, stores)
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if len(newAssign) != 1 {
		t.Fatalf("assigned must remain len=1, got %d", len(newAssign))
	}
	if len(newAwait) != 2 {
		t.Fatalf("awaiting should be best-effort len=2, got %d", len(newAwait))
	}
	if containsStr(newAwait, "n1") {
		t.Fatalf("awaiting must not include already assigned n1, got %v", newAwait)
	}
}
