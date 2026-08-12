package cniserver

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github/setera/internal/podnetwork"
	"github/setera/pkg/tenantmeta"
	"github/setera/pkg/wire"
)

func TestHandleAddUsesPodMetadata(t *testing.T) {
	pods := newPodLister(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod-a",
			UID:       "pod-uid-a",
			Labels: map[string]string{
				tenantmeta.PodTenantLabel: "tenant-a",
			},
		},
	})

	podNetwork := &fakePodNetwork{
		addResult: podnetwork.Result{
			IP:              netip.MustParseAddr("10.244.0.10"),
			HostVethName:    "veth1234",
			HostVethIfIndex: 42,
		},
	}

	server := newTestServer(t, pods, podNetwork)

	response := server.handleRequest(
		context.Background(),
		&wire.Request{
			Cmd:          wire.CmdADD,
			CNIVersion:   "1.1.0",
			ContainerID:  "container-a",
			NetNS:        "/var/run/netns/pod-a",
			IfName:       "eth0",
			PodNamespace: "default",
			PodName:      "pod-a",
			PodUID:       "pod-uid-a",
		},
	)

	if !response.OK {
		t.Fatalf("ADD failed: %s", response.Message)
	}

	if podNetwork.addCalls != 1 {
		t.Fatalf(
			"got %d AddPod calls, want 1",
			podNetwork.addCalls,
		)
	}

	want := podnetwork.Request{
		ContainerID: "container-a",
		NetNS:       "/var/run/netns/pod-a",
		IfName:      "eth0",
		PodUID:      "pod-uid-a",
		TenantID:    "tenant-a",
	}

	if podNetwork.addReq != want {
		t.Fatalf(
			"got request %+v, want %+v",
			podNetwork.addReq,
			want,
		)
	}

	if len(response.Result) == 0 {
		t.Fatal("ADD response has no CNI result")
	}
}

func TestHandleAddRejectsPodUIDMismatch(t *testing.T) {
	pods := newPodLister(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod-a",
			UID:       "current-uid",
			Labels: map[string]string{
				tenantmeta.PodTenantLabel: "tenant-a",
			},
		},
	})

	podNetwork := &fakePodNetwork{}
	server := newTestServer(t, pods, podNetwork)

	response := server.handleRequest(
		context.Background(),
		&wire.Request{
			Cmd:          wire.CmdADD,
			ContainerID:  "container-a",
			NetNS:        "/var/run/netns/pod-a",
			IfName:       "eth0",
			PodNamespace: "default",
			PodName:      "pod-a",
			PodUID:       "stale-uid",
		},
	)

	if response.OK {
		t.Fatal("expected ADD failure")
	}

	if podNetwork.addCalls != 0 {
		t.Fatalf(
			"got %d AddPod calls, want 0",
			podNetwork.addCalls,
		)
	}
}

func TestHandleAddUsesDefaultTenantForUnlabeledPod(
	t *testing.T,
) {
	pods := newPodLister(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod-a",
			UID:       "pod-uid-a",
		},
	})

	podNetwork := &fakePodNetwork{
		addResult: podnetwork.Result{
			IP:              netip.MustParseAddr("10.244.0.10"),
			HostVethName:    "veth1234",
			HostVethIfIndex: 42,
		},
	}

	server := newTestServer(t, pods, podNetwork)

	response := server.handleRequest(
		context.Background(),
		&wire.Request{
			Cmd:          wire.CmdADD,
			CNIVersion:   "1.1.0",
			ContainerID:  "container-a",
			NetNS:        "/var/run/netns/pod-a",
			IfName:       "eth0",
			PodNamespace: "default",
			PodName:      "pod-a",
			PodUID:       "pod-uid-a",
		},
	)

	if !response.OK {
		t.Fatalf("ADD failed: %s", response.Message)
	}

	if podNetwork.addCalls != 1 {
		t.Fatalf(
			"got %d AddPod calls, want 1",
			podNetwork.addCalls,
		)
	}

	if podNetwork.addReq.TenantID != tenantmeta.DefaultTenant {
		t.Fatalf(
			"got tenant %q, want %q",
			podNetwork.addReq.TenantID,
			tenantmeta.DefaultTenant,
		)
	}
}

func TestHandleAddUsesDefaultTenantForKubeSystemPod(
	t *testing.T,
) {
	pods := newPodLister(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system",
			Name:      "coredns-a",
			UID:       "coredns-uid-a",
			Labels: map[string]string{
				tenantmeta.PodTenantLabel: "tenant-a",
			},
		},
	})

	podNetwork := &fakePodNetwork{
		addResult: podnetwork.Result{
			IP:              netip.MustParseAddr("10.244.0.53"),
			HostVethName:    "vethdns",
			HostVethIfIndex: 53,
		},
	}

	server := newTestServer(t, pods, podNetwork)

	response := server.handleRequest(
		context.Background(),
		&wire.Request{
			Cmd:          wire.CmdADD,
			CNIVersion:   "1.1.0",
			ContainerID:  "container-dns",
			NetNS:        "/var/run/netns/coredns-a",
			IfName:       "eth0",
			PodNamespace: "kube-system",
			PodName:      "coredns-a",
			PodUID:       "coredns-uid-a",
		},
	)

	if !response.OK {
		t.Fatalf("ADD failed: %s", response.Message)
	}

	if podNetwork.addCalls != 1 {
		t.Fatalf(
			"got %d AddPod calls, want 1",
			podNetwork.addCalls,
		)
	}

	if podNetwork.addReq.TenantID != tenantmeta.DefaultTenant {
		t.Fatalf(
			"got tenant %q, want %q",
			podNetwork.addReq.TenantID,
			tenantmeta.DefaultTenant,
		)
	}
}

func TestHandleAddRejectsTenantNotAssignedToLocalNode(
	t *testing.T,
) {
	pods := newPodLister(t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod-a",
			UID:       "pod-uid-a",
			Labels: map[string]string{
				tenantmeta.PodTenantLabel: "tenant-b",
			},
		},
	})

	podNetwork := &fakePodNetwork{}

	server, err := New(
		"/tmp/setera-test.sock",
		"node-a",
		pods,
		newNodeLister(
			t,
			nodeWithTenants(t, "node-a", "tenant-a"),
		),
		podNetwork,
		"1.1.0",
	)
	if err != nil {
		t.Fatal(err)
	}

	response := server.handleRequest(
		context.Background(),
		&wire.Request{
			Cmd:          wire.CmdADD,
			CNIVersion:   "1.1.0",
			ContainerID:  "container-a",
			NetNS:        "/var/run/netns/pod-a",
			IfName:       "eth0",
			PodNamespace: "default",
			PodName:      "pod-a",
			PodUID:       "pod-uid-a",
		},
	)

	if response.OK {
		t.Fatal("expected ADD failure")
	}

	if podNetwork.addCalls != 0 {
		t.Fatalf(
			"got %d AddPod calls, want 0",
			podNetwork.addCalls,
		)
	}
}

func TestHandleDelDoesNotNeedPodMetadata(t *testing.T) {
	podNetwork := &fakePodNetwork{}
	server := newTestServer(
		t,
		newPodLister(t),
		podNetwork,
	)

	response := server.handleRequest(
		context.Background(),
		&wire.Request{
			Cmd:         wire.CmdDEL,
			ContainerID: "container-a",
			IfName:      "eth0",
		},
	)

	if !response.OK {
		t.Fatalf("DEL failed: %s", response.Message)
	}

	if podNetwork.delCalls != 1 {
		t.Fatalf(
			"got %d DelPod calls, want 1",
			podNetwork.delCalls,
		)
	}

	want := podnetwork.Request{
		ContainerID: "container-a",
		IfName:      "eth0",
	}

	if podNetwork.delReq != want {
		t.Fatalf(
			"got request %+v, want %+v",
			podNetwork.delReq,
			want,
		)
	}
}

func TestHandleCheck(t *testing.T) {
	podNetwork := &fakePodNetwork{}
	server := newTestServer(
		t,
		newPodLister(t),
		podNetwork,
	)

	response := server.handleRequest(
		context.Background(),
		&wire.Request{
			Cmd:         wire.CmdCHECK,
			ContainerID: "container-a",
			NetNS:       "/var/run/netns/pod-a",
			IfName:      "eth0",
		},
	)

	if !response.OK {
		t.Fatalf("CHECK failed: %s", response.Message)
	}

	if podNetwork.checkCalls != 1 {
		t.Fatalf(
			"got %d CheckPod calls, want 1",
			podNetwork.checkCalls,
		)
	}
}

func TestHandlePodNetworkError(t *testing.T) {
	podNetwork := &fakePodNetwork{
		delErr: errors.New("delete failed"),
	}

	server := newTestServer(
		t,
		newPodLister(t),
		podNetwork,
	)

	response := server.handleRequest(
		context.Background(),
		&wire.Request{
			Cmd:         wire.CmdDEL,
			ContainerID: "container-a",
			IfName:      "eth0",
		},
	)

	if response.OK {
		t.Fatal("expected DEL failure")
	}
}

func newTestServer(
	t *testing.T,
	pods corev1listers.PodLister,
	podNetwork podNetwork,
) *Server {
	t.Helper()

	server, err := New(
		"/tmp/setera-test.sock",
		"node-a",
		pods,
		newNodeLister(
			t,
			nodeWithTenants(t, "node-a", "tenant-a"),
		),
		podNetwork,
		"1.1.0",
	)
	if err != nil {
		t.Fatal(err)
	}

	return server
}

func newPodLister(
	t *testing.T,
	pods ...*corev1.Pod,
) corev1listers.PodLister {
	t.Helper()

	indexer := cache.NewIndexer(
		cache.MetaNamespaceKeyFunc,
		cache.Indexers{
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		},
	)

	for _, pod := range pods {
		if err := indexer.Add(pod); err != nil {
			t.Fatal(err)
		}
	}

	return corev1listers.NewPodLister(indexer)
}

func newNodeLister(
	t *testing.T,
	nodes ...*corev1.Node,
) corev1listers.NodeLister {
	t.Helper()

	indexer := cache.NewIndexer(
		cache.MetaNamespaceKeyFunc,
		cache.Indexers{},
	)

	for _, node := range nodes {
		if err := indexer.Add(node); err != nil {
			t.Fatal(err)
		}
	}

	return corev1listers.NewNodeLister(indexer)
}

func nodeWithTenants(
	t *testing.T,
	name string,
	tenants ...string,
) *corev1.Node {
	t.Helper()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{},
		},
	}

	for _, tenant := range tenants {
		labelKey, err := tenantmeta.NodeTenantLabel(tenant)
		if err != nil {
			t.Fatal(err)
		}

		node.Labels[labelKey] = "true"
	}

	return node
}

type fakePodNetwork struct {
	addReq    podnetwork.Request
	addResult podnetwork.Result
	addErr    error
	addCalls  int

	delReq   podnetwork.Request
	delErr   error
	delCalls int

	checkReq   podnetwork.Request
	checkErr   error
	checkCalls int
}

func (f *fakePodNetwork) AddPod(
	_ context.Context,
	req podnetwork.Request,
) (podnetwork.Result, error) {
	f.addCalls++
	f.addReq = req

	return f.addResult, f.addErr
}

func (f *fakePodNetwork) DelPod(
	_ context.Context,
	req podnetwork.Request,
) error {
	f.delCalls++
	f.delReq = req

	return f.delErr
}

func (f *fakePodNetwork) CheckPod(
	_ context.Context,
	req podnetwork.Request,
) error {
	f.checkCalls++
	f.checkReq = req

	return f.checkErr
}
