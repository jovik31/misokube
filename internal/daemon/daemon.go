/*
Package daemon implements the Setera daemon that runs on each node.

The daemon is responsible for:
1. Managing network resources for tenants on the local node
2. Providing a socket server for CNI plugin communication
3. Watching Kubernetes resources (Tenants and NodeStores) for changes
4. Coordinating with the trie-based IP allocation system

Architecture:
- NetworkManager: Handles IP allocation and subnet management using the trie data structure
- SocketServer: Provides a Unix domain socket for CNI plugin communication
- K8sController: Encapsulates Kubernetes operations including resource watching,
  client management, and event processing through workqueues

The daemon integrates with the broader Setera system to ensure network isolation
between tenants while providing efficient IP address management. The modular design
with separate controllers makes the code more maintainable and testable.
*/

package daemon

import (
	"context"
	"fmt"
	"net"

	// setera packages
	seterav1 "github/setera/pkg/api/setera.com/v1"
	seterav1clientset "github/setera/pkg/generated/clientset/versioned"
	informers "github/setera/pkg/generated/informers/externalversions/setera.com/v1"
	listers "github/setera/pkg/generated/listers/setera.com/v1"
	"github/setera/pkg/server"

	// k8s packages
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// This file is part of the Setera project, which is released under the GNU General Public License v3.0 (GPL-3.0).
// See the LICENSE file in the project root for license information.

// K8sController manages Kubernetes resources and operations
type K8sController struct {
	// K8s clients for interacting with cluster resources
	kubeClientset   kubernetes.Interface
	seteraClientset seterav1clientset.Interface

	// Informers and listers for watching resources
	tenantInformer    cache.SharedIndexInformer
	tenantLister      listers.TenantLister
	nodestoreInformer cache.SharedIndexInformer
	nodestoreLister   listers.NodeStoreLister

	// Workqueue for processing events
	workqueue workqueue.RateLimitingInterface

	// Event recorder and logger
	recorder record.EventRecorder
	logger   klog.Logger
}

type Daemon struct {
	// Network Manager handles IP allocation and subnet management
	//networkManager *network.NetworkManager

	// Socket Server for CNI plugin communication
	socketServer *server.SocketServer

	// K8s Controller manages Kubernetes resources and operations
	k8sController *K8sController

	// Node information
	nodeName string
	nodeIP   net.IP
}

// NewK8sController creates a new K8s controller instance
func NewK8sController(
	kubeClientset kubernetes.Interface,
	seteraClientset seterav1clientset.Interface,
	tenantInformer informers.TenantInformer,
	nodestoreInformer informers.NodeStoreInformer,
	recorder record.EventRecorder,
	logger klog.Logger,
) *K8sController {
	return &K8sController{
		kubeClientset:     kubeClientset,
		seteraClientset:   seteraClientset,
		tenantInformer:    tenantInformer.Informer(),
		tenantLister:      tenantInformer.Lister(),
		nodestoreInformer: nodestoreInformer.Informer(),
		nodestoreLister:   nodestoreInformer.Lister(),
		workqueue:         workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder:          recorder,
		logger:            logger,
	}
}

// NewDaemon creates a new daemon instance
func NewDaemon(
	ctx context.Context,
	kubeClientset kubernetes.Interface,
	seteraClientset seterav1clientset.Interface,
	tenantInformer informers.TenantInformer,
	nodestoreInformer informers.NodeStoreInformer,
	//networkManager *network.NetworkManager,
	nodeName string,
	nodeIP net.IP,
	socketPath string,
) *Daemon {

	logger := klog.FromContext(ctx)
	socketServer := server.NewSocketServer(socketPath, logger)

	// Create K8s controller
	k8sController := NewK8sController(
		kubeClientset,
		seteraClientset,
		tenantInformer,
		nodestoreInformer,
		nil, // recorder - can be initialized later if needed
		logger,
	)

	return &Daemon{
		//networkManager: networkManager,
		socketServer:  socketServer,
		k8sController: k8sController,
		nodeName:      nodeName,
		nodeIP:        nodeIP,
	}
}

// Run starts the daemon and all its components
func (d *Daemon) Run(ctx context.Context) error {
	defer d.k8sController.workqueue.ShutDown()

	d.k8sController.logger.Info("Starting Setera daemon", "node", d.nodeName, "nodeIP", d.nodeIP.String())

	// Start the socket server for CNI communication
	go func() {
		if err := d.startSocketServer(ctx); err != nil {
			d.k8sController.logger.Error(err, "Failed to start socket server")
		}
	}()

	// Set up event handlers for tenant and nodestore resources
	d.k8sController.tenantInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    d.handleTenantAdd,
		UpdateFunc: d.handleTenantUpdate,
		DeleteFunc: d.handleTenantDelete,
	})

	d.k8sController.nodestoreInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    d.handleNodestoreAdd,
		UpdateFunc: d.handleNodestoreUpdate,
		DeleteFunc: d.handleNodestoreDelete,
	})

	// Wait for cache to sync
	if ok := cache.WaitForCacheSync(ctx.Done(), d.k8sController.tenantInformer.HasSynced, d.k8sController.nodestoreInformer.HasSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	// Start workers
	go d.runWorker(ctx)

	d.k8sController.logger.Info("Daemon started successfully")
	<-ctx.Done()
	d.k8sController.logger.Info("Shutting down daemon")

	return nil
}

// startSocketServer initializes and starts the socket server for CNI communication
func (d *Daemon) startSocketServer(ctx context.Context) error {
	if err := d.socketServer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start socket server: %w", err)
	}

	d.k8sController.logger.Info("Socket server started successfully")

	// Wait for context cancellation to stop the server
	<-ctx.Done()

	if err := d.socketServer.Stop(); err != nil {
		d.k8sController.logger.Error(err, "Error stopping socket server")
	}

	return nil
}

// runWorker processes items from the workqueue
func (d *Daemon) runWorker(ctx context.Context) {
	for d.processNextWorkItem() {
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// processNextWorkItem processes the next item in the workqueue
func (d *Daemon) processNextWorkItem() bool {
	obj, shutdown := d.k8sController.workqueue.Get()
	if shutdown {
		return false
	}

	defer d.k8sController.workqueue.Done(obj)

	key, ok := obj.(string)
	if !ok {
		d.k8sController.workqueue.Forget(obj)
		d.k8sController.logger.Error(fmt.Errorf("expected string in workqueue but got %#v", obj), "Invalid object in workqueue")
		return true
	}

	if err := d.processItem(key); err != nil {
		d.k8sController.workqueue.AddRateLimited(key)
		d.k8sController.logger.Error(err, "Error processing item", "key", key)
		return true
	}

	d.k8sController.workqueue.Forget(obj)
	return true
}

// processItem processes a single item from the workqueue
func (d *Daemon) processItem(key string) error {
	// [ ]TODO: Implement item processing logic
	d.k8sController.logger.Info("Processing item", "key", key)
	return nil
}

// Event handlers for tenant resources
func (d *Daemon) handleTenantAdd(obj interface{}) {
	tenant, ok := obj.(*seterav1.Tenant)
	if !ok {
		d.k8sController.logger.Error(nil, "Failed to cast object to Tenant")
		return
	}

	d.k8sController.logger.Info("Tenant added", "tenant", tenant.Name, "namespace", tenant.Namespace)
	key, _ := cache.MetaNamespaceKeyFunc(obj)
	d.k8sController.workqueue.Add(fmt.Sprintf("tenant:add:%s", key))
}

func (d *Daemon) handleTenantUpdate(oldObj, newObj interface{}) {
	tenant, ok := newObj.(*seterav1.Tenant)
	if !ok {
		d.k8sController.logger.Error(nil, "Failed to cast object to Tenant")
		return
	}

	d.k8sController.logger.Info("Tenant updated", "tenant", tenant.Name, "namespace", tenant.Namespace)
	key, _ := cache.MetaNamespaceKeyFunc(newObj)
	d.k8sController.workqueue.Add(fmt.Sprintf("tenant:update:%s", key))
}

func (d *Daemon) handleTenantDelete(obj interface{}) {
	tenant, ok := obj.(*seterav1.Tenant)
	if !ok {
		d.k8sController.logger.Error(nil, "Failed to cast object to Tenant")
		return
	}

	d.k8sController.logger.Info("Tenant deleted", "tenant", tenant.Name, "namespace", tenant.Namespace)
	key, _ := cache.MetaNamespaceKeyFunc(obj)
	d.k8sController.workqueue.Add(fmt.Sprintf("tenant:delete:%s", key))
}

// Event handlers for nodestore resources
func (d *Daemon) handleNodestoreAdd(obj interface{}) {
	nodestore, ok := obj.(*seterav1.NodeStore)
	if !ok {
		d.k8sController.logger.Error(nil, "Failed to cast object to NodeStore")
		return
	}

	d.k8sController.logger.Info("NodeStore added", "nodestore", nodestore.Name, "namespace", nodestore.Namespace)
	key, _ := cache.MetaNamespaceKeyFunc(obj)
	d.k8sController.workqueue.Add(fmt.Sprintf("nodestore:add:%s", key))
}

func (d *Daemon) handleNodestoreUpdate(oldObj, newObj interface{}) {
	nodestore, ok := newObj.(*seterav1.NodeStore)
	if !ok {
		d.k8sController.logger.Error(nil, "Failed to cast object to NodeStore")
		return
	}

	d.k8sController.logger.Info("NodeStore updated", "nodestore", nodestore.Name, "namespace", nodestore.Namespace)
	key, _ := cache.MetaNamespaceKeyFunc(newObj)
	d.k8sController.workqueue.Add(fmt.Sprintf("nodestore:update:%s", key))
}

func (d *Daemon) handleNodestoreDelete(obj interface{}) {
	nodestore, ok := obj.(*seterav1.NodeStore)
	if !ok {
		d.k8sController.logger.Error(nil, "Failed to cast object to NodeStore")
		return
	}

	d.k8sController.logger.Info("NodeStore deleted", "nodestore", nodestore.Name, "namespace", nodestore.Namespace)
	key, _ := cache.MetaNamespaceKeyFunc(obj)
	d.k8sController.workqueue.Add(fmt.Sprintf("nodestore:delete:%s", key))
}

// K8sController methods for easier access to internal components

// GetTenantLister returns the tenant lister
func (k *K8sController) GetTenantLister() listers.TenantLister {
	return k.tenantLister
}

// GetNodeStoreLister returns the nodestore lister
func (k *K8sController) GetNodeStoreLister() listers.NodeStoreLister {
	return k.nodestoreLister
}

// GetKubeClientset returns the Kubernetes clientset
func (k *K8sController) GetKubeClientset() kubernetes.Interface {
	return k.kubeClientset
}

// GetSeteraClientset returns the Setera clientset
func (k *K8sController) GetSeteraClientset() seterav1clientset.Interface {
	return k.seteraClientset
}

// GetLogger returns the logger instance
func (k *K8sController) GetLogger() klog.Logger {
	return k.logger
}

// AddToWorkqueue adds an item to the workqueue
func (k *K8sController) AddToWorkqueue(item string) {
	k.workqueue.Add(item)
}

// Stop gracefully stops the K8s controller
func (k *K8sController) Stop() {
	k.workqueue.ShutDown()
}

// GetNetworkManager returns the network manager instance
/*func (d *Daemon) GetNetworkManager() *network.NetworkManager {
	return d.networkManager
}*/

// GetSocketServer returns the socket server instance
func (d *Daemon) GetSocketServer() *server.SocketServer {
	return d.socketServer
}

// GetK8sController returns the K8s controller instance
func (d *Daemon) GetK8sController() *K8sController {
	return d.k8sController
}

// GetNodeName returns the node name this daemon is running on
func (d *Daemon) GetNodeName() string {
	return d.nodeName
}

// GetNodeIP returns the IP address of the node this daemon is running on
func (d *Daemon) GetNodeIP() net.IP {
	return d.nodeIP
}

// Stop gracefully stops the daemon and all its components
func (d *Daemon) Stop() error {
	d.k8sController.logger.Info("Stopping daemon")

	// Stop the socket server
	if d.socketServer != nil {
		if err := d.socketServer.Stop(); err != nil {
			d.k8sController.logger.Error(err, "Error stopping socket server")
		}
	}

	// Shutdown the workqueue
	d.k8sController.workqueue.ShutDown()

	d.k8sController.logger.Info("Daemon stopped")
	return nil
}
