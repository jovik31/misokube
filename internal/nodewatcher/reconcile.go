package nodewatcher

import (
	"context"
	"fmt"

	"github/setera/internal/noderouting"
	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (w *Watcher) reconcileAll(ctx context.Context) error {
	nodes, err := w.nodeLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list Nodes: %w", err)
	}

	desired := make([]noderouting.RemoteNode, 0, len(nodes))
	for _, node := range nodes {
		if node == nil || node.Name == w.localNodeName {
			continue
		}

		remote, ready, err := remoteNodeFor(node)
		if err != nil {
			return err
		}
		if !ready {
			continue
		}
		desired = append(desired, remote)
	}

	if err := w.routing.ReconcileRemoteNodes(ctx, desired); err != nil {
		return err
	}

	return nil
}

func remoteNodeFor(node *corev1.Node) (noderouting.RemoteNode, bool, error) {
	if node == nil {
		return noderouting.RemoteNode{}, false, nil
	}

	vtep, ready, err := tenantmeta.VTEPFromMetadata(node.Labels, node.Annotations)
	if err != nil {
		return noderouting.RemoteNode{}, false, fmt.Errorf("Node %s VTEP metadata: %w", node.Name, err)
	}
	if !ready {
		return noderouting.RemoteNode{}, false, nil
	}

	podCIDR, err := nodeIPv4PodCIDR(node)
	if err != nil {
		return noderouting.RemoteNode{}, false, err
	}
	underlayIP, err := nodeInternalIPv4(node)
	if err != nil {
		return noderouting.RemoteNode{}, false, err
	}

	return noderouting.RemoteNode{
		Name:       node.Name,
		PodCIDR:    podCIDR,
		UnderlayIP: underlayIP,
		VTEPIP:     vtep.IP,
		VTEPMAC:    vtep.MAC,
	}, true, nil
}