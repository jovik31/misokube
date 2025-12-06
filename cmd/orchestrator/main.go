package main

import (
	"github/setera/internal/orchestrator"
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
		logger.Info("INIT KUBECONFIG SUCCESSFUL")
	}

	kubeClient, seteraClient, err := k8s.InitClients(config)
	if err != nil {
		logger.Error(err, "failed to initialize clients")
		os.Exit(1)
	}

	// Build the orchestrator operator (with base operator inside)
	orch := orchestrator.New()

	// Run it
	if err := tenantOperator.Base.Run(ctx, tenantOperator); err != nil {
		logger.Error(err, "orchestrator failed to run")
		os.Exit(1)
	}
}
