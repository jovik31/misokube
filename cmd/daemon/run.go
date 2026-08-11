package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"

	"github/setera/internal/cniserver"
	"github/setera/internal/ebpfmanager"
	"github/setera/internal/nodeipam"
	"github/setera/internal/noderouting"
	"github/setera/internal/nodewatcher"
	"github/setera/internal/podnetwork"
	"github/setera/internal/podwatcher"
	"github/setera/pkg/ipam/bitmap"
	"github/setera/pkg/network"
	filestore "github/setera/pkg/store/file"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func run(ctx context.Context, cfg config, logger klog.Logger) error {
	if cfg.nodeName == "" {
		return fmt.Errorf("node name is empty; set --node-name or NODE_NAME")
	}
	if cfg.stateDir == "" {
		return fmt.Errorf("state directory is empty")
	}
	if cfg.socketPath == "" {
		return fmt.Errorf("socket path is empty")
	}
	if cfg.mtu <= 0 {
		return fmt.Errorf("invalid MTU %d", cfg.mtu)
	}

	restConfig, err := buildRESTConfig(cfg)
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	node, err := kubeClient.CoreV1().Nodes().Get(
		ctx,
		cfg.nodeName,
		metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("get local Node %s: %w", cfg.nodeName, err)
	}

	nodePodCIDR, err := nodeIPv4PodCIDR(node)
	if err != nil {
		return err
	}
	if _, err := noderouting.VTEPAddress(nodePodCIDR); err != nil {
		return fmt.Errorf("derive local VTEP address: %w", err)
	}

	allocator, err := bitmap.New(
		nodePodCIDR,
		bitmap.WithReserved(
			reservedNodeAddresses(nodePodCIDR)...,
		),
	)
	if err != nil {
		return fmt.Errorf("create node IP allocator for %s: %w", nodePodCIDR, err)
	}

	stateStore, err := filestore.Open(cfg.stateDir)
	if err != nil {
		return fmt.Errorf("open node IPAM state %q: %w", cfg.stateDir, err)
	}
	defer func() {
		if err := stateStore.Close(); err != nil {
			logger.Error(err, "close node IPAM state")
		}
	}()

	ipam, err := nodeipam.New(allocator, stateStore)
	if err != nil {
		return fmt.Errorf("create node IPAM: %w", err)
	}
	if err := ipam.Restore(ctx); err != nil {
		return fmt.Errorf("restore node IPAM: %w", err)
	}

	networkOps := network.NewLinux()
	if err := networkOps.EnableIPv4Forwarding(); err != nil {
		return err
	}

	if cfg.clusterPodCIDR != "" {
		clusterPodCIDR, err := netip.ParsePrefix(cfg.clusterPodCIDR)
		if err != nil {
			return fmt.Errorf(
				"parse cluster Pod CIDR %q: %w",
				cfg.clusterPodCIDR,
				err,
			)
		}
		clusterPodCIDR = clusterPodCIDR.Masked()
		if !clusterPodCIDR.Addr().Is4() {
			return fmt.Errorf(
				"cluster Pod CIDR must be IPv4, got %s",
				clusterPodCIDR,
			)
		}
		if err := networkOps.EnsureIPv4Masquerade(clusterPodCIDR); err != nil {
			return err
		}
	}

	datapath := ebpfmanager.New()

	podNetwork, err := podnetwork.New(
		ipam,
		networkOps,
		datapath,
		cfg.mtu,
	)
	if err != nil {
		return fmt.Errorf("create Pod network configurator: %w", err)
	}

	recovery, err := podNetwork.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover local Pod networking: %w", err)
	}

	logger.Info(
		"restored local state",
		"node", cfg.nodeName,
		"podCIDR", nodePodCIDR.String(),
		"allocations", len(ipam.List()),
		"recoveredDatapaths", recovery.Recovered,
		"missingVeth", recovery.MissingVeth,
	)

	nodeRouting, err := noderouting.New(
		networkOps,
		noderouting.DefaultConfig(cfg.mtu),
	)
	if err != nil {
		return fmt.Errorf("create node routing: %w", err)
	}

	coreFactory := informers.NewSharedInformerFactory(
		kubeClient,
		cfg.resyncPeriod,
	)
	podInformer := coreFactory.Core().V1().Pods()
	nodeInformer := coreFactory.Core().V1().Nodes()

	remotePodWatcher, err := podwatcher.New(
		logger,
		cfg.nodeName,
		podInformer.Informer(),
		podInformer.Lister(),
		nodeInformer.Informer(),
		nodeInformer.Lister(),
		datapath,
	)
	if err != nil {
		return fmt.Errorf("create Pod watcher: %w", err)
	}

	routingWatcher, err := nodewatcher.New(
		logger,
		cfg.nodeName,
		nodePodCIDR,
		nodeInformer.Informer(),
		nodeInformer.Lister(),
		kubeClient.CoreV1().Nodes(),
		nodeRouting,
	)
	if err != nil {
		return fmt.Errorf("create Node watcher: %w", err)
	}

	cniServer, err := cniserver.New(
		cfg.socketPath,
		podInformer.Lister(),
		podNetwork,
		"",
	)
	if err != nil {
		return fmt.Errorf("create CNI server: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Both watchers registered their handlers before the shared informer
	// factory starts.
	coreFactory.Start(runCtx.Done())

	podWatcherErr := make(chan error, 1)
	go func() {
		podWatcherErr <- remotePodWatcher.Run(runCtx)
	}()

	nodeWatcherErr := make(chan error, 1)
	go func() {
		nodeWatcherErr <- routingWatcher.Run(runCtx)
	}()

	// Do not expose the CNI socket until both identity state and node routing
	// have completed their authoritative startup reconciliation.
	podReady := false
	nodeReady := false
	for !podReady || !nodeReady {
		select {
		case <-remotePodWatcher.Ready():
			if !podReady {
				podReady = true
				logger.Info("remote Pod state reconciled")
			}

		case <-routingWatcher.Ready():
			if !nodeReady {
				nodeReady = true
				logger.Info("node routing reconciled")
			}

		case err := <-podWatcherErr:
			if err == nil && runCtx.Err() != nil {
				return nil
			}
			return fmt.Errorf("Pod watcher stopped before readiness: %w", err)

		case err := <-nodeWatcherErr:
			if err == nil && runCtx.Err() != nil {
				return nil
			}
			return fmt.Errorf("Node watcher stopped before readiness: %w", err)

		case <-runCtx.Done():
			return nil
		}
	}

	cniErr := make(chan error, 1)
	go func() {
		cniErr <- cniServer.Run(runCtx)
	}()

	vtep, _ := noderouting.VTEPAddress(nodePodCIDR)
	logger.Info(
		"daemon started",
		"node", cfg.nodeName,
		"podCIDR", nodePodCIDR.String(),
		"vtepIP", vtep.String(),
		"socket", cfg.socketPath,
	)
	defer logger.Info("daemon stopped", "node", cfg.nodeName)

	select {
	case <-runCtx.Done():
		return nil

	case err := <-podWatcherErr:
		cancel()
		if err == nil || errors.Is(err, context.Canceled) {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("Pod watcher stopped unexpectedly")
		}
		return fmt.Errorf("Pod watcher stopped: %w", err)

	case err := <-nodeWatcherErr:
		cancel()
		if err == nil || errors.Is(err, context.Canceled) {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("Node watcher stopped unexpectedly")
		}
		return fmt.Errorf("Node watcher stopped: %w", err)

	case err := <-cniErr:
		cancel()
		if err == nil || errors.Is(err, context.Canceled) {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("CNI server stopped unexpectedly")
		}
		return fmt.Errorf("CNI server stopped: %w", err)
	}
}

func nodeIPv4PodCIDR(node *corev1.Node) (netip.Prefix, error) {
	if node == nil {
		return netip.Prefix{}, fmt.Errorf("local Node is nil")
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
			return netip.Prefix{}, fmt.Errorf(
				"parse PodCIDR %q for Node %s: %w",
				value,
				node.Name,
				err,
			)
		}
		prefix = prefix.Masked()
		if prefix.Addr().Is4() {
			return prefix, nil
		}
	}

	return netip.Prefix{}, fmt.Errorf(
		"Node %s does not have an IPv4 PodCIDR",
		node.Name,
	)
}

func reservedNodeAddresses(prefix netip.Prefix) []netip.Addr {
	prefix = prefix.Masked()
	if !prefix.IsValid() || !prefix.Addr().Is4() {
		return nil
	}

	reserved := make(map[netip.Addr]struct{}, 4)
	reserved[prefix.Addr()] = struct{}{}
	reserved[ipv4LastAddr(prefix)] = struct{}{}

	if vtep, err := noderouting.VTEPAddress(prefix); err == nil {
		reserved[vtep.Addr()] = struct{}{}
	}

	out := make([]netip.Addr, 0, len(reserved))
	for addr := range reserved {
		out = append(out, addr)
	}
	return out
}

func ipv4LastAddr(prefix netip.Prefix) netip.Addr {
	prefix = prefix.Masked()
	baseBytes := prefix.Addr().As4()
	base := uint64(binary.BigEndian.Uint32(baseBytes[:]))
	hostBits := uint(32 - prefix.Bits())
	last := base | ((uint64(1) << hostBits) - 1)

	var out [4]byte
	binary.BigEndian.PutUint32(out[:], uint32(last))
	return netip.AddrFrom4(out)
}