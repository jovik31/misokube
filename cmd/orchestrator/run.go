package main

import (
	"context"
	"fmt"

	"github/setera/internal/tenantcontroller"
	"github/setera/internal/webhook"
	seteraclient "github/setera/pkg/generated/clientset/versioned"
	seterainformers "github/setera/pkg/generated/informers/externalversions"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func run(
	ctx context.Context,
	cfg config,
	logger klog.Logger,
) error {
	restConfig, err := buildRESTConfig(cfg)
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf(
			"create kubernetes client: %w",
			err,
		)
	}

	seteraClient, err := seteraclient.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf(
			"create Setera client: %w",
			err,
		)
	}

	tlsFiles, err := webhook.GenerateTLSFiles(
		webhook.WebhookServiceName,
		cfg.webhookNamespace,
		cfg.webhookTLSDir,
	)
	if err != nil {
		return fmt.Errorf(
			"prepare webhook TLS: %w",
			err,
		)
	}

	if err := webhook.EnsureMutatingWebhookConfiguration(
		ctx,
		kubeClient,
		cfg.webhookNamespace,
		tlsFiles.CABundle,
	); err != nil {
		return fmt.Errorf(
			"register Pod admission webhook: %w",
			err,
		)
	}

	webhookServer := webhook.NewWebhookServer(
		seteraClient,
		kubeClient,
		tlsFiles.CertFile,
		tlsFiles.KeyFile,
	)

	coreFactory := informers.NewSharedInformerFactory(
		kubeClient,
		cfg.resyncPeriod,
	)

	seteraFactory := seterainformers.NewSharedInformerFactory(
		seteraClient,
		cfg.resyncPeriod,
	)

	nodeInformer := coreFactory.
		Core().
		V1().
		Nodes()

	tenantInformer := seteraFactory.
		Setera().
		V1().
		Tenants()

	controller := tenantcontroller.New(
		logger,
		seteraClient,
		kubeClient,
		tenantInformer.Informer(),
		tenantInformer.Lister(),
		nodeInformer.Informer(),
		nodeInformer.Lister(),
	)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Request informers before Start so the factories start all required caches.
	coreFactory.Start(runCtx.Done())
	seteraFactory.Start(runCtx.Done())

	controllerErr := make(chan error, 1)

	go func() {
		controllerErr <- controller.Run(runCtx)
	}()

	webhookErr := make(chan error, 1)

	go func() {
		webhookErr <- webhookServer.Run(runCtx)
	}()

	logger.Info(
		"orchestrator started",
		"webhookService",
		webhook.WebhookServiceName,
		"webhookNamespace",
		cfg.webhookNamespace,
	)

	defer logger.Info("orchestrator stopped")

	select {
	case <-ctx.Done():
		return nil

	case err := <-controllerErr:
		cancel()

		if err == nil {
			return fmt.Errorf(
				"tenant controller stopped unexpectedly",
			)
		}

		if ctx.Err() != nil {
			return nil
		}

		return fmt.Errorf(
			"tenant controller stopped: %w",
			err,
		)

	case err := <-webhookErr:
		cancel()

		if err == nil {
			return fmt.Errorf(
				"webhook server stopped unexpectedly",
			)
		}

		if ctx.Err() != nil {
			return nil
		}

		return fmt.Errorf(
			"webhook server stopped: %w",
			err,
		)
	}
}
