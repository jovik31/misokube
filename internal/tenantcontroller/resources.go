package tenantcontroller

import (
	"context"
	"fmt"
	"net/netip"
	"sort"

	"github/misokube/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

const (
	computeScoreWeight = 0.5
	addressScoreWeight = 1 - computeScoreWeight
)

type nodeResourceScore struct {
	total     float64
	compute   float64
	addresses float64
}

func (c *Controller) rankScaleUpCandidates(
	ctx context.Context,
	nodes []*corev1.Node,
) ([]*corev1.Node, error) {
	if len(nodes) == 0 {
		return nodes, nil
	}
	if c.metrics == nil {
		// Unit tests and embedders that do not provide a metrics client keep the
		// deterministic tenant-count ordering produced by scaleUpCandidates.
		return nodes, nil
	}

	pods, err := c.kube.CoreV1().Pods("").List(
		ctx,
		metav1.ListOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("list pods for node resource scoring: %w", err)
	}

	type scoredNode struct {
		node  *corev1.Node
		score nodeResourceScore
	}

	scored := make([]scoredNode, 0, len(nodes))
	for _, node := range nodes {
		metrics, err := c.metrics.MetricsV1beta1().NodeMetricses().Get(
			ctx,
			node.Name,
			metav1.GetOptions{},
		)
		if err != nil {
			return nil, fmt.Errorf(
				"get metrics for node %s: %w",
				node.Name,
				err,
			)
		}

		score, err := calculateNodeResourceScore(
			node,
			metrics,
			pods.Items,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"score node %s: %w",
				node.Name,
				err,
			)
		}

		c.logger.Info(
			"computed resource-aware node score",
			"node", node.Name,
			"score", score.total,
			"computeAvailability", score.compute,
			"addressAvailability", score.addresses,
		)

		scored = append(scored, scoredNode{
			node:  node,
			score: score,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score.total != scored[j].score.total {
			return scored[i].score.total > scored[j].score.total
		}

		left := tenantmeta.TenantCount(scored[i].node.Labels)
		right := tenantmeta.TenantCount(scored[j].node.Labels)
		if left != right {
			return left < right
		}

		return scored[i].node.Name < scored[j].node.Name
	})

	out := make([]*corev1.Node, 0, len(scored))
	for _, candidate := range scored {
		out = append(out, candidate.node)
	}
	return out, nil
}

func calculateNodeResourceScore(
	node *corev1.Node,
	metrics *metricsv1beta1.NodeMetrics,
	pods []corev1.Pod,
) (nodeResourceScore, error) {
	if node == nil || metrics == nil {
		return nodeResourceScore{}, fmt.Errorf("node or metrics is nil")
	}

	cpuCapacity := node.Status.Allocatable.Cpu().MilliValue()
	memoryCapacity := node.Status.Allocatable.Memory().Value()
	if cpuCapacity <= 0 || memoryCapacity <= 0 {
		return nodeResourceScore{}, fmt.Errorf(
			"allocatable CPU or memory is zero",
		)
	}

	cpuUsage := metrics.Usage.Cpu().MilliValue()
	memoryUsage := metrics.Usage.Memory().Value()
	cpuAvailability := clamp01(
		1 - float64(cpuUsage)/float64(cpuCapacity),
	)
	memoryAvailability := clamp01(
		1 - float64(memoryUsage)/float64(memoryCapacity),
	)
	computeAvailability := (cpuAvailability + memoryAvailability) / 2

	addressAvailability, err := nodeAddressAvailability(node, pods)
	if err != nil {
		return nodeResourceScore{}, err
	}

	return nodeResourceScore{
		total: computeScoreWeight*computeAvailability +
			addressScoreWeight*addressAvailability,
		compute:   computeAvailability,
		addresses: addressAvailability,
	}, nil
}

func nodeAddressAvailability(
	node *corev1.Node,
	pods []corev1.Pod,
) (float64, error) {
	prefix, err := nodeIPv4PodCIDR(node)
	if err != nil {
		return 0, err
	}

	hostBits := 32 - prefix.Bits()
	total := uint64(1) << hostBits
	reserved := uint64(1)
	if total > 1 {
		reserved++
	}
	if total > 2 {
		reserved++
	}
	capacity := total - reserved
	if capacity == 0 {
		return 0, fmt.Errorf("PodCIDR %s has no allocatable addresses", prefix)
	}

	allocated := make(map[netip.Addr]struct{})
	for i := range pods {
		pod := &pods[i]
		if pod.Spec.NodeName != node.Name {
			continue
		}

		for _, podIP := range pod.Status.PodIPs {
			addr, err := netip.ParseAddr(podIP.IP)
			if err != nil {
				continue
			}
			addr = addr.Unmap()
			if addr.Is4() && prefix.Contains(addr) {
				allocated[addr] = struct{}{}
			}
		}
	}

	used := uint64(len(allocated))
	if used >= capacity {
		return 0, nil
	}
	return float64(capacity-used) / float64(capacity), nil
}

func nodeIPv4PodCIDR(node *corev1.Node) (netip.Prefix, error) {
	if node == nil {
		return netip.Prefix{}, fmt.Errorf("node is nil")
	}

	values := append([]string(nil), node.Spec.PodCIDRs...)
	if node.Spec.PodCIDR != "" {
		values = append(values, node.Spec.PodCIDR)
	}
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		if prefix.Addr().Unmap().Is4() {
			return prefix, nil
		}
	}

	return netip.Prefix{}, fmt.Errorf(
		"node %s has no IPv4 PodCIDR",
		node.Name,
	)
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
