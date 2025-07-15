// +kubebuilder:rbac:groups=setera.com,resources=tenants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=setera.com,resources=tenants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=setera.com,resources=nodeStores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=setera.com,resources=nodeStores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete

package daemon

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

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	//k8s-api
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

type NodeStoreOperator struct {
	Base *operator.BaseOperator

	// watch and trigger nodestore events
	NodeStoreLister   seterav1.NodeStoreLister
	NodeStoreInformer cache.SharedIndexInformer

	// watch and trigger tenant updates
	TenantLister   seterav1.TenantLister
	TenantInformer cache.SharedIndexInformer

	// watch and trigger pods
	PodLister   corev1.PodLister
	PodInformer cache.SharedIndexInformer
}

func NewNodeStoreOperator(
	ctx context.Context,
	name string,
	seterav1Clientset seterav1clientset.Interface,
	kubeClientset kubernetes.Interface) *NodeStoreOperator {

	// create setera informer factory, informers and listers
	factorySetera := seterav1Factory.NewSharedInformerFactory(seterav1Clientset, 30*time.Second)
	factoryCore := informers.NewSharedInformerFactory(kubeClientset, 30*time.Second)
	nodeStoreInformer := factorySetera.Setera().V1().NodeStores().Informer()
	nodeStoreLister := factorySetera.Setera().V1().NodeStores().Lister()

	tenantInformer := factorySetera.Setera().V1().Tenants().Informer()
	tenantLister := factorySetera.Setera().V1().Tenants().Lister()

	podInformer := factoryCore.Core().V1().Pods().Informer()
	podLister := factoryCore.Core().V1().Pods().Lister()

	// build orchestrator operator handler
	nodeStoreOperator := &NodeStoreOperator{
		TenantLister:      tenantLister,
		TenantInformer:    tenantInformer,
		NodeStoreLister:   nodeStoreLister,
		NodeStoreInformer: nodeStoreInformer,
		PodLister:         podLister,
		PodInformer:       podInformer,
	}

	//inject informers and lister to base operator
	base := operator.NewBaseOperator(ctx, name, seterav1Clientset, kubeClientset, factorySetera, nodeStoreOperator)
	nodeStoreOperator.Base = base

	return nodeStoreOperator

}

func (n *NodeStoreOperator) RegisterEventHandler() {

	n.NodeStoreInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{

		AddFunc:    n.addNodeStoreHandler,
		UpdateFunc: n.updateNodeStoreHandler,
		DeleteFunc: n.deleteNodeStoreHandler,
	})

	n.TenantInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    nil,
		UpdateFunc: n.updateNodestoreFromTenantHandler,
		DeleteFunc: n.deleteNodestoreFromTenantHandler,
	})

	n.PodInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    n.addPod,
		UpdateFunc: nil,
		DeleteFunc: nil,
	})
}

func (n *NodeStoreOperator) Process() bool {

	// operator must be initialized
	if n.Base == nil {
		panic("n.Base is nil in Process()")
	}

	// process the workqueue
	wrappedKey, shutdown := n.Base.Workqueue.Get()
	if shutdown {
		n.Base.Logger.Info("Workqueue shutdown, stopping processing")
		return false
	}

	// mark the key as done
	defer n.Base.Workqueue.Done(wrappedKey)

	// parse the wrapped key to get the event and key
	event, key := operator.ParseQueuedKey(wrappedKey)
	n.Base.Logger.WithValues("event", event, "key", key).Info("Processing event")

	// check if the key is valid
	if key == "" {
		n.Base.Logger.Info("Invalid key, skipping processing", "key", key)
		n.Base.Workqueue.Forget(wrappedKey)
		return true
	}

	if event == operator.UnknownEvent {
		n.Base.Logger.Info("Unknown event type, skipping processing", "key", key)
		n.Base.Workqueue.Forget(wrappedKey)
		return true
	}

	var err error
	switch event {

	// nodestore added
	case operator.AddEvent:
		err = n.addNodeStore(key)

	// nodestore updated
	case operator.UpdateEvent:
		err = n.updateNodeStore(key)

	// nodestore deleted
	case operator.DeleteEvent:
		err = n.deleteNodeStore(key)

	// tenant created with awaiting node configuration for this node
	case WaitingNodeTenantEvent:
		err = n.configNodestore(key)
	}

	if err != nil {

		// requeue the nodestore if there was an error
		n.Base.Logger.Error(err, "Error processing event", "event", event, "key", key)
		utilruntime.HandleError(fmt.Errorf("error processing key %s: %w", key, err))
		n.Base.Workqueue.AddRateLimited(wrappedKey) q
	} else {

		n.Base.Logger.Info("Successfully processed event", "event", event, "key", key)
		// mark the key as processed
		n.Base.Workqueue.Forget(wrappedKey)
	}

	return true
}
