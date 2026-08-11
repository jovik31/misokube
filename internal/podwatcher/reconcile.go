package podwatcher

import (
	"context"
	"fmt"
	"net/netip"

	"github/setera/internal/ebpfmanager"
	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

func (w *Watcher) reconcileAll(ctx context.Context) error {
	localNode, err := w.nodeLister.Get(w.localNodeName)
	if err != nil {
		return fmt.Errorf(
			"get local Node %s: %w",
			w.localNodeName,
			err,
		)
	}

	nodes, err := w.nodeLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list Nodes: %w", err)
	}
	nodesByName := make(map[string]*corev1.Node, len(nodes))
	for _, node := range nodes {
		if node != nil {
			nodesByName[node.Name] = node
		}
	}

	pods, err := w.podLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list Pods: %w", err)
	}

	desired := make([]ebpfmanager.RemotePod, 0)
	known := make(map[string]ebpfmanager.RemotePod)

	for _, pod := range pods {
		if pod == nil {
			continue
		}

		remote, relevant, err := remotePodFor(
			localNode,
			nodesByName[pod.Spec.NodeName],
			pod,
		)
		if err != nil {
			w.logger.Error(
				err,
				"ignore invalid remote Pod",
				"namespace", pod.Namespace,
				"pod", pod.Name,
			)
			continue
		}
		if !relevant {
			continue
		}

		key, err := cache.MetaNamespaceKeyFunc(pod)
		if err != nil {
			return fmt.Errorf(
				"build Pod key %s/%s: %w",
				pod.Namespace,
				pod.Name,
				err,
			)
		}

		desired = append(desired, remote)
		known[key] = remote
	}

	if err := w.datapath.ReconcileRemotePods(ctx, desired); err != nil {
		return err
	}

	// Only replace watcher ownership after the full datapath reconciliation
	// succeeds.
	w.known = known
	return nil
}

func (w *Watcher) reconcileKey(
	ctx context.Context,
	key string,
) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("split Pod key %q: %w", key, err)
	}

	pod, err := w.podLister.Pods(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		return w.deleteKnown(ctx, key)
	}
	if err != nil {
		return fmt.Errorf("get Pod %s: %w", key, err)
	}

	localNode, err := w.nodeLister.Get(w.localNodeName)
	if err != nil {
		return fmt.Errorf(
			"get local Node %s: %w",
			w.localNodeName,
			err,
		)
	}

	var remoteNode *corev1.Node
	if pod.Spec.NodeName != "" &&
		pod.Spec.NodeName != w.localNodeName {
		remoteNode, err = w.nodeLister.Get(pod.Spec.NodeName)
		if apierrors.IsNotFound(err) {
			remoteNode = nil
		} else if err != nil {
			return fmt.Errorf(
				"get remote Node %s for Pod %s: %w",
				pod.Spec.NodeName,
				key,
				err,
			)
		}
	}

	desired, relevant, err := remotePodFor(
		localNode,
		remoteNode,
		pod,
	)
	if err != nil {
		return fmt.Errorf("resolve remote Pod %s: %w", key, err)
	}
	if !relevant {
		return w.deleteKnown(ctx, key)
	}

	old, hadOld := w.known[key]

	if err := w.datapath.UpsertRemotePod(ctx, desired); err != nil {
		return err
	}

	// If Kubernetes moved/recreated this keyed Pod with a new IP, remove the
	// old endpoint only after the new one is successfully published.
	if hadOld && old.IP != desired.IP {
		if err := w.datapath.DeleteRemotePod(
			ctx,
			old.IP,
			old.PodUID,
		); err != nil {
			return err
		}
	}

	w.known[key] = desired
	return nil
}

func (w *Watcher) deleteKnown(
	ctx context.Context,
	key string,
) error {
	old, ok := w.known[key]
	if !ok {
		return nil
	}

	if err := w.datapath.DeleteRemotePod(
		ctx,
		old.IP,
		old.PodUID,
	); err != nil {
		return err
	}

	delete(w.known, key)
	return nil
}

func remotePodFor(
	localNode *corev1.Node,
	remoteNode *corev1.Node,
	pod *corev1.Pod,
) (ebpfmanager.RemotePod, bool, error) {
	if localNode == nil || pod == nil {
		return ebpfmanager.RemotePod{}, false, nil
	}

	if pod.Spec.HostNetwork ||
		pod.Spec.NodeName == "" ||
		pod.Spec.NodeName == localNode.Name ||
		pod.Status.Phase == corev1.PodSucceeded ||
		pod.Status.Phase == corev1.PodFailed {
		return ebpfmanager.RemotePod{}, false, nil
	}

	ip, ok, err := podIPv4(pod)
	if err != nil {
		return ebpfmanager.RemotePod{}, false, err
	}
	if !ok {
		return ebpfmanager.RemotePod{}, false, nil
	}

	tenant := tenantmeta.ResolvePodTenant(
		pod.Namespace,
		pod.Labels,
	)

	// The default tenant is shared and must be reachable from every tenant,
	// regardless of Node tenant-membership labels.
	if tenant != tenantmeta.DefaultTenant {
		if !tenantmeta.HasTenant(localNode.Labels, tenant) {
			return ebpfmanager.RemotePod{}, false, nil
		}
		if remoteNode == nil ||
			!tenantmeta.HasTenant(remoteNode.Labels, tenant) {
			return ebpfmanager.RemotePod{}, false, nil
		}
	}

	if pod.UID == "" {
		return ebpfmanager.RemotePod{}, false, fmt.Errorf(
			"Pod UID is empty",
		)
	}

	return ebpfmanager.RemotePod{
		IP:       ip,
		PodUID:   string(pod.UID),
		TenantID: tenant,
	}, true, nil
}

func podIPv4(
	pod *corev1.Pod,
) (netip.Addr, bool, error) {
	if pod == nil {
		return netip.Addr{}, false, nil
	}

	candidates := make([]string, 0, len(pod.Status.PodIPs)+1)
	for _, podIP := range pod.Status.PodIPs {
		if podIP.IP != "" {
			candidates = append(candidates, podIP.IP)
		}
	}
	if pod.Status.PodIP != "" {
		candidates = append(candidates, pod.Status.PodIP)
	}

	var firstParseErr error
	for _, value := range candidates {
		ip, err := netip.ParseAddr(value)
		if err != nil {
			if firstParseErr == nil {
				firstParseErr = fmt.Errorf(
					"parse Pod IP %q: %w",
					value,
					err,
				)
			}
			continue
		}

		ip = ip.Unmap()
		if ip.Is4() && ip.Zone() == "" {
			return ip, true, nil
		}
	}

	if firstParseErr != nil {
		return netip.Addr{}, false, firstParseErr
	}

	return netip.Addr{}, false, nil
}
