package webhook

import (
	"context"
	"encoding/json"
	"testing"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	seterafake "github/setera/pkg/generated/clientset/versioned/fake"
	"github/setera/pkg/tenantmeta"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestAdmitPodAllowsDefaultTenant(t *testing.T) {
	server := newTestWebhookServer()

	request := podAdmissionRequest(
		t,
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "pod-a",
			},
		},
	)

	response, err := server.admitPod(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !response.Allowed {
		t.Fatalf(
			"Pod was denied: %s",
			response.Result.Message,
		)
	}

	if len(response.Patch) != 0 {
		t.Fatalf(
			"default tenant Pod has unexpected patch: %s",
			response.Patch,
		)
	}
}

func TestAdmitPodRejectsUnknownTenant(
	t *testing.T,
) {
	server := newTestWebhookServer()

	request := podAdmissionRequest(
		t,
		tenantPod("tenant-a"),
	)

	response, err := server.admitPod(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if response.Allowed {
		t.Fatal("expected Pod denial")
	}

	if response.Result.Message != tenantNotFound {
		t.Fatalf(
			"got reason %q, want %q",
			response.Result.Message,
			tenantNotFound,
		)
	}
}

func TestAdmitPodRejectsTenantThatIsNotAssigned(
	t *testing.T,
) {
	tenant := readyTenant("tenant-a", 1)

	tenant.Status.Conditions[0].Status =
		metav1.ConditionFalse

	server := newTestWebhookServer(tenant)

	request := podAdmissionRequest(
		t,
		tenantPod("tenant-a"),
	)

	response, err := server.admitPod(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if response.Allowed {
		t.Fatal("expected Pod denial")
	}

	if response.Result.Message != tenantNotAssigned {
		t.Fatalf(
			"got reason %q, want %q",
			response.Result.Message,
			tenantNotAssigned,
		)
	}
}

func TestAdmitPodRejectsStaleTenantStatus(
	t *testing.T,
) {
	tenant := readyTenant("tenant-a", 2)

	tenant.Status.Conditions[0].
		ObservedGeneration = 1

	server := newTestWebhookServer(tenant)

	request := podAdmissionRequest(
		t,
		tenantPod("tenant-a"),
	)

	response, err := server.admitPod(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if response.Allowed {
		t.Fatal("expected Pod denial")
	}

	if response.Result.Message != tenantNotAssigned {
		t.Fatalf(
			"got reason %q, want %q",
			response.Result.Message,
			tenantNotAssigned,
		)
	}
}

func TestAdmitPodAddsTenantNodeSelector(
	t *testing.T,
) {
	server := newTestWebhookServer(
		readyTenant("tenant-a", 1),
	)

	pod := tenantPod("tenant-a")

	pod.Spec.NodeSelector = map[string]string{
		"kubernetes.io/arch": "amd64",
	}

	request := podAdmissionRequest(t, pod)

	response, err := server.admitPod(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !response.Allowed {
		t.Fatalf(
			"Pod was denied: %s",
			response.Result.Message,
		)
	}

	if response.PatchType == nil ||
		*response.PatchType !=
			admissionv1.PatchTypeJSONPatch {
		t.Fatalf(
			"got patch type %v, want JSONPatch",
			response.PatchType,
		)
	}

	var patch []struct {
		Op    string            `json:"op"`
		Path  string            `json:"path"`
		Value map[string]string `json:"value"`
	}

	if err := json.Unmarshal(
		response.Patch,
		&patch,
	); err != nil {
		t.Fatalf(
			"decode patch: %v",
			err,
		)
	}

	if len(patch) != 1 {
		t.Fatalf(
			"got %d patch operations, want 1",
			len(patch),
		)
	}

	if patch[0].Op != "add" ||
		patch[0].Path != "/spec/nodeSelector" {
		t.Fatalf(
			"unexpected patch operation: %#v",
			patch[0],
		)
	}

	labelKey, err := tenantmeta.NodeTenantLabel(
		"tenant-a",
	)
	if err != nil {
		t.Fatal(err)
	}

	if patch[0].Value[labelKey] != "true" {
		t.Fatalf(
			"tenant selector = %q, want true",
			patch[0].Value[labelKey],
		)
	}

	if patch[0].Value["kubernetes.io/arch"] !=
		"amd64" {
		t.Fatal(
			"existing node selector was not preserved",
		)
	}
}

func TestAdmitPodRejectsNodeName(t *testing.T) {
	server := newTestWebhookServer(
		readyTenant("tenant-a", 1),
	)

	pod := tenantPod("tenant-a")
	pod.Spec.NodeName = "node-a"

	request := podAdmissionRequest(t, pod)

	response, err := server.admitPod(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if response.Allowed {
		t.Fatal("expected Pod denial")
	}

	if response.Result.Message !=
		podNodeNameNotAllowed {
		t.Fatalf(
			"got reason %q, want %q",
			response.Result.Message,
			podNodeNameNotAllowed,
		)
	}
}

func TestAdmitPodRejectsConflictingTenantSelector(
	t *testing.T,
) {
	server := newTestWebhookServer(
		readyTenant("tenant-a", 1),
	)

	pod := tenantPod("tenant-a")

	otherLabel, err := tenantmeta.NodeTenantLabel(
		"tenant-b",
	)
	if err != nil {
		t.Fatal(err)
	}

	pod.Spec.NodeSelector = map[string]string{
		otherLabel: "true",
	}

	request := podAdmissionRequest(t, pod)

	response, err := server.admitPod(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if response.Allowed {
		t.Fatal("expected Pod denial")
	}

	if response.Result.Message !=
		tenantNodeSelectorConflict {
		t.Fatalf(
			"got reason %q, want %q",
			response.Result.Message,
			tenantNodeSelectorConflict,
		)
	}
}

func TestAdmitPodDoesNotPatchExistingTenantSelector(
	t *testing.T,
) {
	server := newTestWebhookServer(
		readyTenant("tenant-a", 1),
	)

	pod := tenantPod("tenant-a")

	labelKey, err := tenantmeta.NodeTenantLabel(
		"tenant-a",
	)
	if err != nil {
		t.Fatal(err)
	}

	pod.Spec.NodeSelector = map[string]string{
		labelKey: "true",
	}

	request := podAdmissionRequest(t, pod)

	response, err := server.admitPod(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !response.Allowed {
		t.Fatalf(
			"Pod was denied: %s",
			response.Result.Message,
		)
	}

	if len(response.Patch) != 0 {
		t.Fatalf(
			"Pod has unexpected patch: %s",
			response.Patch,
		)
	}
}

func newTestWebhookServer(
	objects ...runtime.Object,
) *WebhookServer {
	return NewWebhookServer(
		seterafake.NewSimpleClientset(
			objects...,
		),
		fake.NewSimpleClientset(),
		"",
		"",
	)
}

func podAdmissionRequest(
	t *testing.T,
	pod *corev1.Pod,
) *admissionv1.AdmissionRequest {
	t.Helper()

	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatal(err)
	}

	return &admissionv1.AdmissionRequest{
		Namespace: pod.Namespace,
		Object: runtime.RawExtension{
			Raw: raw,
		},
	}
}

func tenantPod(
	tenantName string,
) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod-a",
			Labels: map[string]string{
				tenantmeta.PodTenantLabel: tenantName,
			},
		},
	}
}

func readyTenant(
	name string,
	generation int64,
) *seterav1.Tenant {
	return &seterav1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Generation: generation,
		},
		Spec: seterav1.TenantSpec{
			Zones: 1,
		},
		Status: seterav1.TenantStatus{
			AssignedNodes: []string{
				"node-a",
			},
			Conditions: []metav1.Condition{
				{
					Type: tenantAssignedCondition,
					Status: metav1.
						ConditionTrue,
					ObservedGeneration: generation,
					Reason:             "Assigned",
				},
			},
		},
	}
}
