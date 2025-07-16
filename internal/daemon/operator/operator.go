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

	v1 "github/setera/pkg/api/setera.com/v1"
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

// problem: the tenant default requires a larger network than a client tenant. in this case a /29
// actually it doesnt since we can always update and increase the default tenant information.

// default tenant SNAT rules application

// for a client tenant A that needs to reach the client tenant B it requires the following:
// No iptables rules between tenant A and tenant default subnet in the same node
// SNAT rule for each remote node subnet of the tenant-default, with the NAT being an IP in the local node tenant default subnet
// To comunicate with the nodes themselves add a forward rule to get the node cidrs.

// nodestore logic on the nodestore operator side
// when a nodestore has its status updated we need to see:
// if a new tenant was added we need to:
// configure tenant isolation between:
// tenant and the existing tenants in the node except the default tenant.
// check the tenants and configure iptables rules to block the traffic between them
//  if there is a change in the tenant Info from the previous

/* Need to think a bit more on the logic and flow onf the nodestore interaction between each other. This is because we can extract
Tenant Info from both the nodestores and the tenant custom resources.

	1) Use tenant to get other node configuration:


	2) Use the nodestores of other nodes to add and update tenant info across nodes:

		When a remote nodestore is updated get the tenants that are common to the local one
		For each common tenant:
			Same tenant: Add routing, ARP and FDB entries
			Different tenants:
				// if default tenant:
					Add SNAT rules
				// else
					// Do nothing
		--> update all required rules, entries and routes when a tenant subnet changes

*/

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

	// build indexers
	tenantInformer.AddIndexers(cache.Indexers{
		"awaitingNodes": func(obj interface{}) ([]string, error) {
			tenant, ok := obj.(*v1.Tenant)
			if !ok {
				return nil, fmt.Errorf("object is not a Tenant: %T", obj)
			}
			return tenant.Status.AwaitingNodeConfiguration, nil
		},
	})

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

		AddFunc:    n.addNodeStoreHandler,    // when a nodestore is added we need to add its finalizer and block its deletion
		UpdateFunc: n.updateNodeStoreHandler, // when there is an update to a nodestore status we need to trigger the config of the iptables to block communications between tenants on the same node and add the snat rules to access the default tenant
		DeleteFunc: n.deleteNodeStoreHandler, // a node store cannot be deleted
	})

	n.TenantInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    nil,                                // handled by the orchestrator tenant operator - we need the tenant to have nodes in waiting or assigned
		UpdateFunc: n.updateNodestoreFromTenantHandler, // when a tenant is updated we need to trigger the node wait config or the node assign config
		DeleteFunc: n.deleteNodestoreFromTenantHandler, // when a tenant is deleted we need to trigger the node remove tenant config
	})

	n.PodInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    n.addPod, // handle pod addition and save it so we can pass the tenantID back to the CNI
		UpdateFunc: nil,      // not required/handled - feature: change pod to a differnet tenant while running
		DeleteFunc: nil,      // not required/handled - feature: change pod to a different tenant while running
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
		n.Base.Workqueue.AddRateLimited(wrappedKey)
	} else {

		n.Base.Logger.Info("Successfully processed event", "event", event, "key", key)
		// mark the key as processed
		n.Base.Workqueue.Forget(wrappedKey)
	}

	return true
}
