package loader

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

const (
	servicePinRoot             = "/sys/fs/bpf/setera/service"
	serviceFrontendMapPath     = servicePinRoot + "/frontend"
	serviceBackendMapPath      = servicePinRoot + "/backend"
	serviceSocketRevNatMapPath = servicePinRoot + "/socket_revnat"
	serviceSocketStatsMapPath  = servicePinRoot + "/socket_stats"
	servicePacketFlowMapPath   = servicePinRoot + "/packet_flow"
	servicePacketRevNatMapPath = servicePinRoot + "/packet_revnat"
	servicePacketStatsMapPath  = servicePinRoot + "/packet_stats"

	serviceFrontendMaxEntries     uint32 = 16384
	serviceBackendMaxEntries      uint32 = 131072
	serviceSocketRevNatMaxEntries uint32 = 262144
	serviceSocketStatsMaxEntries  uint32 = 6
	servicePacketFlowMaxEntries   uint32 = 262144
	servicePacketRevNatMaxEntries uint32 = 262144
	servicePacketStatsMaxEntries  uint32 = 7

	serviceBackendFlagManagedPod uint8 = 1 << 0
	maxServiceTenantBytes              = 63
)

// ServiceKey identifies one IPv4 Service frontend.
type ServiceKey struct {
	IP       net.IP
	Port     uint16
	Protocol uint8
}

// ServiceBackend is one ready backend for a Service frontend.
type ServiceBackend struct {
	IP         net.IP
	Port       uint16
	Tenant     string
	ManagedPod bool
}

// Service is one Service frontend and its ready backends.
type Service struct {
	Key      ServiceKey
	Backends []ServiceBackend
}

// ServiceMap owns the pinned Service frontend and backend maps.
type ServiceMap struct {
	frontend *ebpf.Map
	backend  *ebpf.Map
	mu       sync.Mutex
}

// serviceFrontendKey is the BPF ABI for a Service frontend.
// Address and Port are stored in network byte order.
type serviceFrontendKey struct {
	Address  [4]byte
	Port     [2]byte
	Protocol uint8
	Pad      uint8
}

// Flags is reserved for future Service datapath features.
type serviceFrontendValue struct {
	BackendCount uint32
	Flags        uint32
}

type serviceBackendKey struct {
	Frontend serviceFrontendKey
	Slot     uint32
}

// Tenant is empty when the backend is not a Setera-managed Pod.
type serviceBackendValue struct {
	Address [4]byte
	Port    [2]byte
	Flags   uint8
	Pad     uint8
	Tenant  [64]byte
}

// serviceSocketRevNatKey identifies one socket translation. The backend
// address and port allow one UDP socket to use more than one Service.
type serviceSocketRevNatKey struct {
	SocketCookie   uint64
	BackendAddress [4]byte
	BackendPort    [2]byte
	Pad            [2]byte
}

// serviceSocketRevNatValue restores the Service address exposed to userspace.
type serviceSocketRevNatValue struct {
	FrontendAddress [4]byte
	FrontendPort    [2]byte
	Protocol        uint8
	Pad             uint8
}

type servicePacketFlowKey struct {
	ClientAddress   [4]byte
	FrontendAddress [4]byte
	ClientPort      [2]byte
	FrontendPort    [2]byte
	Protocol        uint8
	Pad             [3]byte
}

type servicePacketFlowValue struct {
	BackendAddress [4]byte
	BackendPort    [2]byte
	BackendFlags   uint8
	Pad            uint8
	LastSeenNS     uint64
}

type servicePacketRevNatKey struct {
	BackendAddress [4]byte
	ClientAddress  [4]byte
	BackendPort    [2]byte
	ClientPort     [2]byte
	Protocol       uint8
	Pad            [3]byte
}

type servicePacketRevNatValue struct {
	FrontendAddress [4]byte
	FrontendPort    [2]byte
	Pad             [2]byte
	LastSeenNS      uint64
}

type packetServiceMaps struct {
	frontend *ebpf.Map
	backend  *ebpf.Map
	flow     *ebpf.Map
	revNAT   *ebpf.Map
	stats    *ebpf.Map
}

func (m *packetServiceMaps) Close() error {
	if m == nil {
		return nil
	}

	return errors.Join(
		closeMap(m.frontend),
		closeMap(m.backend),
		closeMap(m.flow),
		closeMap(m.revNAT),
		closeMap(m.stats),
	)
}

func (m *packetServiceMaps) replacements() map[string]*ebpf.Map {
	return map[string]*ebpf.Map{
		"svc_frontend":   m.frontend,
		"svc_backend":    m.backend,
		"svc_pkt_flow":   m.flow,
		"svc_pkt_revnat": m.revNAT,
		"svc_pkt_stats":  m.stats,
	}
}

func serviceFrontendMapSpec() *ebpf.MapSpec {
	return &ebpf.MapSpec{
		Name:       "svc_frontend",
		Type:       ebpf.Hash,
		KeySize:    uint32(binary.Size(serviceFrontendKey{})),
		ValueSize:  uint32(binary.Size(serviceFrontendValue{})),
		MaxEntries: serviceFrontendMaxEntries,
	}
}

func serviceBackendMapSpec() *ebpf.MapSpec {
	return &ebpf.MapSpec{
		Name:       "svc_backend",
		Type:       ebpf.Hash,
		KeySize:    uint32(binary.Size(serviceBackendKey{})),
		ValueSize:  uint32(binary.Size(serviceBackendValue{})),
		MaxEntries: serviceBackendMaxEntries,
	}
}

func serviceSocketRevNatMapSpec() *ebpf.MapSpec {
	return &ebpf.MapSpec{
		Name:       "svc_sock_revnat",
		Type:       ebpf.LRUHash,
		KeySize:    uint32(binary.Size(serviceSocketRevNatKey{})),
		ValueSize:  uint32(binary.Size(serviceSocketRevNatValue{})),
		MaxEntries: serviceSocketRevNatMaxEntries,
	}
}

func serviceSocketStatsMapSpec() *ebpf.MapSpec {
	return &ebpf.MapSpec{
		Name:       "svc_sock_stats",
		Type:       ebpf.PerCPUArray,
		KeySize:    uint32(binary.Size(uint32(0))),
		ValueSize:  uint32(binary.Size(uint64(0))),
		MaxEntries: serviceSocketStatsMaxEntries,
	}
}

func servicePacketFlowMapSpec() *ebpf.MapSpec {
	return &ebpf.MapSpec{
		Name:       "svc_pkt_flow",
		Type:       ebpf.LRUHash,
		KeySize:    uint32(binary.Size(servicePacketFlowKey{})),
		ValueSize:  uint32(binary.Size(servicePacketFlowValue{})),
		MaxEntries: servicePacketFlowMaxEntries,
	}
}

func servicePacketRevNatMapSpec() *ebpf.MapSpec {
	return &ebpf.MapSpec{
		Name:       "svc_pkt_revnat",
		Type:       ebpf.LRUHash,
		KeySize:    uint32(binary.Size(servicePacketRevNatKey{})),
		ValueSize:  uint32(binary.Size(servicePacketRevNatValue{})),
		MaxEntries: servicePacketRevNatMaxEntries,
	}
}

func servicePacketStatsMapSpec() *ebpf.MapSpec {
	return &ebpf.MapSpec{
		Name:       "svc_pkt_stats",
		Type:       ebpf.PerCPUArray,
		KeySize:    uint32(binary.Size(uint32(0))),
		ValueSize:  uint32(binary.Size(uint64(0))),
		MaxEntries: servicePacketStatsMaxEntries,
	}
}

func openPacketServiceMaps() (*packetServiceMaps, error) {
	frontend, err := openOrCreatePinnedServiceMap(
		serviceFrontendMapPath,
		serviceFrontendMapSpec(),
	)
	if err != nil {
		return nil, err
	}

	backend, err := openOrCreatePinnedServiceMap(
		serviceBackendMapPath,
		serviceBackendMapSpec(),
	)
	if err != nil {
		frontend.Close()
		return nil, err
	}

	flow, err := openOrCreatePinnedServiceMap(
		servicePacketFlowMapPath,
		servicePacketFlowMapSpec(),
	)
	if err != nil {
		frontend.Close()
		backend.Close()
		return nil, err
	}

	revNAT, err := openOrCreatePinnedServiceMap(
		servicePacketRevNatMapPath,
		servicePacketRevNatMapSpec(),
	)
	if err != nil {
		frontend.Close()
		backend.Close()
		flow.Close()
		return nil, err
	}

	stats, err := openOrCreatePinnedServiceMap(
		servicePacketStatsMapPath,
		servicePacketStatsMapSpec(),
	)
	if err != nil {
		frontend.Close()
		backend.Close()
		flow.Close()
		revNAT.Close()
		return nil, err
	}

	return &packetServiceMaps{
		frontend: frontend,
		backend:  backend,
		flow:     flow,
		revNAT:   revNAT,
		stats:    stats,
	}, nil
}

// OpenServiceMap opens or creates the node-local pinned Service maps.
func OpenServiceMap() (*ServiceMap, error) {
	frontend, err := openOrCreatePinnedServiceMap(
		serviceFrontendMapPath,
		serviceFrontendMapSpec(),
	)
	if err != nil {
		return nil, err
	}

	backend, err := openOrCreatePinnedServiceMap(
		serviceBackendMapPath,
		serviceBackendMapSpec(),
	)
	if err != nil {
		frontend.Close()
		return nil, err
	}

	return &ServiceMap{
		frontend: frontend,
		backend:  backend,
	}, nil
}

// Close releases the userspace handles. The pinned maps remain.
func (m *ServiceMap) Close() error {
	if m == nil {
		return nil
	}

	return errors.Join(
		closeMap(m.frontend),
		closeMap(m.backend),
	)
}

// ReconcileServices makes both maps match an authoritative Service snapshot.
// Backends are written before the frontend publishes their count.
func (m *ServiceMap) ReconcileServices(services []Service) error {
	if m == nil || m.frontend == nil || m.backend == nil {
		return fmt.Errorf("service map is not open")
	}

	encoded, desired, err := encodeServices(services)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, service := range encoded {
		if err := m.upsertServiceLocked(service); err != nil {
			return err
		}
	}

	if err := m.deleteStaleFrontendsLocked(desired); err != nil {
		return err
	}

	if err := m.deleteOrphanBackendsLocked(); err != nil {
		return err
	}

	return nil
}

type encodedService struct {
	key      serviceFrontendKey
	value    serviceFrontendValue
	backends []serviceBackendValue
}

func encodeServices(
	services []Service,
) ([]encodedService, map[serviceFrontendKey]struct{}, error) {
	out := make([]encodedService, 0, len(services))
	desired := make(
		map[serviceFrontendKey]struct{},
		len(services),
	)

	for _, service := range services {
		encoded, err := encodeService(service)
		if err != nil {
			return nil, nil, err
		}

		if _, exists := desired[encoded.key]; exists {
			return nil, nil, fmt.Errorf(
				"duplicate Service frontend %v",
				service.Key,
			)
		}

		desired[encoded.key] = struct{}{}
		out = append(out, encoded)
	}

	return out, desired, nil
}

func encodeService(service Service) (encodedService, error) {
	key, err := encodeServiceKey(service.Key)
	if err != nil {
		return encodedService{}, err
	}

	if len(service.Backends) > int(serviceBackendMaxEntries) {
		return encodedService{}, fmt.Errorf(
			"too many Service backends",
		)
	}

	backends := make(
		[]serviceBackendValue,
		0,
		len(service.Backends),
	)

	for _, backend := range service.Backends {
		value, err := encodeServiceBackend(backend)
		if err != nil {
			return encodedService{}, err
		}

		backends = append(backends, value)
	}

	return encodedService{
		key: key,
		value: serviceFrontendValue{
			BackendCount: uint32(len(backends)),
		},
		backends: backends,
	}, nil
}

func encodeServiceKey(
	key ServiceKey,
) (serviceFrontendKey, error) {
	ip := key.IP.To4()
	if ip == nil {
		return serviceFrontendKey{}, fmt.Errorf(
			"invalid Service IPv4 address %v",
			key.IP,
		)
	}

	if key.Port == 0 {
		return serviceFrontendKey{}, fmt.Errorf(
			"Service port is zero",
		)
	}

	if key.Protocol == 0 {
		return serviceFrontendKey{}, fmt.Errorf(
			"Service protocol is zero",
		)
	}

	var out serviceFrontendKey

	copy(out.Address[:], ip)
	binary.BigEndian.PutUint16(
		out.Port[:],
		key.Port,
	)
	out.Protocol = key.Protocol

	return out, nil
}

func encodeServiceBackend(
	backend ServiceBackend,
) (serviceBackendValue, error) {
	ip := backend.IP.To4()
	if ip == nil {
		return serviceBackendValue{}, fmt.Errorf(
			"invalid Service backend IPv4 address %v",
			backend.IP,
		)
	}

	if backend.Port == 0 {
		return serviceBackendValue{}, fmt.Errorf(
			"Service backend port is zero",
		)
	}

	if backend.ManagedPod {
		if backend.Tenant == "" {
			return serviceBackendValue{}, fmt.Errorf(
				"managed Service backend tenant is empty",
			)
		}

		if len(backend.Tenant) > maxServiceTenantBytes {
			return serviceBackendValue{}, fmt.Errorf(
				"Service backend tenant %q is too long",
				backend.Tenant,
			)
		}
	} else if backend.Tenant != "" {
		return serviceBackendValue{}, fmt.Errorf(
			"external Service backend tenant must be empty",
		)
	}

	var out serviceBackendValue

	copy(out.Address[:], ip)
	binary.BigEndian.PutUint16(
		out.Port[:],
		backend.Port,
	)

	if backend.ManagedPod {
		out.Flags |= serviceBackendFlagManagedPod
		copy(out.Tenant[:], backend.Tenant)
	}

	return out, nil
}

func (m *ServiceMap) upsertServiceLocked(
	service encodedService,
) error {
	oldCount, err :=
		m.frontendBackendCountLocked(service.key)
	if err != nil {
		return err
	}

	for slot, backend := range service.backends {
		key := serviceBackendKey{
			Frontend: service.key,
			Slot:     uint32(slot),
		}

		if err := m.backend.Update(
			key,
			backend,
			ebpf.UpdateAny,
		); err != nil {
			return fmt.Errorf(
				"write Service backend slot %d: %w",
				slot,
				err,
			)
		}
	}

	if err := m.frontend.Update(
		service.key,
		service.value,
		ebpf.UpdateAny,
	); err != nil {
		return fmt.Errorf(
			"write Service frontend: %w",
			err,
		)
	}

	for slot := service.value.BackendCount; slot < oldCount; slot++ {
		key := serviceBackendKey{
			Frontend: service.key,
			Slot:     slot,
		}

		if err := deleteMapKey(
			m.backend,
			key,
		); err != nil {
			return fmt.Errorf(
				"delete stale Service backend slot %d: %w",
				slot,
				err,
			)
		}
	}

	return nil
}

func (m *ServiceMap) deleteStaleFrontendsLocked(
	desired map[serviceFrontendKey]struct{},
) error {
	var stale []serviceFrontendKey

	it := m.frontend.Iterate()

	var key serviceFrontendKey
	var value serviceFrontendValue

	for it.Next(&key, &value) {
		if _, keep := desired[key]; !keep {
			stale = append(stale, key)
		}
	}

	if err := it.Err(); err != nil {
		return fmt.Errorf(
			"iterate Service frontends: %w",
			err,
		)
	}

	for _, key := range stale {
		if err := deleteMapKey(
			m.frontend,
			key,
		); err != nil {
			return fmt.Errorf(
				"delete stale Service frontend: %w",
				err,
			)
		}
	}

	return nil
}

func (m *ServiceMap) deleteOrphanBackendsLocked() error {
	frontendCounts :=
		make(map[serviceFrontendKey]uint32)

	frontendIt := m.frontend.Iterate()

	var frontendKey serviceFrontendKey
	var frontendValue serviceFrontendValue

	for frontendIt.Next(
		&frontendKey,
		&frontendValue,
	) {
		frontendCounts[frontendKey] =
			frontendValue.BackendCount
	}

	if err := frontendIt.Err(); err != nil {
		return fmt.Errorf(
			"iterate Service frontends for backend cleanup: %w",
			err,
		)
	}

	var orphaned []serviceBackendKey

	backendIt := m.backend.Iterate()

	var backendKey serviceBackendKey
	var backendValue serviceBackendValue

	for backendIt.Next(
		&backendKey,
		&backendValue,
	) {
		count := frontendCounts[backendKey.Frontend]

		if backendKey.Slot >= count {
			orphaned = append(
				orphaned,
				backendKey,
			)
		}
	}

	if err := backendIt.Err(); err != nil {
		return fmt.Errorf(
			"iterate Service backends: %w",
			err,
		)
	}

	for _, key := range orphaned {
		if err := deleteMapKey(
			m.backend,
			key,
		); err != nil {
			return fmt.Errorf(
				"delete orphan Service backend: %w",
				err,
			)
		}
	}

	return nil
}

func (m *ServiceMap) frontendBackendCountLocked(
	key serviceFrontendKey,
) (uint32, error) {
	var value serviceFrontendValue

	if err := m.frontend.Lookup(
		key,
		&value,
	); err != nil {
		if errors.Is(err, ebpf.ErrKeyNotExist) {
			return 0, nil
		}

		return 0, fmt.Errorf(
			"lookup Service frontend: %w",
			err,
		)
	}

	return value.BackendCount, nil
}

func deleteMapKey(
	m *ebpf.Map,
	key any,
) error {
	if err := m.Delete(key); err != nil &&
		!errors.Is(err, ebpf.ErrKeyNotExist) {
		return err
	}

	return nil
}

func closeMap(m *ebpf.Map) error {
	if m == nil {
		return nil
	}

	return m.Close()
}

func openOrCreatePinnedServiceMap(
	path string,
	spec *ebpf.MapSpec,
) (*ebpf.Map, error) {
	if err := ensureBPFFSMounted(
		"/sys/fs/bpf",
	); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(
		servicePinRoot,
		0755,
	); err != nil {
		return nil, fmt.Errorf(
			"create Service pin directory %s: %w",
			servicePinRoot,
			err,
		)
	}

	m, err := ebpf.LoadPinnedMap(
		path,
		nil,
	)

	if err == nil {
		if err := spec.Compatible(m); err != nil {
			m.Close()

			return nil, fmt.Errorf(
				"pinned Service map %s is incompatible: %w",
				path,
				err,
			)
		}

		return m, nil
	}

	if !errors.Is(err, os.ErrNotExist) &&
		!errors.Is(err, unix.ENOENT) {
		return nil, fmt.Errorf(
			"open pinned Service map %s: %w",
			path,
			err,
		)
	}

	m, err = ebpf.NewMap(spec)
	if err != nil {
		return nil, fmt.Errorf(
			"create Service map %s: %w",
			path,
			err,
		)
	}

	if err := m.Pin(path); err == nil {
		return m, nil
	} else if !errors.Is(err, os.ErrExist) &&
		!errors.Is(err, unix.EEXIST) {
		m.Close()

		return nil, fmt.Errorf(
			"pin Service map %s: %w",
			path,
			err,
		)
	}

	m.Close()

	m, err = ebpf.LoadPinnedMap(
		path,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"reopen Service map %s: %w",
			path,
			err,
		)
	}

	if err := spec.Compatible(m); err != nil {
		m.Close()

		return nil, fmt.Errorf(
			"reopened Service map %s is incompatible: %w",
			path,
			err,
		)
	}

	return m, nil
}
