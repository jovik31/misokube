package orchestrator

/*
import (
	"testing"

	seterav1 "github/setera/pkg/api/setera.com/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: These tests assume Operator.base is an interface with EnqueueObjectWith/EnqueueWith.
// If it's a concrete type, consider refactoring to an interface for testability.

func TestUpdateTenantHandler_EnqueueOnGenerationBump(t *testing.T) {
	op := &Operator{}
	sink := &enqueueSink{}
	op.base = sink // ensure this matches your Operator.base type (interface recommended)

	oldT := &seterav1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"}, Spec: seterav1.TenantSpec{}}
	oldT.Generation = 1
	newT := oldT.DeepCopy()
	newT.Generation = 2

	op.updateEventTenantHandler(oldT, newT)

	if sink.calls() != 1 {
		t.Fatalf("expected 1 enqueue on spec change, got %d", sink.calls())
	}
}

func TestUpdateTenantHandler_NoEnqueueOnMetadataOnly(t *testing.T) {
	op := &Operator{}
	sink := &enqueueSink{}
	op.base = sink

	oldT := &seterav1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"}, Spec: seterav1.TenantSpec{}}
	oldT.Generation = 2
	newT := oldT.DeepCopy()
	newT.ResourceVersion = "999" // metadata-only

	op.updateEventTenantHandler(oldT, newT)

	if sink.calls() != 0 {
		t.Fatalf("expected 0 enqueues on metadata-only update, got %d", sink.calls())
	}
}

type enqueueSink struct {
	adds int
}

func (e *enqueueSink) EnqueueObjectWith(_ Source, _ Event, _ any) { e.adds++ }
func (e *enqueueSink) EnqueueWith(_ Source, _ Event, _ ResourceRef) {
	e.adds++
}
func (e *enqueueSink) calls() int { return e.adds }
*/
