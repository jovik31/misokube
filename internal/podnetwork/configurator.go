package podnetwork

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"

	"github/setera/internal/nodeipam"
	"github/setera/pkg/network/device"
)

var (
	ErrInvalidDependency = errors.New("podnetwork: invalid dependency")
	ErrInvalidConfig     = errors.New("podnetwork: invalid config")
	ErrInvalidRequest    = errors.New("podnetwork: invalid request")
	ErrAllocationMissing = errors.New("podnetwork: allocation not found")
)

// nodeIPAM is the part of NodeIPAM used by the pod network configurator.
type nodeIPAM interface {
	Allocate(context.Context, nodeipam.Request) (nodeipam.Allocation, error)
	Release(context.Context, nodeipam.Owner) error
	Get(nodeipam.Owner) (nodeipam.Allocation, bool)
}

// localDatapath is the local pod datapath API required by the configurator.
type localDatapath interface {
	AddLocalPod(context.Context, LocalPod) error
	DeleteLocalPod(context.Context, netip.Addr) error
}

type networkState struct {
	hostVethName    string
	hostVethIfIndex int
}

// networkOps contains the Linux network operations required by Configurator.
// It is an interface so transaction behavior can be tested without creating
// real network namespaces.
type networkOps interface {
	Setup(netnsPath, ifName string, podIP, gateway netip.Addr, mtu int) (networkState, error)
	Delete(netnsPath, ifName string) error
	Check(netnsPath, ifName string, podIP netip.Addr) error
}

// Configurator creates and removes the network state for local pods.
type Configurator struct {
	ipam     nodeIPAM
	network  networkOps
	datapath localDatapath

	hostGateway netip.Addr
	mtu         int
}

// New creates a local pod network configurator.
func New(ipam nodeIPAM, datapath localDatapath, hostGateway netip.Addr, mtu int) (*Configurator, error) {
	return newConfigurator(ipam, linuxNetworkOps{}, datapath, hostGateway, mtu)
}

func newConfigurator(
	ipam nodeIPAM,
	network networkOps,
	datapath localDatapath,
	hostGateway netip.Addr,
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
	if !hostGateway.IsValid() || !hostGateway.Is4() || hostGateway.IsUnspecified() {
		return nil, fmt.Errorf("%w: invalid IPv4 host gateway %s", ErrInvalidConfig, hostGateway)
	}
	if mtu <= 0 {
		return nil, fmt.Errorf("%w: invalid MTU %d", ErrInvalidConfig, mtu)
	}

	return &Configurator{
		ipam:        ipam,
		network:     network,
		datapath:    datapath,
		hostGateway: hostGateway.Unmap(),
		mtu:         mtu,
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

	state, err := c.network.Setup(
		req.NetNS,
		req.IfName,
		allocation.IP,
		c.hostGateway,
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
		HostVethName:    state.hostVethName,
		HostVethIfIndex: state.hostVethIfIndex,
	}

	if err := c.datapath.AddLocalPod(ctx, pod); err != nil {
		deleteErr := c.network.Delete(req.NetNS, req.IfName)
		releaseErr := c.ipam.Release(ctx, owner)

		return Result{}, errors.Join(
			fmt.Errorf("configure local datapath: %w", err),
			wrapError("rollback pod network", deleteErr),
			wrapError("rollback pod IP", releaseErr),
		)
	}

	return Result{
		IP:              allocation.IP,
		HostVethName:    state.hostVethName,
		HostVethIfIndex: state.hostVethIfIndex,
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
	if err := c.network.Delete(req.NetNS, req.IfName); err != nil {
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

	if err := c.network.Check(req.NetNS, req.IfName, allocation.IP); err != nil {
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

// linuxNetworkOps uses the existing direct-veth implementation.
type linuxNetworkOps struct{}

func (linuxNetworkOps) Setup(
	netnsPath,
	ifName string,
	podIP,
	gateway netip.Addr,
	mtu int,
) (networkState, error) {
	if !podIP.Is4() {
		return networkState{}, fmt.Errorf("only IPv4 pod addresses are supported: %s", podIP)
	}

	podNet := &net.IPNet{
		IP:   addrToIP(podIP),
		Mask: net.CIDRMask(32, 32),
	}

	hostVethName, err := device.SetupVethDirect(
		netnsPath,
		mtu,
		ifName,
		podNet,
		addrToIP(gateway),
	)
	if err != nil {
		cleanupErr := linuxNetworkOps{}.Delete(netnsPath, ifName)
		return networkState{}, errors.Join(
			fmt.Errorf("setup direct veth: %w", err),
			wrapError("cleanup partial pod veth", cleanupErr),
		)
	}

	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil {
		cleanupErr := linuxNetworkOps{}.Delete(netnsPath, ifName)
		return networkState{}, errors.Join(
			fmt.Errorf("lookup host veth %q: %w", hostVethName, err),
			wrapError("cleanup pod veth", cleanupErr),
		)
	}

	return networkState{
		hostVethName:    hostVethName,
		hostVethIfIndex: hostVeth.Attrs().Index,
	}, nil
}

func (linuxNetworkOps) Delete(netnsPath, ifName string) error {
	if netnsPath == "" {
		return nil
	}

	netns, err := ns.GetNS(netnsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open netns %q: %w", netnsPath, err)
	}
	defer netns.Close()

	return device.DelVeth(netns, ifName)
}

func (linuxNetworkOps) Check(netnsPath, ifName string, podIP netip.Addr) error {
	netns, err := ns.GetNS(netnsPath)
	if err != nil {
		return fmt.Errorf("open netns %q: %w", netnsPath, err)
	}
	defer netns.Close()

	return device.CheckVeth(netns, ifName, addrToIP(podIP))
}

func addrToIP(addr netip.Addr) net.IP {
	addr = addr.Unmap()
	if addr.Is4() {
		v := addr.As4()
		return net.IPv4(v[0], v[1], v[2], v[3])
	}
	v := addr.As16()
	return net.IP(v[:])
}
