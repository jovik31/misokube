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

// setera packages

// k8s packages

// This file is part of the Setera project, which is released under the GNU General Public License v3.0 (GPL-3.0).
// See the LICENSE file in the project root for license information.

import (

	//std
	"context"
	"fmt"

	// internal packages
	"github/setera/internal/daemon/service"

	doperator "github/setera/internal/daemon/operator"
	"github/setera/pkg/generated/clientset/versioned"

	"k8s.io/client-go/kubernetes"
)

type Daemon struct {
	NodeStoreOperator *doperator.NodeStoreOperator // manages local nodestore and tenant interactions

	NetworkService *service.NetworkService // manages tenant network configurations

	//CNIServer *daemon.SocketServer // provides CNI plugin communication interface

	//scoreClient http.Client // HTTP client for score API interactions
}

func NewDaemon(
	ctx context.Context,
	componentName string,
	nodeName string,
	nodeIP string,
	nodeCIDR string,
	seteraclientset versioned.Interface,
	kubeclientset kubernetes.Interface,

) (*Daemon, error) {

	netService, err := service.NewNetworkService(nodeCIDR, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to create network service: %w", err)

	}
	return &Daemon{
		NodeStoreOperator: doperator.NewNodeStoreOperator(ctx, componentName, nodeName, nodeIP, seteraclientset, kubeclientset, netService),
		NetworkService:    netService,
		//CNIServer:         daemon.NewSocketServer(),
		//scoreClient:       http.Client{},
	}, nil
}
