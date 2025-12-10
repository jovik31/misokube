package main

import (
	"os"

	// internal
	orch "github/setera/internal/orchestrator"
	op "github/setera/pkg/operator"

	// clients/informers
	seterainformers "github/setera/pkg/generated/informers/externalversions"
	seterav1informers "github/setera/pkg/generated/informers/externalversions/setera.com/v1"
	"github/setera/pkg/k8s"

	// logging + signals
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
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

	_, seteraClient, err := k8s.InitClients(config)
	if err != nil {
		logger.Error(err, "failed to initialize clients")
		os.Exit(1)
	}

	// Shared informer factory for Setera CRDs
	factory := seterainformers.NewSharedInformerFactory(seteraClient, 0)
	v1 := seterav1informers.New(factory, "default", nil)
	tenantInf := v1.Tenants().Informer()
	tenantLister := v1.Tenants().Lister()
	nodeStoreInf := v1.NodeStores().Informer()
	nodeStoreLister := v1.NodeStores().Lister()

	// Base operator
	base := op.NewBaseOperator("orchestrator", logger, nil)

	// Construct orchestrator operator
	orchOp := orch.New(
		base,
		logger,
		nil, // recorder (optional)
		seteraClient,
		tenantInf,
		tenantLister,
		nodeStoreInf,
		nodeStoreLister,
	)
	if orchOp == nil {
		logger.Error(nil, "failed to construct orchestrator operator")
		os.Exit(1)
	}

	// Start informers then run the operator
	factory.Start(ctx.Done())
	if err := orchOp.Run(ctx); err != nil {
		logger.Error(err, "orchestrator failed to run")
		os.Exit(1)
	}
}
