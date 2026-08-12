package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultSocketPath = "/var/run/setera/setera.sock"
	defaultStateDir   = "/var/lib/cni/tenantcni/ipam"
	defaultCgroupRoot = "/host/sys/fs/cgroup"
	defaultMTU        = 1450
)

type config struct {
	kubeconfig string
	nodeName   string

	socketPath string
	stateDir   string
	cgroupRoot string
	mtu        int

	// Optional for now. When set, the daemon installs Pod egress
	// masquerading for traffic leaving the cluster-wide Pod CIDR.
	clusterPodCIDR string

	resyncPeriod time.Duration
}

func parseConfig() config {
	var cfg config

	flag.StringVar(
		&cfg.kubeconfig,
		"kubeconfig",
		"",
		"path to kubeconfig file; when empty, in-cluster config is tried first and the default kubeconfig is used as a fallback",
	)
	flag.StringVar(
		&cfg.nodeName,
		"node-name",
		os.Getenv("NODE_NAME"),
		"Kubernetes Node name; defaults to NODE_NAME",
	)
	flag.StringVar(
		&cfg.socketPath,
		"socket-path",
		defaultSocketPath,
		"Unix domain socket used by the Setera CNI shim",
	)
	flag.StringVar(
		&cfg.stateDir,
		"state-dir",
		defaultStateDir,
		"node-local durable IPAM state directory",
	)
	flag.StringVar(
		&cfg.cgroupRoot,
		"cgroup-root",
		defaultCgroupRoot,
		"host cgroup v2 root used by the Service socket load balancer",
	)
	flag.IntVar(
		&cfg.mtu,
		"mtu",
		defaultMTU,
		"Pod veth and VXLAN MTU",
	)
	flag.StringVar(
		&cfg.clusterPodCIDR,
		"cluster-pod-cidr",
		os.Getenv("CLUSTER_POD_CIDR"),
		"optional cluster-wide IPv4 Pod CIDR used for egress masquerading",
	)
	flag.DurationVar(
		&cfg.resyncPeriod,
		"resync-period",
		0,
		"shared informer resync period; 0 disables periodic resync",
	)

	flag.Parse()
	return cfg
}

func buildRESTConfig(cfg config) (*rest.Config, error) {
	if cfg.kubeconfig != "" {
		restConfig, err := clientcmd.BuildConfigFromFlags("", cfg.kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("build kubeconfig %q: %w", cfg.kubeconfig, err)
		}
		return restConfig, nil
	}

	if restConfig, err := rest.InClusterConfig(); err == nil {
		return restConfig, nil
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build default kubeconfig: %w", err)
	}
	return restConfig, nil
}
