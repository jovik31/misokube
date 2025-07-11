package main

import (
	orchestrator "github/setera/internal/orchestrator/operator"
	"os"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github/setera/pkg/k8s"
)

func main() {

	// Initialize command line flags

	ctx := signals.SetupSignalHandler()
	logger := klog.FromContext(ctx).WithName("orchestrator-main")

	config, err := k8s.InitKubeConfig()
	if err != nil {
		logger.Error(err, "failed to fetch kubeconfig")
		os.Exit(1)
	}

	// Check if kubeclientset is nil
	if config == nil {
		logger.Error(nil, "kubeconfig is nil, cannot proceed")
		os.Exit(1)
	} else {
		logger.Info("kubeconfig initialized successfully")
	}

	kubeClient, err := k8s.NewKubeClient(config)
	if err != nil {
		logger.Error(err, "failed to create kubeClient")
		os.Exit(1)
	}

	seteraClient, err := k8s.NewSeteraClient(config)
	if err != nil {
		logger.Error(err, "failed to create seteraClient")
		os.Exit(1)
	}

	// Build the orchestrator operator (with base operator inside)
	tenantOperator := orchestrator.NewTenantOperator(ctx, "setera-orchestrator-operator", seteraClient, kubeClient)

	// Run it
	if err := tenantOperator.Base.Run(ctx); err != nil {
		logger.Error(err, "orchestrator failed to run")
		os.Exit(1)
	}
}
