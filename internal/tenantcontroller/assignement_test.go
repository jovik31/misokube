package tenantcontroller

import (
	"testing"

	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScaleUpCandidatesPreferFewerTenants(
	t *testing.T,
) {
	n1 := readyNode("node-a")
	n2 := readyNode("node-b")
	n3 := readyNode("node-c")

	label, err :=
		tenantmeta.NodeTenantLabel("other")
	if err != nil {
		t.Fatal(err)
	}

	n1.Labels[label] = "true"

	got := scaleUpCandidates(
		[]*corev1.Node{
			n1,
			n2,
			n3,
		},
		"tenant-a",
	)

	if len(got) != 3 {
		t.Fatalf(
			"got %d candidates, want 3",
			len(got),
		)
	}

	if got[0].Name != "node-b" ||
		got[1].Name != "node-c" ||
		got[2].Name != "node-a" {
		t.Fatalf(
			"unexpected candidate order: %s, %s, %s",
			got[0].Name,
			got[1].Name,
			got[2].Name,
		)
	}
}

func TestScaleUpCandidatesSkipAssignedAndUnavailableNodes(
	t *testing.T,
) {
	assigned := readyNode("assigned")

	unschedulable :=
		readyNode("unschedulable")
	unschedulable.Spec.Unschedulable = true

	notReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "not-ready",
			Labels: map[string]string{
				tenantmeta.NodeVTEPReadyLabel: "true",
			},
		},
	}

	available := readyNode("available")

	label, err :=
		tenantmeta.NodeTenantLabel(
			"tenant-a",
		)
	if err != nil {
		t.Fatal(err)
	}

	assigned.Labels[label] = "true"

	got := scaleUpCandidates(
		[]*corev1.Node{
			assigned,
			unschedulable,
			notReady,
			available,
		},
		"tenant-a",
	)

	if len(got) != 1 ||
		got[0].Name != "available" {
		t.Fatalf(
			"got %v, want only available",
			nodeNames(got),
		)
	}
}

func TestScaleUpCandidatesSkipNodeWithoutSeteraDataplane(
	t *testing.T,
) {
	ready := readyNode("ready")

	withoutDataplane :=
		readyNode("without-dataplane")

	delete(
		withoutDataplane.Labels,
		tenantmeta.NodeVTEPReadyLabel,
	)

	got := scaleUpCandidates(
		[]*corev1.Node{
			withoutDataplane,
			ready,
		},
		"tenant-a",
	)

	if len(got) != 1 ||
		got[0].Name != "ready" {
		t.Fatalf(
			"got %v, want ready",
			nodeNames(got),
		)
	}
}

func TestScaleUpCandidatesSkipNoScheduleNode(
	t *testing.T,
) {
	tainted := readyNode("tainted")

	tainted.Spec.Taints =
		[]corev1.Taint{
			{
				Key: "node-role.kubernetes.io/control-plane",
				Effect: corev1.
					TaintEffectNoSchedule,
			},
		}

	available := readyNode("available")

	got := scaleUpCandidates(
		[]*corev1.Node{
			tainted,
			available,
		},
		"tenant-a",
	)

	if len(got) != 1 ||
		got[0].Name != "available" {
		t.Fatalf(
			"got %v, want available",
			nodeNames(got),
		)
	}
}

func TestScaleUpCandidatesSkipNoExecuteNode(
	t *testing.T,
) {
	tainted := readyNode("tainted")

	tainted.Spec.Taints =
		[]corev1.Taint{
			{
				Key: "maintenance",
				Effect: corev1.
					TaintEffectNoExecute,
			},
		}

	available := readyNode("available")

	got := scaleUpCandidates(
		[]*corev1.Node{
			tainted,
			available,
		},
		"tenant-a",
	)

	if len(got) != 1 ||
		got[0].Name != "available" {
		t.Fatalf(
			"got %v, want available",
			nodeNames(got),
		)
	}
}

func TestScaleUpCandidatesAllowsPreferNoScheduleNode(
	t *testing.T,
) {
	node := readyNode("node-a")

	node.Spec.Taints =
		[]corev1.Taint{
			{
				Key: "preferred-workload",
				Effect: corev1.
					TaintEffectPreferNoSchedule,
			},
		}

	got := scaleUpCandidates(
		[]*corev1.Node{
			node,
		},
		"tenant-a",
	)

	if len(got) != 1 ||
		got[0].Name != "node-a" {
		t.Fatalf(
			"got %v, want node-a",
			nodeNames(got),
		)
	}
}

func TestScaleDownCandidatesSkipNodesWithActiveTenantPods(
	t *testing.T,
) {
	n1 := readyNode("node-a")
	n2 := readyNode("node-b")

	got := scaleDownCandidates(
		[]*corev1.Node{
			n1,
			n2,
		},
		map[string]struct{}{
			"node-b": {},
		},
	)

	if len(got) != 1 ||
		got[0].Name != "node-a" {
		t.Fatalf(
			"got %v, want node-a",
			nodeNames(got),
		)
	}
}

func TestScaleDownCandidatesPreferMostLoadedNode(
	t *testing.T,
) {
	n1 := readyNode("node-a")
	n2 := readyNode("node-b")

	labelOne, err :=
		tenantmeta.NodeTenantLabel(
			"other-one",
		)
	if err != nil {
		t.Fatal(err)
	}

	labelTwo, err :=
		tenantmeta.NodeTenantLabel(
			"other-two",
		)
	if err != nil {
		t.Fatal(err)
	}

	n1.Labels[labelOne] = "true"
	n1.Labels[labelTwo] = "true"
	n2.Labels[labelOne] = "true"

	got := scaleDownCandidates(
		[]*corev1.Node{
			n1,
			n2,
		},
		map[string]struct{}{},
	)

	if len(got) != 2 {
		t.Fatalf(
			"got %d candidates, want 2",
			len(got),
		)
	}

	if got[0].Name != "node-a" {
		t.Fatalf(
			"got first candidate %s, want node-a",
			got[0].Name,
		)
	}
}

func TestAssignedNodesReturnsOnlyTenantNodes(
	t *testing.T,
) {
	n1 := readyNode("node-a")
	n2 := readyNode("node-b")

	label, err :=
		tenantmeta.NodeTenantLabel(
			"tenant-a",
		)
	if err != nil {
		t.Fatal(err)
	}

	n2.Labels[label] = "true"

	got := assignedNodes(
		[]*corev1.Node{
			n1,
			n2,
		},
		"tenant-a",
	)

	if len(got) != 1 ||
		got[0].Name != "node-b" {
		t.Fatalf(
			"got %v, want node-b",
			nodeNames(got),
		)
	}
}

func TestNodeEligibleRejectsNoScheduleTaint(
	t *testing.T,
) {
	node := readyNode("node-a")

	node.Spec.Taints =
		[]corev1.Taint{
			{
				Key: "test",
				Effect: corev1.
					TaintEffectNoSchedule,
			},
		}

	if nodeEligible(node) {
		t.Fatal(
			"expected node to be ineligible",
		)
	}
}

func TestNodeEligibleAllowsPreferNoScheduleTaint(
	t *testing.T,
) {
	node := readyNode("node-a")

	node.Spec.Taints =
		[]corev1.Taint{
			{
				Key: "test",
				Effect: corev1.
					TaintEffectPreferNoSchedule,
			},
		}

	if !nodeEligible(node) {
		t.Fatal(
			"expected node to be eligible",
		)
	}
}

func readyNode(
	name string,
) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				tenantmeta.NodeVTEPReadyLabel: "true",
			},
		},

		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type: corev1.
						NodeReady,
					Status: corev1.
						ConditionTrue,
				},
			},
		},
	}
}

func nodeNames(
	nodes []*corev1.Node,
) []string {
	out := make(
		[]string,
		0,
		len(nodes),
	)

	for _, node := range nodes {
		out = append(
			out,
			node.Name,
		)
	}

	return out
}
