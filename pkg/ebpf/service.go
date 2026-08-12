package ebpf

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github/setera/pkg/ebpf/loader"
)

const (
	ServiceProtocolTCP uint8 = 6
	ServiceProtocolUDP uint8 = 17

	maxServiceTenantBytes = 63
)

// ServiceKey identifies one IPv4 Service frontend.
type ServiceKey struct {
	IP       netip.Addr
	Port     uint16
	Protocol uint8
}

// ServiceBackend is one ready backend for a Service frontend.
//
// ManagedPod is true only when the backend is a Setera-managed Pod. TenantID
// is required for managed Pod backends and empty for external backends.
type ServiceBackend struct {
	IP         netip.Addr
	Port       uint16
	TenantID   string
	ManagedPod bool
}

// Service is one Service frontend and its ready backends.
type Service struct {
	Key      ServiceKey
	Backends []ServiceBackend
}

// ServiceMap owns Setera's node-local Service frontend and backend maps.
//
// The cgroup socket load balancer consumes these maps for the primary IPv4
// ClusterIP path. kube-proxy remains enabled as the fallback for Service paths
// Setera does not translate yet.
type ServiceMap struct {
	handle *loader.ServiceMap
}

// OpenServiceMap opens or creates the pinned Service maps.
func OpenServiceMap() (*ServiceMap, error) {
	handle, err := loader.OpenServiceMap()
	if err != nil {
		return nil, fmt.Errorf("ebpf: open Service maps: %w", err)
	}
	return &ServiceMap{handle: handle}, nil
}

// Close releases the userspace map handles. The pinned maps remain available
// to future eBPF Service programs and survive daemon restart.
func (m *ServiceMap) Close() error {
	if m == nil || m.handle == nil {
		return nil
	}
	if err := m.handle.Close(); err != nil {
		return fmt.Errorf("ebpf: close Service maps: %w", err)
	}
	return nil
}

// ReconcileServices makes the Service maps match an authoritative snapshot.
func (m *ServiceMap) ReconcileServices(
	ctx context.Context,
	services []Service,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil || m.handle == nil {
		return fmt.Errorf("ebpf: Service map is not open")
	}

	converted, err := servicesToLoader(services)
	if err != nil {
		return err
	}
	if err := m.handle.ReconcileServices(converted); err != nil {
		return fmt.Errorf("ebpf: reconcile Service maps: %w", err)
	}
	return nil
}

func servicesToLoader(services []Service) ([]loader.Service, error) {
	out := make([]loader.Service, 0, len(services))
	for _, service := range services {
		converted, err := serviceToLoader(service)
		if err != nil {
			return nil, err
		}
		out = append(out, converted)
	}
	return out, nil
}

func serviceToLoader(service Service) (loader.Service, error) {
	key, err := serviceKeyToLoader(service.Key)
	if err != nil {
		return loader.Service{}, err
	}

	backends := make([]loader.ServiceBackend, 0, len(service.Backends))
	for _, backend := range service.Backends {
		ip := backend.IP.Unmap()
		if !ip.IsValid() || !ip.Is4() || ip.Zone() != "" {
			return loader.Service{}, fmt.Errorf(
				"ebpf: invalid Service backend IPv4 address %s",
				ip,
			)
		}
		if backend.Port == 0 {
			return loader.Service{}, fmt.Errorf(
				"ebpf: Service backend port is zero",
			)
		}

		tenant := strings.TrimSpace(backend.TenantID)
		if backend.ManagedPod {
			if tenant == "" {
				return loader.Service{}, fmt.Errorf(
					"ebpf: managed Service backend tenant is empty",
				)
			}
			if len(tenant) > maxServiceTenantBytes {
				return loader.Service{}, fmt.Errorf(
					"ebpf: Service backend tenant %q is too long: got %d bytes, maximum is %d",
					tenant,
					len(tenant),
					maxServiceTenantBytes,
				)
			}
		} else if tenant != "" {
			return loader.Service{}, fmt.Errorf(
				"ebpf: external Service backend tenant must be empty",
			)
		}

		backends = append(backends, loader.ServiceBackend{
			IP:         net.IP(ip.AsSlice()),
			Port:       backend.Port,
			Tenant:     tenant,
			ManagedPod: backend.ManagedPod,
		})
	}

	return loader.Service{
		Key:      key,
		Backends: backends,
	}, nil
}

func serviceKeyToLoader(key ServiceKey) (loader.ServiceKey, error) {
	ip := key.IP.Unmap()
	if !ip.IsValid() || !ip.Is4() || ip.Zone() != "" {
		return loader.ServiceKey{}, fmt.Errorf(
			"ebpf: invalid Service frontend IPv4 address %s",
			ip,
		)
	}
	if key.Port == 0 {
		return loader.ServiceKey{}, fmt.Errorf(
			"ebpf: Service frontend port is zero",
		)
	}
	if key.Protocol != ServiceProtocolTCP &&
		key.Protocol != ServiceProtocolUDP {
		return loader.ServiceKey{}, fmt.Errorf(
			"ebpf: unsupported Service protocol %d",
			key.Protocol,
		)
	}

	return loader.ServiceKey{
		IP:       net.IP(ip.AsSlice()),
		Port:     key.Port,
		Protocol: key.Protocol,
	}, nil
}
