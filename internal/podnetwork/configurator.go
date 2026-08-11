package podnetwork

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github/setera/internal/nodeipam"
	"github/setera/pkg/network"
)

var (
	ErrInvalidDependency = errors.New("podnetwork: invalid dependency")
	ErrInvalidConfig     = errors.New("podnetwork: invalid config")
	ErrInvalidRequest    = errors.New("podnetwork: invalid request")
	ErrAllocationMissing = errors.New("podnetwork: allocation not found")

	// podLinkGateway is a synthetic next-hop visible only inside each Pod
	// network namespace. The host veth does not own this address.
	podLinkGateway = netip.MustParseAddr("169.254.1.1")
)

// nodeIPAM is the part of NodeIPAM used by the pod network configurator.
type nodeIPAM interface {
	Allocate(context.Context, nodeipam.Request) (nodeipam.Allocation, error)
	Release(context.Context, nodeipam.Owner) error
	Get(nodeipam.Owner) (nodeipam.Allocation, bool)
	List() []nodeipam.Allocation
}

// networkOps is the Linux network API used by the configurator.
// pkg/network keeps netlink and network namespace types behind this boundary.
type networkOps interface {
	SetupVeth(
		netnsPath string,
		ifName string,
		podIP netip.Addr,
		gateway netip.Addr,
		mtu int,
	) (network.Veth, error)
	DeleteVeth(netnsPath, ifName string) error
	CheckVeth(netnsPath, ifName string, podIP netip.Addr) error
	FindPodVeth(podIP netip.Addr) (network.Veth, error)
}

// localDatapath is the local pod datapath API required by the configurator.
type localDatapath interface {
	AddLocalPod(context.Context, LocalPod) error
	DeleteLocalPod(context.Context, netip.Addr) error
	RecoverLocalPod(context.Context, LocalPod) error
}

// Configurator creates and removes the network state for local pods.
type Configurator struct {
	ipam     nodeIPAM
	network  networkOps
	datapath localDatapath

	podGateway netip.Addr
	mtu        int
}

// New creates a local pod network configurator.
func New(
	ipam nodeIPAM,
	network networkOps,
	datapath localDatapath,
	mtu int,
) (*Configurator, error) {
	if ipam == nil {
		return nil, fmt.Errorf("%w: node IPAM is nil", ErrInvalidDependency)
	}
	if network == nil {
		return nil, fmt.Errorf("%w: network operations are nil", ErrInvalidDependency)
	}
	if datapath == nil {
		return nil, fmt.Errorf("%w: datapath is nil", ErrInvalidDependency)
	}
	if mtu <= 0 {
		return nil, fmt.Errorf("%w: invalid MTU %d", ErrInvalidConfig, mtu)
	}

	return &Configurator{
		ipam:       ipam,
		network:    network,
		datapath:   datapath,
		podGateway: podLinkGateway,
		mtu:        mtu,
	}, nil
}

// AddPod allocates an address, configures the pod veth pair, and installs the
// local datapath state. On failure, it rolls back completed steps.
func (c *Configurator) AddPod(ctx context.Context, req Request) (Result, error) {
	if err := validateAddRequest(req); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	owner := ownerFromRequest(req)
	allocation, err := c.ipam.Allocate(ctx, nodeipam.Request{
		Owner:    owner,
		PodUID:   req.PodUID,
		TenantID: req.TenantID,
	})
	if err != nil {
		return Result{}, fmt.Errorf("allocate pod IP: %w", err)
	}

	veth, err := c.network.SetupVeth(
		req.NetNS,
		req.IfName,
		allocation.IP,
		c.podGateway,
		c.mtu,
	)
	if err != nil {
		releaseErr := c.ipam.Release(ctx, owner)
		if releaseErr != nil {
			return Result{}, errors.Join(
				fmt.Errorf("setup pod network: %w", err),
				fmt.Errorf("rollback pod IP %s: %w", allocation.IP, releaseErr),
			)
		}
		return Result{}, fmt.Errorf("setup pod network: %w", err)
	}

	pod := LocalPod{
		IP:              allocation.IP,
		PodUID:          allocation.PodUID,
		TenantID:        allocation.TenantID,
		HostVethName:    veth.HostName,
		HostVethIfIndex: veth.HostIfIndex,
	}

	if err := c.datapath.AddLocalPod(ctx, pod); err != nil {
		deleteErr := c.network.DeleteVeth(req.NetNS, req.IfName)
		releaseErr := c.ipam.Release(ctx, owner)

		return Result{}, errors.Join(
			fmt.Errorf("configure local datapath: %w", err),
			wrapError("rollback pod network", deleteErr),
			wrapError("rollback pod IP", releaseErr),
		)
	}

	return Result{
		IP:              allocation.IP,
		Gateway:         c.podGateway,
		HostVethName:    veth.HostName,
		HostVethIfIndex: veth.HostIfIndex,
	}, nil
}

// DelPod removes datapath and network state before it releases the IP.
// NetNS may be empty because CNI DEL can run after the pod namespace is gone.
func (c *Configurator) DelPod(ctx context.Context, req Request) error {
	if err := validateOwnerRequest(req); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	owner := ownerFromRequest(req)
	allocation, ok := c.ipam.Get(owner)
	if !ok {
		return nil
	}

	var datapathErr error
	if err := c.datapath.DeleteLocalPod(ctx, allocation.IP); err != nil {
		datapathErr = fmt.Errorf("delete local datapath: %w", err)
	}

	var networkErr error
	if err := c.network.DeleteVeth(req.NetNS, req.IfName); err != nil {
		networkErr = fmt.Errorf("delete pod network: %w", err)
	}

	// Keep the allocation if required cleanup failed. This prevents the IP from
	// being reused while stale network or datapath state can still exist.
	if datapathErr != nil || networkErr != nil {
		return errors.Join(datapathErr, networkErr)
	}

	if err := c.ipam.Release(ctx, owner); err != nil {
		return fmt.Errorf("release pod IP %s: %w", allocation.IP, err)
	}

	return nil
}

// CheckPod verifies that the allocation exists and that the pod interface has
// the expected IP address.
func (c *Configurator) CheckPod(ctx context.Context, req Request) error {
	if err := validateCheckRequest(req); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	allocation, ok := c.ipam.Get(ownerFromRequest(req))
	if !ok {
		return ErrAllocationMissing
	}

	if err := c.network.CheckVeth(req.NetNS, req.IfName, allocation.IP); err != nil {
		return fmt.Errorf("check pod network: %w", err)
	}

	return nil
}

func ownerFromRequest(req Request) nodeipam.Owner {
	return nodeipam.Owner{
		ContainerID: req.ContainerID,
		IfName:      req.IfName,
	}
}

func validateOwnerRequest(req Request) error {
	if req.ContainerID == "" {
		return fmt.Errorf("%w: container ID is empty", ErrInvalidRequest)
	}
	if req.IfName == "" {
		return fmt.Errorf("%w: interface name is empty", ErrInvalidRequest)
	}
	return nil
}

func validateAddRequest(req Request) error {
	if err := validateOwnerRequest(req); err != nil {
		return err
	}
	if req.NetNS == "" {
		return fmt.Errorf("%w: network namespace is empty", ErrInvalidRequest)
	}
	if req.PodUID == "" {
		return fmt.Errorf("%w: pod UID is empty", ErrInvalidRequest)
	}
	if req.TenantID == "" {
		return fmt.Errorf("%w: tenant ID is empty", ErrInvalidRequest)
	}
	return nil
}

func validateCheckRequest(req Request) error {
	if err := validateOwnerRequest(req); err != nil {
		return err
	}
	if req.NetNS == "" {
		return fmt.Errorf("%w: network namespace is empty", ErrInvalidRequest)
	}
	return nil
}

func wrapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}