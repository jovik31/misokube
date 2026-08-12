package servicewatcher

import (
	"context"
	"fmt"
	"net/netip"
	"sort"

	seteraebpf "github/setera/pkg/ebpf"
	"github/setera/pkg/tenantmeta"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

func (w *Watcher) reconcileAll(ctx context.Context) error {
	services, err := w.serviceLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list Services: %w", err)
	}

	slices, err := w.endpointSliceLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list EndpointSlices: %w", err)
	}

	pods, err := w.podLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list Pods: %w", err)
	}

	podState, err := buildPodSnapshot(pods)
	if err != nil {
		return fmt.Errorf("build Pod identity snapshot: %w", err)
	}

	slicesByService := make(map[string][]*discoveryv1.EndpointSlice)
	for _, slice := range slices {
		if slice == nil {
			continue
		}
		serviceName := slice.Labels[discoveryv1.LabelServiceName]
		if serviceName == "" {
			continue
		}
		key := slice.Namespace + "/" + serviceName
		slicesByService[key] = append(slicesByService[key], slice)
	}

	desired := make([]seteraebpf.Service, 0)
	for _, service := range services {
		if service == nil {
			continue
		}

		key := service.Namespace + "/" + service.Name
		resolved, err := buildServices(
			service,
			slicesByService[key],
			podState,
		)
		if err != nil {
			return fmt.Errorf("resolve Service %s: %w", key, err)
		}
		desired = append(desired, resolved...)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := w.datapath.ReconcileServices(ctx, desired); err != nil {
		return err
	}

	w.logger.V(2).Info(
		"Service maps reconciled",
		"services", len(services),
		"frontends", len(desired),
		"endpointSlices", len(slices),
	)
	return nil
}

func buildServices(
	service *corev1.Service,
	slices []*discoveryv1.EndpointSlice,
	pods podSnapshot,
) ([]seteraebpf.Service, error) {
	if service == nil {
		return nil, nil
	}

	clusterIPs, err := serviceIPv4ClusterIPs(service)
	if err != nil {
		return nil, err
	}
	if len(clusterIPs) == 0 {
		return nil, nil
	}

	out := make([]seteraebpf.Service, 0)

	for _, servicePort := range service.Spec.Ports {
		protocol, ok := serviceProtocol(servicePort.Protocol)
		if !ok {
			continue
		}
		if servicePort.Port <= 0 || servicePort.Port > 65535 {
			return nil, fmt.Errorf(
				"invalid Service port %d",
				servicePort.Port,
			)
		}

		backends, err := backendsForPort(
			service,
			servicePort,
			slices,
			pods,
		)
		if err != nil {
			return nil, err
		}

		for _, clusterIP := range clusterIPs {
			out = append(out, seteraebpf.Service{
				Key: seteraebpf.ServiceKey{
					IP:       clusterIP,
					Port:     uint16(servicePort.Port),
					Protocol: protocol,
				},
				Backends: append(
					[]seteraebpf.ServiceBackend(nil),
					backends...,
				),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.IP != out[j].Key.IP {
			return out[i].Key.IP.Less(out[j].Key.IP)
		}
		if out[i].Key.Port != out[j].Key.Port {
			return out[i].Key.Port < out[j].Key.Port
		}
		return out[i].Key.Protocol < out[j].Key.Protocol
	})

	return out, nil
}

func backendsForPort(
	service *corev1.Service,
	servicePort corev1.ServicePort,
	slices []*discoveryv1.EndpointSlice,
	pods podSnapshot,
) ([]seteraebpf.ServiceBackend, error) {
	type backendKey struct {
		IP   netip.Addr
		Port uint16
	}

	seen := make(map[backendKey]seteraebpf.ServiceBackend)

	for _, slice := range slices {
		if slice == nil || slice.AddressType != discoveryv1.AddressTypeIPv4 {
			continue
		}

		backendPort, ok := endpointPort(servicePort, slice.Ports)
		if !ok {
			continue
		}

		for _, endpoint := range slice.Endpoints {
			if !endpointReady(service, endpoint) {
				continue
			}

			for _, addressText := range endpoint.Addresses {
				address, err := netip.ParseAddr(addressText)
				if err != nil {
					return nil, fmt.Errorf(
						"parse EndpointSlice address %q: %w",
						addressText,
						err,
					)
				}
				address = address.Unmap()
				if !address.Is4() || address.Zone() != "" {
					continue
				}

				backend, include, err := resolveBackend(
					slice.Namespace,
					address,
					backendPort,
					endpoint,
					pods,
				)
				if err != nil {
					return nil, err
				}
				if !include {
					continue
				}

				key := backendKey{
					IP:   backend.IP,
					Port: backend.Port,
				}
				if old, exists := seen[key]; exists {
					if old.ManagedPod != backend.ManagedPod ||
						old.TenantID != backend.TenantID {
						return nil, fmt.Errorf(
							"conflicting identity for Service backend %s:%d",
							backend.IP,
							backend.Port,
						)
					}
					continue
				}

				seen[key] = backend
			}
		}
	}

	out := make([]seteraebpf.ServiceBackend, 0, len(seen))
	for _, backend := range seen {
		out = append(out, backend)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].IP != out[j].IP {
			return out[i].IP.Less(out[j].IP)
		}
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		if out[i].ManagedPod != out[j].ManagedPod {
			return out[i].ManagedPod
		}
		return out[i].TenantID < out[j].TenantID
	})

	return out, nil
}

func resolveBackend(
	sliceNamespace string,
	address netip.Addr,
	port uint16,
	endpoint discoveryv1.Endpoint,
	pods podSnapshot,
) (seteraebpf.ServiceBackend, bool, error) {
	backend := seteraebpf.ServiceBackend{
		IP:   address,
		Port: port,
	}

	ref := endpoint.TargetRef
	if ref != nil && ref.Kind == "Pod" && ref.Name != "" {
		namespace := ref.Namespace
		if namespace == "" {
			namespace = sliceNamespace
		}

		pod, ok := pods.byName[namespace+"/"+ref.Name]
		if !ok {
			// The EndpointSlice can briefly outlive its Pod. Do not publish a
			// backend whose Pod identity cannot be verified.
			return seteraebpf.ServiceBackend{}, false, nil
		}
		if ref.UID != "" && pod.uid != ref.UID {
			// Stale EndpointSlice entry for an older Pod instance.
			return seteraebpf.ServiceBackend{}, false, nil
		}
		if _, ok := pod.ipv4[address]; !ok {
			// The EndpointSlice address does not belong to the referenced Pod.
			return seteraebpf.ServiceBackend{}, false, nil
		}

		if pod.hostNetwork {
			// A host-network Pod does not have a Setera-managed Pod veth.
			return backend, true, nil
		}

		backend.ManagedPod = true
		backend.TenantID = pod.tenantID
		return backend, true, nil
	}

	// Do not trust a missing or non-Pod TargetRef as proof that the address is
	// external. A manually managed EndpointSlice can point directly at a Pod
	// IP. Resolve the address against the authoritative Pod snapshot so a
	// Service cannot bypass tenant enforcement by omitting TargetRef.
	if pod, ok := pods.byIP[address]; ok {
		backend.ManagedPod = true
		backend.TenantID = pod.tenantID
	}

	return backend, true, nil
}

type podSnapshot struct {
	byName map[string]podRecord
	byIP   map[netip.Addr]podRecord
}

type podRecord struct {
	uid         types.UID
	tenantID    string
	hostNetwork bool
	ipv4        map[netip.Addr]struct{}
}

func buildPodSnapshot(pods []*corev1.Pod) (podSnapshot, error) {
	out := podSnapshot{
		byName: make(map[string]podRecord, len(pods)),
		byIP:   make(map[netip.Addr]podRecord, len(pods)),
	}

	for _, pod := range pods {
		if pod == nil {
			continue
		}

		record := podRecord{
			uid:         pod.UID,
			tenantID:    tenantmeta.ResolvePodTenant(pod.Namespace, pod.Labels),
			hostNetwork: pod.Spec.HostNetwork,
			ipv4:        podIPv4Set(pod),
		}

		out.byName[pod.Namespace+"/"+pod.Name] = record
		if record.hostNetwork {
			continue
		}

		for address := range record.ipv4 {
			if existing, exists := out.byIP[address]; exists && existing.uid != record.uid {
				return podSnapshot{}, fmt.Errorf(
					"Pod IPv4 address %s is owned by multiple Pods",
					address,
				)
			}
			out.byIP[address] = record
		}
	}

	return out, nil
}

func podIPv4Set(pod *corev1.Pod) map[netip.Addr]struct{} {
	out := make(map[netip.Addr]struct{})
	if pod == nil {
		return out
	}

	values := make([]string, 0, len(pod.Status.PodIPs)+1)
	if pod.Status.PodIP != "" {
		values = append(values, pod.Status.PodIP)
	}
	for _, podIP := range pod.Status.PodIPs {
		if podIP.IP != "" {
			values = append(values, podIP.IP)
		}
	}

	for _, value := range values {
		address, err := netip.ParseAddr(value)
		if err != nil {
			continue
		}
		address = address.Unmap()
		if address.Is4() && address.Zone() == "" {
			out[address] = struct{}{}
		}
	}

	return out
}

func endpointReady(
	service *corev1.Service,
	endpoint discoveryv1.Endpoint,
) bool {
	if service != nil && service.Spec.PublishNotReadyAddresses {
		return true
	}
	if endpoint.Conditions.Ready == nil {
		return true
	}
	return *endpoint.Conditions.Ready
}

func endpointPort(
	servicePort corev1.ServicePort,
	ports []discoveryv1.EndpointPort,
) (uint16, bool) {
	serviceProtocolValue := servicePort.Protocol
	if serviceProtocolValue == "" {
		serviceProtocolValue = corev1.ProtocolTCP
	}

	for _, port := range ports {
		if port.Port == nil || *port.Port <= 0 || *port.Port > 65535 {
			continue
		}

		name := ""
		if port.Name != nil {
			name = *port.Name
		}
		if name != servicePort.Name {
			continue
		}

		protocol := corev1.ProtocolTCP
		if port.Protocol != nil && *port.Protocol != "" {
			protocol = *port.Protocol
		}
		if protocol != serviceProtocolValue {
			continue
		}

		return uint16(*port.Port), true
	}

	return 0, false
}

func serviceProtocol(protocol corev1.Protocol) (uint8, bool) {
	if protocol == "" {
		protocol = corev1.ProtocolTCP
	}

	switch protocol {
	case corev1.ProtocolTCP:
		return seteraebpf.ServiceProtocolTCP, true
	case corev1.ProtocolUDP:
		return seteraebpf.ServiceProtocolUDP, true
	default:
		return 0, false
	}
}

func serviceIPv4ClusterIPs(
	service *corev1.Service,
) ([]netip.Addr, error) {
	if service == nil || service.Spec.Type == corev1.ServiceTypeExternalName {
		return nil, nil
	}

	values := append([]string(nil), service.Spec.ClusterIPs...)
	if len(values) == 0 && service.Spec.ClusterIP != "" {
		values = append(values, service.Spec.ClusterIP)
	}

	seen := make(map[netip.Addr]struct{})
	out := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		if value == "" || value == corev1.ClusterIPNone {
			continue
		}

		address, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf(
				"parse Service ClusterIP %q: %w",
				value,
				err,
			)
		}
		address = address.Unmap()
		if !address.Is4() || address.Zone() != "" {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, address)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Less(out[j])
	})
	return out, nil
}
