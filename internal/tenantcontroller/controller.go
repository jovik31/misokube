package tenantcontroller

import (
	"context"
	"fmt"
	"time"

	misokubeclient "github/misokube/pkg/generated/clientset/versioned"
	misokubelisters "github/misokube/pkg/generated/listers/misokube.com/v1"

	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

const (
	tenantFinalizer   = "misokube.com/tenant-finalizer"
	blockedRetryDelay = 10 * time.Second
)

// Controller reconciles Tenant resources into Kubernetes Node tenant labels.
type Controller struct {
	logger klog.Logger

	misokube misokubeclient.Interface
	kube     kubernetes.Interface
	metrics  metricsclient.Interface
	pods     podReader

	tenantInformer cache.SharedIndexInformer
	tenantLister   misokubelisters.TenantLister

	nodeInformer cache.SharedIndexInformer
	nodeLister   corelisters.NodeLister

	queue workqueue.TypedRateLimitingInterface[string]
}

func New(
	logger klog.Logger,
	misokubeClient misokubeclient.Interface,
	kubeClient kubernetes.Interface,
	metricsClient metricsclient.Interface,
	tenantInformer cache.SharedIndexInformer,
	tenantLister misokubelisters.TenantLister,
	nodeInformer cache.SharedIndexInformer,
	nodeLister corelisters.NodeLister,
) *Controller {
	c := &Controller{
		logger:         logger.WithName("tenant-controller"),
		misokube:       misokubeClient,
		kube:           kubeClient,
		metrics:        metricsClient,
		pods:           newKubePodReader(kubeClient),
		tenantInformer: tenantInformer,
		tenantLister:   tenantLister,
		nodeInformer:   nodeInformer,
		nodeLister:     nodeLister,
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
