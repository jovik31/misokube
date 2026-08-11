package tenantcontroller

import (
	"context"
	"testing"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	seterafake "github/setera/pkg/generated/clientset/versioned/fake"
	seteralisters "github/setera/pkg/generated/listers/setera.com/v1"
	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

func TestReconcileInitialTenantAssignsNodesAndUpdatesStatus(t *testing.T) {
	ctx := context.Background()

	tenant := &seterav1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "tenant-a",
			Generation: 1,
		},
		Spec: seterav1.TenantSpec{
			Zones: 2,
		},
	}

	nodeA := readyNode("node-a")
	nodeB := readyNode("node-b")
	nodeC := readyNode("node-c")

	seteraClient := seterafake.NewSimpleClientset(tenant.DeepCopy())
	kubeClient := fake.NewSimpleClientset(
		nodeA.DeepCopy(),
		nodeB.DeepCopy(),
		nodeC.DeepCopy(),
	)

	tenantIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if err := tenantIndexer.Add(tenant.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	nodeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, node := range []*corev1.Node{nodeA, nodeB, nodeC} {
		if err := nodeIndexer.Add(node.DeepCopy()); err != nil {
			t.Fatal(err)
		}
	}

	controller := &Controller{
		logger:       klog.Background(),
		setera:       seteraClient,
		kube:         kubeClient,
		pods:         newKubePodReader(kubeClient),
		tenantLister: seteralisters.NewTenantLister(tenantIndexer),
		nodeLister:   corelisters.NewNodeLister(nodeIndexer),
		queue: workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[string](),
		),
	}
	defer controller.queue.ShutDown()

	if err := controller.reconcileKey(ctx, tenant.Name); err != nil {
		t.Fatalf("reconcile tenant: %v", err)
	}

	current, err := seteraClient.
		SeteraV1().
		Tenants().
		Get(ctx, tenant.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}

	if !containsString(current.Finalizers, tenantFinalizer) {
		t.Fatalf(
			"tenant finalizers = %v, want %q",
			current.Finalizers,
			tenantFinalizer,
		)
	}

	if len(current.Status.AssignedNodes) != 2 {
		t.Fatalf(
			"assigned nodes = %v, want 2 nodes",
			current.Status.AssignedNodes,
		)
	}

	if len(current.Status.Conditions) == 0 {
		t.Fatal("tenant status has no conditions")
	}

	condition := current.Status.Conditions[0]
	if condition.Type != assignedCondition ||
		condition.Status != metav1.ConditionTrue ||
		condition.Reason != "Assigned" {
		t.Fatalf(
			"unexpected Assigned condition: %#v",
			condition,
		)
	}

	labelKey, err := tenantmeta.NodeTenantLabel(tenant.Name)
	if err != nil {
		t.Fatal(err)
	}

	labeled := 0
	for _, nodeName := range []string{"node-a", "node-b", "node-c"} {
		node, err := kubeClient.
			CoreV1().
			Nodes().
			Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get node %s: %v", nodeName, err)
		}

		if node.Labels[labelKey] == "true" {
			labeled++
		}
	}

	if labeled != 2 {
		t.Fatalf("labeled nodes = %d, want 2", labeled)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}
