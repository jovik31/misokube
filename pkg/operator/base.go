package operator

import (

	//std
	"context"
	"fmt"
	"time"

	// client-go
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	//generated clientset
	seterav1clientset "github/setera/pkg/generated/clientset/versioned"
	"github/setera/pkg/generated/clientset/versioned/scheme"
	seterav1Factory "github/setera/pkg/generated/informers/externalversions"

	// logging
	"k8s.io/klog/v2"

	//k8s api
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
)

type EventType string

const (
	AddEvent     EventType = "Add"
	UpdateEvent  EventType = "Update"
	DeleteEvent  EventType = "Delete"
	UnknownEvent EventType = "Unknown"
)

type WorkHandler interface {
	RegisterEventHandler()
	Process() bool
}

type BaseOperator struct {
	Name              string
	Seterav1Clientset seterav1clientset.Interface
	KubeClientset     kubernetes.Interface
	factory           seterav1Factory.SharedInformerFactory        // factory for Setera resources
	Workqueue         workqueue.TypedRateLimitingInterface[string] // workqueue to store the tenant objects with their namespace/name as the key
	Handler           WorkHandler
	//informers          []cache.SharedIndexInformer // informers for tenant and nodestore
	Recorder record.EventRecorder
	Logger   klog.Logger
}

func NewBaseOperator(

	ctx context.Context,
	name string,
	seterav1Clientset seterav1clientset.Interface,
	kubeClientset kubernetes.Interface,
	factory seterav1Factory.SharedInformerFactory, // factory for Setera resources
	handler WorkHandler) *BaseOperator {

	// create logger
	baseLogger := klog.FromContext(ctx).WithName(name).WithValues("component", "operator")

	//create event boradcaster
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(baseLogger.V(4).Info)
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: name})
	baseLogger.V(4).Info("Event broadcaster started", "component", name)

	return &BaseOperator{
		Name:              name,
		Seterav1Clientset: seterav1Clientset,
		KubeClientset:     kubeClientset,
		factory:           factory,
		Workqueue:         workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
		Handler:           handler, // Handler will be set later
		Recorder:          recorder,
		Logger:            baseLogger,
	}
}

func (b *BaseOperator) Run(ctx context.Context) error {

	// avoid panic on the operator
	defer utilruntime.HandleCrash()

	// make sure the work queue is shutdown, triggering workers to stop
	defer b.Workqueue.ShutDown()

	// register the event handlers
	b.Handler.RegisterEventHandler()
	b.Logger.Info("Registered event handlers", "name", b.Name)

	// start the informers
	b.factory.Start(ctx.Done())

	// wait for the caches to sync
	informerStatus := b.factory.WaitForCacheSync(ctx.Done())

	/// check if all informers are synced
	for resource, synced := range informerStatus {
		if !synced {
			b.Logger.Error(nil, "Failed to sync informer cache", "resource", resource)
			return fmt.Errorf("[CACHE SYNC][ERROR] - informer cache failed to sync for resource: %v", resource) // or return an error if you want to stop the operator
		}
		b.Logger.Info("Informer cache synced", "resource", resource)
	}

	b.Logger.Info("All informers synced successfully", "name", b.Name)

	// start the work handler
	go func() {
		defer utilruntime.HandleCrash()
		wait.UntilWithContext(ctx, b.worker, time.Second)
		b.Logger.Info("Work handler stopped", "name", b.Name)
	}()

	b.Logger.Info("Started work handler", "name", b.Name)
	<-ctx.Done()
	b.Logger.Info("Shutting down operator", "name", b.Name)

	return nil
}

func (b *BaseOperator) worker(ctx context.Context) {
	for b.Handler.Process() {
	}
}
