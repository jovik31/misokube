package main

import (
	"context"
	"fmt"

	"github/setera/internal/tenantcontroller"
	seteraclient "github/setera/pkg/generated/clientset/versioned"
	seterainformers "github/setera/pkg/generated/informers/externalversions"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func run(ctx context.Context, cfg config, logger klog.Logger) error {
	restConfig, err := buildRESTConfig(cfg)
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	seteraClient, err := seteraclient.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create setera client: %w", err)
	}

	coreFactory := informers.NewSharedInformerFactory(
		kubeClient,
		cfg.resyncPeriod,
	)
	seteraFactory := seterainformers.NewSharedInformerFactory(
		seteraClient,
		cfg.resyncPeriod,
	)

	nodeInformer := coreFactory.Core().V1().Nodes()
	tenantInformer := seteraFactory.Setera().V1().Tenants()

	controller := tenantcontroller.New(
		logger,
		seteraClient,
		kubeClient,
		tenantInformer.Informer(),
		tenantInformer.Lister(),
		nodeInformer.Informer(),
		nodeInformer.Lister(),
	)

	// Informers must be requested before Start so both factories know which
	// informers to launch. tenantcontroller.New() registers the event handlers.
	coreFactory.Start(ctx.Done())
	seteraFactory.Start(ctx.Done())

	logger.Info("orchestrator started")
	defer logger.Info("orchestrator stopped")

	// The controller owns cache synchronization and its worker lifecycle.
	if err := controller.Run(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	return nil
}
