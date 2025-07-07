package orchestrator

/*
import (

	//std
	"context"
	"fmt"
	"time"

	// api types

	//generated clientset and tenantLister
	seterav1clientset "github/setera/pkg/generated/clientset/versioned"
	"github/setera/pkg/generated/clientset/versioned/scheme"
	informers "github/setera/pkg/generated/informers/externalversions/setera.com/v1"
	listers "github/setera/pkg/generated/listers/setera.com/v1"

	// client-go
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	//k8s api
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sInformers "k8s.io/client-go/informers/core/v1"
	k8sListers "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
)

type Orchestrator struct {
	// contains filtered or unexported fields

	//clientset for custom resource tenant and nodestore
	seterav1Clientset seterav1clientset.Interface

	// clientset for k8s resources
	kubeClientset kubernetes.Interface

	tenantSynced cache.InformerSynced

	// tenantLister is able to list/get tenant objects from the k8s cluster
	tenantInformer cache.SharedIndexInformer

	// tenantLister is able to list/get tenant objects from the k8s cluster
	tenantLister listers.TenantLister

	// nodeStore informer is able to list/get nodestore objects from the k8s cluster
	nodeStoreInformer cache.SharedIndexInformer

	// NodeStoreLister is able to list/get nodestore objects from the k8s cluster
	nodeStoreLister listers.NodeStoreLister

	// node informer is able to list/get node objects from the k8s cluster
	nodeInformer cache.SharedIndexInformer

	// node lister is able to list/get node objects from the cache
	nodeLister k8sListers.NodeLister

	// workqueue to store the tenant objects
	workqueue workqueue.TypedRateLimitingInterface[string] // workqueue to store the tenant objects with their namespace/name as the key

	//event recorder to record events
	recorder record.EventRecorder

	//logger sink
	logger klog.Logger
}

func NewOrchestrator(

	// context
	ctx context.Context,

	// clientset for custom resource tenant and nodestore
	seterav1Clientset seterav1clientset.Interface,

	// clientset for k8s resources
	kubeClientset kubernetes.Interface,

	// tenant informer
	tenantInformer informers.TenantInformer,

	// nodestore informer
	nodestoreInformer informers.NodeStoreInformer,

	//node informer
	nodeInformer k8sInformers.NodeInformer) *Orchestrator {

	logger := klog.FromContext(ctx)

	// create a new event broadcaster
	utilruntime.Must(scheme.AddToScheme(scheme.Scheme))
	logger.V(4).Info("Creating event broadcaster")

	// create an event broadcaster
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "setera-orchestrator"})
	logger.V(4).Info("Event broadcaster created")

	o := &Orchestrator{
		seterav1Clientset: seterav1Clientset,
		kubeClientset:     kubeClientset,
		tenantInformer:    tenantInformer.Informer(),
		tenantLister:      tenantInformer.Lister(),
		nodeStoreInformer: nodestoreInformer.Informer(),
		nodeStoreLister:   nodestoreInformer.Lister(),
		nodeInformer:      nodeInformer.Informer(),
		nodeLister:        nodeInformer.Lister(),
		workqueue:         workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
		logger:            logger,
		recorder:          recorder,
	}

	// add event handlers for tenant
	o.tenantInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    o.addTenantHandler,
			UpdateFunc: o.updateTenantHandler,
			DeleteFunc: o.deleteTenantHandler,
		},
	)

	return o
}

func (o *Orchestrator) Run(ctx context.Context) error {

	// avoids panicking the orchestrator
	defer utilruntime.HandleCrash()

	//make sure the work queue is shutdown, this triggers the workers to stop
	defer o.workqueue.ShutDown()

	o.logger.Info("Starting tenant orchestrator")
	if ok := cache.WaitForCacheSync(ctx.Done(), o.tenantSynced); !ok {
		o.logger.Info("Failed to wait for caches to sync")

		return fmt.Errorf("failed to wait for caches to sync")

	}

	go wait.UntilWithContext(ctx, o.worker, time.Second)
	o.logger.Info("Started orchestrator worker")
	<-ctx.Done()
	o.logger.Info("Shutting down orchestrator workers")

	return nil
}

func (o *Orchestrator) worker(ctx context.Context) {
	for o.processNextWorkItem() {
	}
}

// ProcessNextWorkItem processes the next item in the work queue
func (o *Orchestrator) processNextWorkItem() bool {

	// get tenant key from the workqueue
	wrappedKey, shutdown := o.workqueue.Get()
	if shutdown {
		o.logger.Info("queue is shutdown")
		return false
	}

	// mark item as done
	defer o.workqueue.Done(wrappedKey)

	event, key := parseQueuedKey(wrappedKey)

	switch event {

	case AddEvent:
		err := o.addTenant(key)
		if err != nil {
			// requeue the tenant with exponential backoff since the tenant could not be processed
			o.workqueue.AddRateLimited(wrappedKey)

		} else {
			// remove item from the workqueue since the tenant has been successfully processed
			o.workqueue.Forget(wrappedKey)
		}

	case UpdateEvent:
		err := o.updateTenant(key)
		if err == nil {
			o.workqueue.Forget(wrappedKey)

		}
	case DeleteEvent:
		err := o.deleteTenant(key)
		if err == nil {
			o.workqueue.Forget(wrappedKey)

		}
	case UnknownEvent:
		o.logger.Info("Unknown event", "key", key)
		o.workqueue.Forget(wrappedKey)
	}

	utilruntime.HandleError(fmt.Errorf("error processing key %s", key))
	o.workqueue.Forget(wrappedKey)
	o.logger.Info("Successfully synced tenant", "key", key)

	return true
}*/
