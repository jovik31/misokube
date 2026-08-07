package main

import (
	"context"
	"os"
	"time"

	// internal
	orch "github/setera/internal/orchestrator"
	op "github/setera/pkg/operator"

	// clients/informers
	"github/setera/pkg/generated/clientset/versioned"
	seterainformers "github/setera/pkg/generated/informers/externalversions"
	seterav1informers "github/setera/pkg/generated/informers/externalversions/setera.com/v1"
	"github/setera/pkg/k8s"

	seterav1 "github/setera/pkg/api/setera.com/v1"
	// logging + signals
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

func main() {

	// Initialize command line flags
	startedAt := time.Now()

	ctx := signals.SetupSignalHandler()
	logger := klog.FromContext(ctx).WithName("orchestrator-main")
	logger.Info("startup began")

	stageStart := startedAt

	config, err := k8s.InitKubeConfig()
	if err != nil {
		logger.Error(err, "failed to fetch kubeconfig")
		os.Exit(1)
	}
	logger.Info("startup stage complete", "stage", "init kubeconfig", "stageDuration", time.Since(stageStart), "totalElapsed", time.Since(startedAt))

	// Check if kubeclientset is nil
	if config == nil {
		logger.Error(nil, "kubeconfig is nil, cannot proceed")
		os.Exit(1)
	} else {
		logger.Info("INIT KUBECONFIG SUCCESSFUL")
	}
	stageStart = time.Now()

	kubeclient, seteraClient, err := k8s.InitClients(config)
	if err != nil {
		logger.Error(err, "failed to initialize clients")
		os.Exit(1)
	}
	logger.Info("startup stage complete", "stage", "init clients", "stageDuration", time.Since(stageStart), "totalElapsed", time.Since(startedAt))
	stageStart = time.Now()

	metricsClient, err := initMetricsClient(config)
	if err != nil {
		logger.Error(err, "failed to initialize metrics client")
		os.Exit(1)
	}
	logger.Info("startup stage complete", "stage", "init metrics client", "stageDuration", time.Since(stageStart), "totalElapsed", time.Since(startedAt))
	stageStart = time.Now()

	// Shared informer factory for Setera CRDs
	factory := seterainformers.NewSharedInformerFactory(seteraClient, 0)
	// Use NamespaceAll to cover cluster-scoped and namespaced resources
	v1 := seterav1informers.New(factory, metav1.NamespaceNone, nil)
	tenantInf := v1.Tenants().Informer()
	tenantLister := v1.Tenants().Lister()
	nodeStoreInf := v1.NodeStores().Informer()
	nodeStoreLister := v1.NodeStores().Lister()
	logger.Info("startup stage complete", "stage", "build informers", "stageDuration", time.Since(stageStart), "totalElapsed", time.Since(startedAt))
	stageStart = time.Now()

	// deploy default tenant
	err = ensureDefaultTenant(ctx, seteraClient, kubeclient, logger)
	if err != nil {
		logger.Error(err, "failed to ensure default tenant")
		os.Exit(1)
	}
	logger.Info("startup stage complete", "stage", "ensure default tenant", "stageDuration", time.Since(stageStart), "totalElapsed", time.Since(startedAt))
	stageStart = time.Now()

	// Base operator
	base := op.NewBaseOperator("orchestrator", logger, nil)

	// Construct orchestrator operator
	orchOp := orch.New(
		base,
		logger,
		nil, // recorder (optional)
		seteraClient,
		kubeclient,
		metricsClient,
		tenantInf,
		tenantLister,
		nodeStoreInf,
		nodeStoreLister,
	)
	if orchOp == nil {
		logger.Error(nil, "failed to construct orchestrator operator")
		os.Exit(1)
	}
	logger.Info("startup stage complete", "stage", "construct orchestrator", "stageDuration", time.Since(stageStart), "totalElapsed", time.Since(startedAt))

	// Start informers then run the operator
	factory.Start(ctx.Done())
	if err := orchOp.Run(ctx, startedAt); err != nil {
		logger.Error(err, "orchestrator failed to run")
		os.Exit(1)
	}
}

func initMetricsClient(config interface{}) (*metricsclient.Clientset, error) {
	metricsClient, err := metricsclient.NewForConfig(config.(*rest.Config))
	if err != nil {
		return nil, err
	}
	return metricsClient, nil
}

func ensureDefaultTenant(ctx context.Context, seteraclient versioned.Interface, kubeclient *kubernetes.Clientset, logger klog.Logger) error {

	_, err := seteraclient.SeteraV1().Tenants(metav1.NamespaceNone).Get(ctx, "default", metav1.GetOptions{})
	if err == nil {
		logger.Info("default tenant already exists; skipping creation")
		return nil
	}

	//get number of nodes - zones
	nodes, err := kubeclient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.Error(err, "failed to list nodes for default tenant creation")
		return err
	}

	// create default tenant

	tn := &seterav1.Tenant{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Tenant",
			APIVersion: seterav1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: metav1.NamespaceNone,
		},
		Spec: seterav1.TenantSpec{
			Name:  "default",
			Zones: len(nodes.Items),
		},
	}

	_, err = seteraclient.SeteraV1().Tenants(metav1.NamespaceNone).Create(ctx, tn, metav1.CreateOptions{})
	if err != nil {
		logger.Error(err, "failed to create default tenant")
		return err
	}

	logger.Info("default tenant created successfully")
	return nil

}
