// +kubebuilder:rbac:groups=setera.com,resources=tenants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=setera.com,resources=tenants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=setera.com,resources=nodeStores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=setera.com,resources=nodeStores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

package orchestrator

import (

	//std
	"context"
	"fmt"
	"time"

	// internal packages
	seterav1clientset "github/setera/pkg/generated/clientset/versioned"
	seterav1Factory "github/setera/pkg/generated/informers/externalversions"
	seterav1 "github/setera/pkg/generated/listers/setera.com/v1"
	"github/setera/pkg/operator"

	//k8s client-go
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	//k8s-api
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

type OrchOperator struct {
	Base           *operator.BaseOperator
	TenantLister   seterav1.TenantLister
	TenantInformer cache.SharedIndexInformer
}

func NewOrchestratorOperator(
	ctx context.Context,
	name string,
	seterav1Clientset seterav1clientset.Interface,
	kubeClientset kubernetes.Interface) *OrchOperator {

	// create setera informer factory, informers and listers
	factory := seterav1Factory.NewSharedInformerFactory(seterav1Clientset, 30*time.Second)
	tenantInformer := factory.Setera().V1().Tenants().Informer()
	tenantLister := factory.Setera().V1().Tenants().Lister()

	//build orchestrator operator handler
	orchOperator := &OrchOperator{
		TenantLister:   tenantLister,
		TenantInformer: tenantInformer,
	}

	//inject informers and listers to base operator
	base := operator.NewBaseOperator(ctx, name, seterav1Clientset, kubeClientset, factory, orchOperator)
	orchOperator.Base = base

	return orchOperator
}

func (o *OrchOperator) RegisterEventHandler() {

	o.TenantInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    o.addTenantHandler,
		UpdateFunc: o.updateTenantHandler,
		DeleteFunc: o.deleteTenantHandler,
	})

}

func (o *OrchOperator) Process() bool {

	// operator must be initialized
	if o.Base == nil {
		panic("o.Base is nil in Process()")
	}

	// process the workqueue
	wrappedKey, shutdown := o.Base.Workqueue.Get()
	if shutdown {
		o.Base.Logger.Info("Workqueue shutdown, stopping processing")
		return false
	}

	// mark the key as done
	defer o.Base.Workqueue.Done(wrappedKey)

	// parse the wrapped key to get the event and key
	event, key := parseQueuedKey(wrappedKey)
	o.Base.Logger.WithValues("event", event, "key", key).Info("Processing event")

	// check if the key is valid
	if key == "" {
		o.Base.Logger.Info("Invalid key, skipping processing", "key", key)
		return true
	}

	// check if the event is unknown - short circuit the processing
	if event == UnknownEvent {
		o.Base.Logger.Info("Unknown event type, skipping processing", "key", key)
		o.Base.Workqueue.Forget(wrappedKey)
		return true
	}

	var err error

	switch event {
	case AddEvent:
		err = o.addTenant(key)

	case UpdateEvent:
		err = o.updateTenant(key)

	case DeleteEvent:
		err = o.deleteTenant(key)

	}
	if err != nil {
		// requeue the key with exponential backoff since the tenant could not be processed
		o.Base.Logger.Error(err, "failed to process tenant", "event", event, "key", key)
		utilruntime.HandleError(fmt.Errorf("error processing key %s: %w", key, err))
		o.Base.Workqueue.AddRateLimited(wrappedKey) // requeue the key with exponential backoff

	} else {
		// remove item from the workqueue since the tenant has been successfully processed
		o.Base.Logger.Info(fmt.Sprintf("successfully handled %s event", event), "key", key)
		// mark the key as processed
		o.Base.Workqueue.Forget(wrappedKey)
	}

	return true

}
