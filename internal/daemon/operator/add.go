package daemon

import (

	//std
	"context"
	"fmt"

	//internal packages
	config "github/setera/pkg"
	"github/setera/pkg/operator"

	//k8s client-go
	"k8s.io/client-go/tools/cache"

	//k8s
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (n *NodeStoreOperator) addNodeStore(key string) error {

	ctx := context.Background()

	n.Base.Logger.WithValues("event", operator.AddEvent, "key", key).Info("Adding NodeStore")

	// fetch the nodestore from the cache
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		n.Base.Logger.Error(err, "Failed to split key", "key", key)
	}

	// fetch nodestore from cache
	nodestore, err := n.NodeStoreLister.NodeStores(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			n.Base.Logger.Error(err, "NodeStore not found in cache", "key", key)
			return fmt.Errorf("nodestore %s not found in namespace %s", name, namespace)
		} else {
			n.Base.Logger.Error(err, "Error fetching NodeStore from cache", "key", key)
			return err
		}
	}

	// create a copy of the nodestore
	mod := nodestore.DeepCopy()

	// ensure finalizer is present
	if !operator.ContainsString(nodestore.Finalizers, config.NodeStoreFinalizer) {
		mod.Finalizers = append(mod.Finalizers, config.NodeStoreFinalizer)
		n.Base.Logger.WithValues("nodestore", nodestore.Name).Info("Adding finalizer to NodeStore")
	}

	// update the nodestore with the finalizer
	_, err = n.Base.Seterav1Clientset.SeteraV1().NodeStores(namespace).Update(ctx, mod, metav1.UpdateOptions{})
	if err != nil {
		n.Base.Logger.WithValues("nodestore", mod.Name).Error(err, "Failed to update NodeStore with finalizer")
		n.Base.Recorder.Eventf(mod, "Warning", "UpdateFailed", "Failed to update NodeStore %s with finalizer: %v", mod.Name, err)

		return fmt.Errorf("failed to update nodestore %s with finalizer: %w", nodestore.Name, err)
	}

	return nil
}
