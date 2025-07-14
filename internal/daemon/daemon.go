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

	"github/setera/pkg/data/network"
	seterav1clientset "github/setera/pkg/generated/clientset/versioned"
	informers "github/setera/pkg/generated/informers/externalversions/setera.com/v1"
	listers "github/setera/pkg/generated/listers/setera.com/v1"
	"github/setera/pkg/server"

	daemon "github/setera/internal/daemon/operator"

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
	NodeID string // Unique identifier for the node
	NodeIP net.IP // IP address of the node

	NetworkManager *network.NetworkManager // Network Manager handles IP allocation and subnet management

	CNIServer *CNIServer

	NodeStoreController *NodeStoreController

	ScoreReporter *ScoreReporter
}

func NewDaemon(
	nodeID string,
	nodeIP net.IP,
	networkManager *network.NetworkManager,
	cniServer *server.CNIServer,
	nodeStoreController *NodeStoreController,
	scoreReporter *ScoreReporter,
) (*Daemon, error) {

	netMgr, err := network.NewNetworkManager(networkManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create network manager: %w", err)
	}
	cniSrv := server.NewCNIServer(cniServer.SocketPath, cniServer.Logger)
	if err := cniSrv.Start(); err != nil {
		return nil, fmt.Errorf("failed to start CNI server: %w", err)
	}

	scoreReporter := NewScoreReporter(scoreReporter.KubeClientset, scoreReporter.Logger)

	nodeStoreController := NewNodeStoreController(nodeStoreController.KubeClientset, nodeStoreController.SeteraClientset, nodeStoreController.Logger)

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
	k8sController := daemon.NewNodeStoreOperator(
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
