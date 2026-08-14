package nodewatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"

	"github/setera/internal/noderouting"
	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (w *Watcher) publishLocalVTEP(ctx context.Context, local noderouting.LocalNode) error {
	labels, annotations, err := tenantmeta.VTEPNodeMetadata(tenantmeta.VTEP{
		IP:  local.VTEPIP,
		MAC: local.VTEPMAC,
	})
	if err != nil {
		return err
	}

	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"labels":      labels,
			"annotations": annotations,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal Node VTEP patch: %w", err)
	}

	if _, err := w.nodeClient.Patch(
		ctx,
		w.localNodeName,
		types.MergePatchType,
		patch,
		metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("patch Node %s: %w", w.localNodeName, err)
	}

	return nil
}

func nodeIPv4PodCIDR(node *corev1.Node) (netip.Prefix, error) {
	if node == nil {
		return netip.Prefix{}, fmt.Errorf("Node is nil")
	}

	candidates := make([]string, 0, len(node.Spec.PodCIDRs)+1)
	candidates = append(candidates, node.Spec.PodCIDRs...)
	if node.Spec.PodCIDR != "" {
		candidates = append(candidates, node.Spec.PodCIDR)
	}

	for _, value := range candidates {
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("parse PodCIDR %q for Node %s: %w", value, node.Name, err)
		}
		prefix = prefix.Masked()
		if prefix.Addr().Unmap().Is4() {
			return prefix, nil
		}
	}

	return netip.Prefix{}, fmt.Errorf("Node %s does not have an IPv4 PodCIDR", node.Name)
}

func nodeInternalIPv4(node *corev1.Node) (netip.Addr, error) {
	if node == nil {
		return netip.Addr{}, fmt.Errorf("Node is nil")
	}

	for _, address := range node.Status.Addresses {
		if address.Type != corev1.NodeInternalIP || address.Address == "" {
			continue
		}
		ip, err := netip.ParseAddr(address.Address)
		if err != nil {
			continue
		}
		ip = ip.Unmap()
		if ip.Is4() && ip.Zone() == "" && !ip.IsUnspecified() {
			return ip, nil
		}
	}

	return netip.Addr{}, fmt.Errorf("Node %s does not have an IPv4 InternalIP", node.Name)
}