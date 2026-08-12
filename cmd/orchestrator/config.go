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
	defaultWebhookNamespace = "default"
	defaultWebhookTLSDir    = "/tmp/setera-webhook"
)

type config struct {
	kubeconfig       string
	resyncPeriod     time.Duration
	webhookNamespace string
	webhookTLSDir    string
}

func parseConfig() config {
	var cfg config

	webhookNamespace := os.Getenv("POD_NAMESPACE")
	if webhookNamespace == "" {
		webhookNamespace = defaultWebhookNamespace
	}

	flag.StringVar(
		&cfg.kubeconfig,
		"kubeconfig",
		"",
		"path to kubeconfig file; when empty, in-cluster config is tried first and the default kubeconfig is used as a fallback",
	)

	flag.DurationVar(
		&cfg.resyncPeriod,
		"resync-period",
		0,
		"shared informer resync period; 0 disables resyncing",
	)

	flag.StringVar(
		&cfg.webhookNamespace,
		"webhook-namespace",
		webhookNamespace,
		"namespace that contains the webhook Service",
	)

	flag.StringVar(
		&cfg.webhookTLSDir,
		"webhook-tls-dir",
		defaultWebhookTLSDir,
		"directory for the generated webhook TLS files",
	)

	flag.Parse()

	return cfg
}

func buildRESTConfig(cfg config) (*rest.Config, error) {
	if cfg.kubeconfig != "" {
		restConfig, err := clientcmd.BuildConfigFromFlags(
			"",
			cfg.kubeconfig,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"build kubeconfig %q: %w",
				cfg.kubeconfig,
				err,
			)
		}

		return restConfig, nil
	}

	if restConfig, err := rest.InClusterConfig(); err == nil {
		return restConfig, nil
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	clientConfig :=
		clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules,
			&clientcmd.ConfigOverrides{},
		)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf(
			"build default kubeconfig: %w",
			err,
		)
	}

	return restConfig, nil
}
