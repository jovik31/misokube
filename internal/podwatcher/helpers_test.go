package podwatcher

import (
	"context"
	"net/netip"
	"testing"

	"github/setera/internal/ebpfmanager"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type fakeRemoteDatapath struct {
	reconcileCalls int
	lastDesired    []ebpfmanager.RemotePod

	upserts []ebpfmanager.RemotePod
	deletes []deleteCall
}

type deleteCall struct {
	IP     netip.Addr
	PodUID string
}

func (f *fakeRemoteDatapath) UpsertRemotePod(
	_ context.Context,
	pod ebpfmanager.RemotePod,
) error {
	f.upserts = append(f.upserts, pod)
	return nil
}

func (f *fakeRemoteDatapath) DeleteRemotePod(
	_ context.Context,
	ip netip.Addr,
	podUID string,
) error {
	f.deletes = append(f.deletes, deleteCall{
		IP:     ip,
		PodUID: podUID,
	})
	return nil
}

func (f *fakeRemoteDatapath) ReconcileRemotePods(
	_ context.Context,
	pods []ebpfmanager.RemotePod,
) error {
	f.reconcileCalls++
	f.lastDesired = append(
		[]ebpfmanager.RemotePod(nil),
		pods...,
	)
	return nil
}

func testWatcher(
	t *testing.T,
	local *corev1.Node,
	remoteNodes []*corev1.Node,
	pods []*corev1.Pod,
) (*Watcher, *fakeRemoteDatapath) {
	t.Helper()

	podIndexer := cache.NewIndexer(
		cache.MetaNamespaceKeyFunc,
		cache.Indexers{
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		},
	)
	nodeIndexer := cache.NewIndexer(
		cache.MetaNamespaceKeyFunc,
		cache.Indexers{},
	)

	if err := nodeIndexer.Add(local); err != nil {
		t.Fatal(err)
	}
	for _, node := range remoteNodes {
		if err := nodeIndexer.Add(node); err != nil {
			t.Fatal(err)
		}
	}
	for _, pod := range pods {
		if err := podIndexer.Add(pod); err != nil {
			t.Fatal(err)
		}
	}

	fake := &fakeRemoteDatapath{}

	w := &Watcher{
		logger:        klog.Background(),
		localNodeName: local.Name,
		datapath:      fake,
		podLister:     v1.NewPodLister(podIndexer),
		nodeLister:    v1.NewNodeLister(nodeIndexer),
		known:         make(map[string]ebpfmanager.RemotePod),
	}

	// Sanity-check listers used by the test.
	if _, err := w.nodeLister.Get(local.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := w.podLister.List(labels.Everything()); err != nil {
		t.Fatal(err)
	}

	return w, fake
}
