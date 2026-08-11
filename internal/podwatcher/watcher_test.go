package podwatcher

import (
	"context"
	"net/netip"
	"testing"

	"github/setera/internal/ebpfmanager"
	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRemotePodForExplicitTenant(t *testing.T) {
	local := testNode(t, "node-a", "tenant-a")
	remoteNode := testNode(t, "node-b", "tenant-a")
	pod := testPod("app", "pod-a", "node-b", "10.244.2.10")
	pod.Labels = map[string]string{
		tenantmeta.PodTenantLabel: "tenant-a",
	}

	got, relevant, err := remotePodFor(local, remoteNode, pod)
	if err != nil {
		t.Fatal(err)
	}
	if !relevant {
		t.Fatal("expected remote Pod to be relevant")
	}
	if got.TenantID != "tenant-a" {
		t.Fatalf("tenant = %q, want tenant-a", got.TenantID)
	}
}

func TestRemotePodForRequiresTenantOnBothNodes(t *testing.T) {
	tests := []struct {
		name         string
		localTenants []string
		remote       []string
	}{
		{
			name:         "local missing",
			localTenants: nil,
			remote:       []string{"tenant-a"},
		},
		{
			name:         "remote missing",
			localTenants: []string{"tenant-a"},
			remote:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local := testNode(t, "node-a", tt.localTenants...)
			remoteNode := testNode(t, "node-b", tt.remote...)
			pod := testPod(
				"app",
				"pod-a",
				"node-b",
				"10.244.2.10",
			)
			pod.Labels = map[string]string{
				tenantmeta.PodTenantLabel: "tenant-a",
			}

			_, relevant, err := remotePodFor(
				local,
				remoteNode,
				pod,
			)
			if err != nil {
				t.Fatal(err)
			}
			if relevant {
				t.Fatal("expected Pod to be irrelevant")
			}
		})
	}
}

func TestRemotePodForDefaultTenantAlwaysRelevant(t *testing.T) {
	local := testNode(t, "node-a")
	remoteNode := testNode(t, "node-b")
	pod := testPod(
		"application",
		"unlabelled",
		"node-b",
		"10.244.2.10",
	)

	got, relevant, err := remotePodFor(local, remoteNode, pod)
	if err != nil {
		t.Fatal(err)
	}
	if !relevant {
		t.Fatal("expected default Pod to be relevant")
	}
	if got.TenantID != tenantmeta.DefaultTenant {
		t.Fatalf(
			"tenant = %q, want %q",
			got.TenantID,
			tenantmeta.DefaultTenant,
		)
	}
}

func TestRemotePodForKubeSystemOverridesExplicitTenant(t *testing.T) {
	local := testNode(t, "node-a")
	remoteNode := testNode(t, "node-b")
	pod := testPod(
		"kube-system",
		"coredns",
		"node-b",
		"10.244.2.53",
	)
	pod.Labels = map[string]string{
		tenantmeta.PodTenantLabel: "tenant-a",
	}

	got, relevant, err := remotePodFor(local, remoteNode, pod)
	if err != nil {
		t.Fatal(err)
	}
	if !relevant {
		t.Fatal("expected kube-system Pod to be relevant")
	}
	if got.TenantID != tenantmeta.DefaultTenant {
		t.Fatalf(
			"tenant = %q, want %q",
			got.TenantID,
			tenantmeta.DefaultTenant,
		)
	}
}

func TestRemotePodForIgnoresLocalHostNetworkTerminalAndUnscheduled(t *testing.T) {
	local := testNode(t, "node-a", "tenant-a")
	remoteNode := testNode(t, "node-b", "tenant-a")

	tests := []struct {
		name string
		edit func(*corev1.Pod)
	}{
		{
			name: "local",
			edit: func(p *corev1.Pod) {
				p.Spec.NodeName = "node-a"
			},
		},
		{
			name: "host network",
			edit: func(p *corev1.Pod) {
				p.Spec.HostNetwork = true
			},
		},
		{
			name: "succeeded",
			edit: func(p *corev1.Pod) {
				p.Status.Phase = corev1.PodSucceeded
			},
		},
		{
			name: "failed",
			edit: func(p *corev1.Pod) {
				p.Status.Phase = corev1.PodFailed
			},
		},
		{
			name: "unscheduled",
			edit: func(p *corev1.Pod) {
				p.Spec.NodeName = ""
			},
		},
		{
			name: "no IP",
			edit: func(p *corev1.Pod) {
				p.Status.PodIP = ""
				p.Status.PodIPs = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := testPod(
				"app",
				"pod-a",
				"node-b",
				"10.244.2.10",
			)
			pod.Labels = map[string]string{
				tenantmeta.PodTenantLabel: "tenant-a",
			}
			tt.edit(pod)

			_, relevant, err := remotePodFor(
				local,
				remoteNode,
				pod,
			)
			if err != nil {
				t.Fatal(err)
			}
			if relevant {
				t.Fatal("expected Pod to be ignored")
			}
		})
	}
}

func TestPodIPv4SelectsIPv4FromDualStackStatus(t *testing.T) {
	pod := testPod(
		"app",
		"pod-a",
		"node-b",
		"2001:db8::10",
	)
	pod.Status.PodIPs = []corev1.PodIP{
		{IP: "2001:db8::10"},
		{IP: "10.244.2.10"},
	}

	got, ok, err := podIPv4(pod)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected IPv4 address")
	}
	if got != netip.MustParseAddr("10.244.2.10") {
		t.Fatalf("IP = %s, want 10.244.2.10", got)
	}
}

func TestReconcileAllBuildsDesiredRemoteSet(t *testing.T) {
	local := testNode(t, "node-a", "tenant-a")
	remoteA := testNode(t, "node-b", "tenant-a")
	remoteB := testNode(t, "node-c", "tenant-b")

	pods := []*corev1.Pod{
		testTenantPod(
			"app",
			"tenant-a-pod",
			"node-b",
			"10.244.2.10",
			"tenant-a",
		),
		testTenantPod(
			"app",
			"tenant-b-pod",
			"node-c",
			"10.244.3.10",
			"tenant-b",
		),
		testPod(
			"kube-system",
			"coredns",
			"node-c",
			"10.244.3.53",
		),
		testTenantPod(
			"app",
			"local",
			"node-a",
			"10.244.1.10",
			"tenant-a",
		),
	}

	w, fake := testWatcher(
		t,
		local,
		[]*corev1.Node{remoteA, remoteB},
		pods,
	)

	if err := w.reconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	if fake.reconcileCalls != 1 {
		t.Fatalf(
			"reconcile calls = %d, want 1",
			fake.reconcileCalls,
		)
	}
	if len(fake.lastDesired) != 2 {
		t.Fatalf(
			"desired Pods = %+v, want 2",
			fake.lastDesired,
		)
	}

	byIP := make(map[netip.Addr]ebpfmanager.RemotePod)
	for _, pod := range fake.lastDesired {
		byIP[pod.IP] = pod
	}

	if byIP[netip.MustParseAddr("10.244.2.10")].TenantID != "tenant-a" {
		t.Fatal("tenant-a remote Pod missing")
	}
	if byIP[netip.MustParseAddr("10.244.3.53")].TenantID != tenantmeta.DefaultTenant {
		t.Fatal("default/kube-system remote Pod missing")
	}
}

func testNode(
	t *testing.T,
	name string,
	tenants ...string,
) *corev1.Node {
	t.Helper()

	node := &corev1.Node{}
	node.Name = name
	node.Labels = make(map[string]string)

	for _, tenant := range tenants {
		key, err := tenantmeta.NodeTenantLabel(tenant)
		if err != nil {
			t.Fatal(err)
		}
		node.Labels[key] = "true"
	}

	return node
}

func testPod(
	namespace string,
	name string,
	nodeName string,
	ip string,
) *corev1.Pod {
	pod := &corev1.Pod{}
	pod.Namespace = namespace
	pod.Name = name
	pod.UID = types.UID(namespace + "-" + name)
	pod.Spec.NodeName = nodeName
	pod.Status.PodIP = ip
	if ip != "" {
		pod.Status.PodIPs = []corev1.PodIP{{IP: ip}}
	}
	return pod
}

func testTenantPod(
	namespace string,
	name string,
	nodeName string,
	ip string,
	tenant string,
) *corev1.Pod {
	pod := testPod(namespace, name, nodeName, ip)
	pod.Labels = map[string]string{
		tenantmeta.PodTenantLabel: tenant,
	}
	return pod
}
