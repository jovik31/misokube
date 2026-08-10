package cniserver

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"

	types100 "github.com/containernetworking/cni/pkg/types/100"

	"github/setera/internal/podnetwork"
	"github/setera/pkg/wire"
)

func encodeAddResult(
	defaultVersion string,
	requestedVersion string,
	req *wire.Request,
	result podnetwork.Result,
) (json.RawMessage, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if !result.IP.IsValid() {
		return nil, fmt.Errorf("pod IP is invalid")
	}

	version := requestedVersion
	if version == "" {
		version = defaultVersion
	}
	if version == "" {
		version = fallbackCNIVersion
	}

	address, err := hostPrefix(result.IP)
	if err != nil {
		return nil, err
	}

	interfaces := []*types100.Interface{
		{
			Name: result.HostVethName,
		},
		{
			Name:    req.IfName,
			Sandbox: req.NetNS,
		},
	}

	podInterface := 1
	current := &types100.Result{
		CNIVersion: version,
		Interfaces: interfaces,
		IPs: []*types100.IPConfig{
			{
				Interface: &podInterface,
				Address:   address,
			},
		},
	}

	converted, err := current.GetAsVersion(version)
	if err != nil {
		return nil, fmt.Errorf("convert result to CNI version %s: %w", version, err)
	}

	data, err := json.Marshal(converted)
	if err != nil {
		return nil, fmt.Errorf("marshal CNI result: %w", err)
	}

	return data, nil
}

func hostPrefix(addr netip.Addr) (net.IPNet, error) {
	if !addr.IsValid() {
		return net.IPNet{}, fmt.Errorf("invalid IP address")
	}
	if addr.Is6() && addr.Zone() != "" {
		return net.IPNet{}, fmt.Errorf("zoned IP address is not supported")
	}

	addr = addr.Unmap()

	if addr.Is4() {
		return net.IPNet{
			IP:   net.IP(addr.AsSlice()),
			Mask: net.CIDRMask(32, 32),
		}, nil
	}

	return net.IPNet{
		IP:   net.IP(addr.AsSlice()),
		Mask: net.CIDRMask(128, 128),
	}, nil
}
