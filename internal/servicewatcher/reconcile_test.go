package servicewatcher

import (
	"net/netip"
	"testing"

	seteraebpf "github/setera/pkg/ebpf"
	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestBuildServicesResolvesManagedPodBackends(t *testing.T) {
	pods := mustPodSnapshot(t,
		podForService("app-a", "10.244.1.10", "tenant-a"),
		podForService("app-b", "10.244.2.20", "tenant-a"),
	)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "app",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:  "10.96.0.20",
			ClusterIPs: []string{"10.96.0.20"},
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}

	portName := "http"
	port := int32(8080)
	protocol := corev1.ProtocolTCP
	ready := true

	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "app-abc",
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{
				Name:     &portName,
				Port:     &port,
				Protocol: &protocol,
			},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: []string{"10.244.1.10"},
				Conditions: discoveryv1.EndpointConditions{
					Ready: &ready,
				},
				TargetRef: &corev1.ObjectReference{
					Kind:      "Pod",
					Namespace: "default",
					Name:      "app-a",
					UID:       types.UID("uid-app-a"),
				},
			},
			{
				Addresses: []string{"10.244.2.20"},
				Conditions: discoveryv1.EndpointConditions{
					Ready: &ready,
				},
				TargetRef: &corev1.ObjectReference{
					Kind:      "Pod",
					Namespace: "default",
					Name:      "app-b",
					UID:       types.UID("uid-app-b"),
				},
			},
		},
	}

	got, err := buildServices(
		service,
		[]*discoveryv1.EndpointSlice{slice},
		pods,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d Service frontends, want 1", len(got))
	}

	frontend := got[0]
	if frontend.Key.IP != netip.MustParseAddr("10.96.0.20") {
		t.Fatalf("frontend IP = %s", frontend.Key.IP)
	}
	if frontend.Key.Port != 80 {
		t.Fatalf("frontend port = %d, want 80", frontend.Key.Port)
	}
	if frontend.Key.Protocol != seteraebpf.ServiceProtocolTCP {
		t.Fatalf(
			"frontend protocol = %d, want TCP",
			frontend.Key.Protocol,
		)
	}
	if len(frontend.Backends) != 2 {
		t.Fatalf("got %d backends, want 2", len(frontend.Backends))
	}

	for _, backend := range frontend.Backends {
		if !backend.ManagedPod {
			t.Fatalf("backend %s was not marked managed", backend.IP)
		}
		if backend.TenantID != "tenant-a" {
			t.Fatalf(
				"backend %s tenant = %q, want tenant-a",
				backend.IP,
				backend.TenantID,
			)
		}
		if backend.Port != 8080 {
			t.Fatalf(
				"backend %s port = %d, want 8080",
				backend.IP,
				backend.Port,
			)
		}
	}
}

func TestBuildServicesKeepsExternalBackendUnmanaged(t *testing.T) {
	pods := mustPodSnapshot(t)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "external",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.30",
			Ports: []corev1.ServicePort{
				{
					Port:     443,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}

	port := int32(9443)
	protocol := corev1.ProtocolTCP
	ready := true
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{
				Port:     &port,
				Protocol: &protocol,
			},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: []string{"192.0.2.10"},
				Conditions: discoveryv1.EndpointConditions{
					Ready: &ready,
				},
			},
		},
	}

	got, err := buildServices(
		service,
		[]*discoveryv1.EndpointSlice{slice},
		pods,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Backends) != 1 {
		t.Fatalf("unexpected Service result: %#v", got)
	}

	backend := got[0].Backends[0]
	if backend.ManagedPod {
		t.Fatal("external backend was marked as managed Pod")
	}
	if backend.TenantID != "" {
		t.Fatalf("external backend tenant = %q, want empty", backend.TenantID)
	}
}

func TestBuildServicesSkipsNotReadyBackend(t *testing.T) {
	pods := mustPodSnapshot(t,
		podForService("app-a", "10.244.1.10", "tenant-a"),
	)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "app",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.20",
			Ports: []corev1.ServicePort{
				{
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}

	port := int32(8080)
	protocol := corev1.ProtocolTCP
	ready := false
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{
				Port:     &port,
				Protocol: &protocol,
			},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: []string{"10.244.1.10"},
				Conditions: discoveryv1.EndpointConditions{
					Ready: &ready,
				},
				TargetRef: &corev1.ObjectReference{
					Kind:      "Pod",
					Namespace: "default",
					Name:      "app-a",
					UID:       types.UID("uid-app-a"),
				},
			},
		},
	}

	got, err := buildServices(
		service,
		[]*discoveryv1.EndpointSlice{slice},
		pods,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d Service frontends, want 1", len(got))
	}
	if len(got[0].Backends) != 0 {
		t.Fatalf("got %d ready backends, want 0", len(got[0].Backends))
	}
}

func TestResolveBackendWithoutTargetRefStillDetectsManagedPod(t *testing.T) {
	pods := mustPodSnapshot(t,
		podForService("app-a", "10.244.1.10", "tenant-a"),
	)

	backend, include, err := resolveBackend(
		"default",
		netip.MustParseAddr("10.244.1.10"),
		8080,
		discoveryv1.Endpoint{},
		pods,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !include {
		t.Fatal("managed Pod backend was excluded")
	}
	if !backend.ManagedPod {
		t.Fatal("Pod IP without TargetRef was treated as external")
	}
	if backend.TenantID != "tenant-a" {
		t.Fatalf(
			"backend tenant = %q, want tenant-a",
			backend.TenantID,
		)
	}
}

func TestResolveBackendSkipsMismatchedPodTargetRef(t *testing.T) {
	pods := mustPodSnapshot(t,
		podForService("app-a", "10.244.1.10", "tenant-a"),
		podForService("app-b", "10.244.2.20", "tenant-b"),
	)

	_, include, err := resolveBackend(
		"default",
		netip.MustParseAddr("10.244.2.20"),
		8080,
		discoveryv1.Endpoint{
			TargetRef: &corev1.ObjectReference{
				Kind:      "Pod",
				Namespace: "default",
				Name:      "app-a",
				UID:       types.UID("uid-app-a"),
			},
		},
		pods,
	)
	if err != nil {
		t.Fatal(err)
	}
	if include {
		t.Fatal("EndpointSlice address that does not belong to TargetRef Pod was published")
	}
}

func TestBuildPodSnapshotRejectsDuplicateManagedPodIP(t *testing.T) {
	first := podForService("app-a", "10.244.1.10", "tenant-a")
	second := podForService("app-b", "10.244.1.10", "tenant-b")

	if _, err := buildPodSnapshot([]*corev1.Pod{first, second}); err == nil {
		t.Fatal("duplicate managed Pod IP was accepted")
	}
}

func TestResolveBackendUsesDefaultTenantForKubeSystem(t *testing.T) {
	pod := podForService("coredns", "10.244.0.3", "tenant-a")
	pod.Namespace = "kube-system"
	pod.UID = types.UID("uid-coredns")

	pods := mustPodSnapshot(t, pod)

	backend, include, err := resolveBackend(
		"kube-system",
		netip.MustParseAddr("10.244.0.3"),
		53,
		discoveryv1.Endpoint{
			TargetRef: &corev1.ObjectReference{
				Kind:      "Pod",
				Namespace: "kube-system",
				Name:      "coredns",
				UID:       types.UID("uid-coredns"),
			},
		},
		pods,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !include {
		t.Fatal("CoreDNS backend was excluded")
	}
	if backend.TenantID != tenantmeta.DefaultTenant {
		t.Fatalf(
			"CoreDNS tenant = %q, want %q",
			backend.TenantID,
			tenantmeta.DefaultTenant,
		)
	}
}

func mustPodSnapshot(
	t *testing.T,
	pods ...*corev1.Pod,
) podSnapshot {
	t.Helper()

	snapshot, err := buildPodSnapshot(pods)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func podForService(
	name string,
	ip string,
	tenant string,
) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
			UID:       types.UID("uid-" + name),
			Labels: map[string]string{
				tenantmeta.PodTenantLabel: tenant,
			},
		},
		Status: corev1.PodStatus{
			PodIP: ip,
			PodIPs: []corev1.PodIP{
				{IP: ip},
			},
		},
	}
}
