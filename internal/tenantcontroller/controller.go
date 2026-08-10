package tenantcontroller

import (
	"context"
	"fmt"

	seteraclient "github/setera/pkg/generated/clientset/versioned"
	seteralisters "github/setera/pkg/generated/listers/setera.com/v1"

	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const tenantFinalizer = "setera.com/tenant-finalizer"

// Controller reconciles Tenant resources into Kubernetes Node tenant labels.
type Controller struct {
	logger klog.Logger

	setera seteraclient.Interface
	kube   kubernetes.Interface

	tenantInformer cache.SharedIndexInformer
	tenantLister   seteralisters.TenantLister

	nodeInformer cache.SharedIndexInformer
	nodeLister   corelisters.NodeLister

	podInformer cache.SharedIndexInformer
	podLister   corelisters.PodLister

	queue workqueue.TypedRateLimitingInterface[string]
}

func New(
	logger klog.Logger,
	seteraClient seteraclient.Interface,
	kubeClient kubernetes.Interface,
	tenantInformer cache.SharedIndexInformer,
	tenantLister seteralisters.TenantLister,
	nodeInformer cache.SharedIndexInformer,
	nodeLister corelisters.NodeLister,
	podInformer cache.SharedIndexInformer,
	podLister corelisters.PodLister,
) *Controller {
	c := &Controller{
		logger:         logger.WithName("tenant-controller"),
		setera:         seteraClient,
		kube:           kubeClient,
		tenantInformer: tenantInformer,
		tenantLister:   tenantLister,
		nodeInformer:   nodeInformer,
		nodeLister:     nodeLister,
		podInformer:    podInformer,
		podLister:      podLister,
		queue: workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[string](),
		),
	}

	c.registerEventHandlers()
	return c
}

// Run waits for informer caches and processes Tenant reconciliation requests.
func (c *Controller) Run(ctx context.Context) error {
	defer c.queue.ShutDown()

	if ok := cache.WaitForCacheSync(
		ctx.Done(),
		c.tenantInformer.HasSynced,
		c.nodeInformer.HasSynced,
		c.podInformer.HasSynced,
	); !ok {
		return fmt.Errorf("tenant controller: informer cache sync failed")
	}

	c.logger.Info("starting")
	defer c.logger.Info("stopped")

	go c.worker(ctx)

	<-ctx.Done()
	return nil
}

func (c *Controller) worker(ctx context.Context) {
	for c.processNext(ctx) {
	}
}

func (c *Controller) processNext(ctx context.Context) bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(key)

	if err := c.reconcileKey(ctx, key); err != nil {
		c.logger.Error(err, "reconcile failed", "key", key)
		c.queue.AddRateLimited(key)
		return true
	}

	c.queue.Forget(key)
	return true
}
