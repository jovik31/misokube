package orchestrator

import (
	"context"
	"fmt"
	"strings"

	seterav1 "github/setera/pkg/api/setera.com/v1"

	// k8s
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// client-go
	"k8s.io/client-go/tools/cache"
)

func (o *OrchOperator) enqueue(obj any, event Event) {

	// check if every node has an existing nodestore

	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		o.Base.Logger.Error(err, "Error in getting key for object", obj)
		return
	}

	//wrap the key with the event type
	wrappedKey := fmt.Sprintf("%s:%s", event, key)

	//ok, reason := o.checkNodeNodeStore()
	/*if !ok {
		o.Base.Logger.Info(reason, key)
		o.Base.Workqueue.AddRateLimited(wrappedKey)
		return
	} else {
		o.Base.Logger.Info("Adding key to workqueue", wrappedKey)
		o.Base.Workqueue.Add(wrappedKey)

	}*/

	o.Base.Logger.Info("Adding key to workqueue", wrappedKey)
	o.Base.Workqueue.Add(wrappedKey)
}

func parseQueuedKey(wrappedKey string) (Event, string) {

	parts := strings.Split(wrappedKey, ":")
	if len(parts) < 2 {
		return UnknownEvent, wrappedKey
	}
	event := Event(parts[0])
	key := parts[1]
	return event, key
}

/*func (o *OrchOperator) checkNodeNodeStore() (bool, string) {

	// retrieve the nodes and nodestores from the cache
	nodes, err := o.nodeLister.List(labels.Everything())
	if err != nil {
		o.logger.Error(err, "Error in getting nodes from cache")
		return false, "failed to retrieve nodes from cache"
	}
	nodestores, err := o.nodeStoreLister.List(labels.Everything())
	if err != nil {
		o.logger.Error(err, "Error in getting nodestores from cache")
		return false, "failed to retrieve nodestores from cache"
	}

	// create a map of nodestores
	nodestoreMap := make(map[string]*seterav1.NodeStore)
	for _, nodestore := range nodestores {
		nodestoreMap[nodestore.Spec.Name] = nodestore
	}

	// check if every node has a nodestore
	for _, node := range nodes {
		if _, ok := nodestoreMap[node.Name]; !ok {
			o.logger.Info("node does not have a nodestore", node.Name)
			return false, fmt.Sprintf("nodestore for node %s does not exist", node.Name)
		}
	}

	return true, ""

}*/

// checks if tenant has the finalizer
func (o *OrchOperator) checkTenantFinalizer(tenant *seterav1.Tenant) bool {

	for _, f := range tenant.Finalizers {
		if f == TenantFinalizer {
			return true // finalizer found
		}
	}
	return false // finalizer not found

}

func (o *OrchOperator) updateTenantObject(tenant *seterav1.Tenant) error {

	// update the tenant object in the k8s cluster
	_, err := o.Base.Seterav1Clientset.SeteraV1().Tenants(tenant.Namespace).Update(context.TODO(), tenant, metav1.UpdateOptions{})
	if err != nil {
		o.Base.Logger.Error(err, "Error in updating tenant object in the k8s cluster", tenant.Name)
		return err
	}
	return nil
}
