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
	"github/setera/internal"
	v1 "github/setera/pkg/api/setera.com/v1"

	seterav1clientset "github/setera/pkg/generated/clientset/versioned"
	seterav1Factory "github/setera/pkg/generated/informers/externalversions"
	seterav1 "github/setera/pkg/generated/listers/setera.com/v1"
	"github/setera/pkg/nodescore"
	"github/setera/pkg/operator"

	//k8s client-go
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	//k8s-api
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

type TenantOperator struct {
	Base           *operator.BaseOperator
	TenantLister   seterav1.TenantLister
	TenantInformer cache.SharedIndexInformer

	// To watch and trigger tenant updates
	NodestoreLister   seterav1.NodeStoreLister
	NodestoreInformer cache.SharedIndexInformer

	// NodeScoreCache to store the scores of nodes
	ScoreCache *nodescore.NodeScoreCache
}

func NewTenantOperator(
	ctx context.Context,
	name string,
	seterav1Clientset seterav1clientset.Interface,
	kubeClientset kubernetes.Interface, nodeScoreCache *nodescore.NodeScoreCache) *TenantOperator {

	// create setera informer factory, informers and listers
	factory := seterav1Factory.NewSharedInformerFactory(seterav1Clientset, 30*time.Second)
	tenantInformer := factory.Setera().V1().Tenants().Informer()
	tenantLister := factory.Setera().V1().Tenants().Lister()

	nodestoreInformer := factory.Setera().V1().NodeStores().Informer()
	nodestoreLister := factory.Setera().V1().NodeStores().Lister()

	//build orchestrator operator handler
	tenantOperator := &TenantOperator{
		TenantLister:      tenantLister,
		TenantInformer:    tenantInformer,
		NodestoreLister:   nodestoreLister,
		NodestoreInformer: nodestoreInformer,
		ScoreCache:        nodeScoreCache,
	}

	//inject informers and listers to base operator
	base := operator.NewBaseOperator(ctx, name, seterav1Clientset, kubeClientset, factory, tenantOperator)
	tenantOperator.Base = base

	return tenantOperator
}

func (t *TenantOperator) RegisterEventHandler() {

	t.TenantInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    t.addTenantHandler,
		UpdateFunc: t.updateTenantHandler,
		DeleteFunc: t.deleteTenantHandler,
	})

	t.NodestoreInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    nil,
		UpdateFunc: t.updateTenantFromNodestoreHandler,
		DeleteFunc: t.deleteTenantFromNodestoreHandler,
	})
}

func (t *TenantOperator) Process() bool {

	// operator must be initialized
	if t.Base == nil {
		panic("o.Base is nil in Process()")
	}

	// process the workqueue
	wrappedKey, shutdown := t.Base.Workqueue.Get()
	if shutdown {
		t.Base.Logger.Info("Workqueue shutdown, stopping processing")
		return false
	}

	// mark the key as done
	defer t.Base.Workqueue.Done(wrappedKey)

	// parse the wrapped key to get the event and key
	event, key := operator.ParseQueuedKey(wrappedKey)

	t.Base.Logger.WithValues("event", event, "key", key).Info("Processing event")

	// check if the key is valid
	if key == "" {
		t.Base.Logger.Info("Invalid key, skipping processing", "key", key)
		return true
	}

	// check if the event is unknown - short circuit the processing
	if event == operator.UnknownEvent {
		t.Base.Logger.Info("Unknown event type, skipping processing", "key", key)
		t.Base.Workqueue.Forget(wrappedKey)
		return true
	}

	var err error

	switch event {

	case operator.AddEvent:
		err = t.addTenant(key)

	case operator.UpdateEvent:
		err = t.updateTenant(key)

	case operator.DeleteEvent:
		err = t.deleteTenant(key)

	}

	if err != nil {
		// requeue the key with exponential backoff since the tenant could not be processed
		t.Base.Logger.Error(err, "failed to process tenant", "event", event, "key", key)
		utilruntime.HandleError(fmt.Errorf("error processing key %s: %w", key, err))
		t.Base.Workqueue.AddRateLimited(wrappedKey) // requeue the key with exponential backoff

	} else {
		// remove item from the workqueue since the tenant has been successfully processed
		t.Base.Logger.Info(fmt.Sprintf("successfully handled %s event", event), "key", key)
		// mark the key as processed
		t.Base.Workqueue.Forget(wrappedKey)
	}

	return true

}

// checks if tenant has the finalizer
func (t *TenantOperator) checkTenantFinalizer(tenant *v1.Tenant) bool {

	for _, f := range tenant.Finalizers {
		if f == internal.TenantFinalizer {
			return true // finalizer found
		}
	}
	return false // finalizer not found

}

func (t *TenantOperator) updateTenantObject(tenant *v1.Tenant) error {

	// update the tenant object in the k8s cluster
	_, err := t.Base.Seterav1Clientset.SeteraV1().Tenants(tenant.Namespace).Update(context.TODO(), tenant, metav1.UpdateOptions{})
	if err != nil {
		t.Base.Logger.Error(err, "Error in updating tenant object in the k8s cluster", tenant.Name)
		return err
	}
	return nil
}
