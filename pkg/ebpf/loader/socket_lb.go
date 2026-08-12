package loader

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

// SocketLB owns the IPv4 cgroup socket load-balancer programs.
//
// Service frontend and backend maps are shared with the Service watcher. The
// reverse-NAT and statistics maps are pinned so translations and observability
// state survive daemon restart.
type SocketLB struct {
	programs socketLBPrograms
	links    []link.Link

	closeOnce sync.Once
	closeErr  error
}

type socketLBPrograms struct {
	Connect4     *ebpf.Program `ebpf:"setera_service_connect4"`
	Sendmsg4     *ebpf.Program `ebpf:"setera_service_sendmsg4"`
	Recvmsg4     *ebpf.Program `ebpf:"setera_service_recvmsg4"`
	Getpeername4 *ebpf.Program `ebpf:"setera_service_getpeername4"`
}

func (p *socketLBPrograms) Close() error {
	if p == nil {
		return nil
	}

	return errors.Join(
		closeProgram(p.Connect4),
		closeProgram(p.Sendmsg4),
		closeProgram(p.Recvmsg4),
		closeProgram(p.Getpeername4),
	)
}

// NewSocketLB loads the Setera IPv4 socket load balancer and attaches it to a
// cgroup v2 root. The programs apply to processes in that cgroup and its
// descendants.
func NewSocketLB(cgroupRoot string) (*SocketLB, error) {
	cgroupRoot = strings.TrimSpace(cgroupRoot)
	if cgroupRoot == "" {
		return nil, fmt.Errorf("Service socket LB cgroup root is empty")
	}
	if err := requireCgroupV2(cgroupRoot); err != nil {
		return nil, err
	}

	frontend, err := openOrCreatePinnedServiceMap(
		serviceFrontendMapPath,
		serviceFrontendMapSpec(),
	)
	if err != nil {
		return nil, err
	}
	defer frontend.Close()

	backend, err := openOrCreatePinnedServiceMap(
		serviceBackendMapPath,
		serviceBackendMapSpec(),
	)
	if err != nil {
		return nil, err
	}
	defer backend.Close()

	reverseNAT, err := openOrCreatePinnedServiceMap(
		serviceSocketRevNatMapPath,
		serviceSocketRevNatMapSpec(),
	)
	if err != nil {
		return nil, err
	}
	defer reverseNAT.Close()

	stats, err := openOrCreatePinnedServiceMap(
		serviceSocketStatsMapPath,
		serviceSocketStatsMapSpec(),
	)
	if err != nil {
		return nil, err
	}
	defer stats.Close()

	spec, err := loadServiceLB()
	if err != nil {
		return nil, fmt.Errorf("load Service socket LB collection spec: %w", err)
	}
	if err := prepareSocketLBSpec(spec); err != nil {
		return nil, err
	}

	var programs socketLBPrograms
	if err := spec.LoadAndAssign(
		&programs,
		&ebpf.CollectionOptions{
			MapReplacements: map[string]*ebpf.Map{
				"svc_frontend":    frontend,
				"svc_backend":     backend,
				"svc_sock_revnat": reverseNAT,
				"svc_sock_stats":  stats,
			},
		},
	); err != nil {
		return nil, fmt.Errorf("load Service socket LB programs: %w", err)
	}

	lb := &SocketLB{programs: programs}
	if err := lb.attach(cgroupRoot); err != nil {
		programs.Close()
		return nil, err
	}

	return lb, nil
}

func prepareSocketLBSpec(spec *ebpf.CollectionSpec) error {
	if spec == nil {
		return fmt.Errorf("Service socket LB collection spec is nil")
	}

	want := map[string]*ebpf.MapSpec{
		"svc_frontend":    serviceFrontendMapSpec(),
		"svc_backend":     serviceBackendMapSpec(),
		"svc_sock_revnat": serviceSocketRevNatMapSpec(),
		"svc_sock_stats":  serviceSocketStatsMapSpec(),
	}

	for name, expected := range want {
		actual := spec.Maps[name]
		if actual == nil {
			return fmt.Errorf(
				"Service socket LB map %s is missing from collection spec",
				name,
			)
		}
		if actual.Type != expected.Type ||
			actual.KeySize != expected.KeySize ||
			actual.ValueSize != expected.ValueSize ||
			actual.MaxEntries != expected.MaxEntries ||
			actual.Flags != expected.Flags {
			return fmt.Errorf(
				"Service socket LB map %s is incompatible: got %s, want %s",
				name,
				actual,
				expected,
			)
		}

		actual.Pinning = ebpf.PinNone
	}

	return nil
}

func (lb *SocketLB) attach(cgroupRoot string) error {
	attachments := []struct {
		name   string
		attach ebpf.AttachType
		prog   *ebpf.Program
	}{
		{
			name:   "connect4",
			attach: ebpf.AttachCGroupInet4Connect,
			prog:   lb.programs.Connect4,
		},
		{
			name:   "sendmsg4",
			attach: ebpf.AttachCGroupUDP4Sendmsg,
			prog:   lb.programs.Sendmsg4,
		},
		{
			name:   "recvmsg4",
			attach: ebpf.AttachCGroupUDP4Recvmsg,
			prog:   lb.programs.Recvmsg4,
		},
		{
			name:   "getpeername4",
			attach: ebpf.AttachCgroupInet4GetPeername,
			prog:   lb.programs.Getpeername4,
		},
	}

	for _, item := range attachments {
		if item.prog == nil {
			lb.closeLinks()
			return fmt.Errorf(
				"Service socket LB program %s is nil",
				item.name,
			)
		}

		attached, err := link.AttachCgroup(link.CgroupOptions{
			Path:    cgroupRoot,
			Attach:  item.attach,
			Program: item.prog,
		})
		if err != nil {
			lb.closeLinks()
			return fmt.Errorf(
				"attach Service socket LB %s to %s: %w",
				item.name,
				cgroupRoot,
				err,
			)
		}

		lb.links = append(lb.links, attached)
	}

	return nil
}

// Close detaches the cgroup programs and releases the program handles. The
// pinned Service, reverse-NAT, and statistics maps remain.
func (lb *SocketLB) Close() error {
	if lb == nil {
		return nil
	}

	lb.closeOnce.Do(func() {
		lb.closeErr = errors.Join(
			lb.closeLinks(),
			lb.programs.Close(),
		)
	})

	return lb.closeErr
}

func (lb *SocketLB) closeLinks() error {
	if lb == nil {
		return nil
	}

	var errs []error
	for i := len(lb.links) - 1; i >= 0; i-- {
		if lb.links[i] == nil {
			continue
		}
		if err := lb.links[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	lb.links = nil
	return errors.Join(errs...)
}

func requireCgroupV2(path string) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return fmt.Errorf("stat Service socket LB cgroup root %s: %w", path, err)
	}
	if uint64(stat.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) {
		return fmt.Errorf(
			"Service socket LB cgroup root %s is not cgroup v2",
			path,
		)
	}
	return nil
}

func closeProgram(program *ebpf.Program) error {
	if program == nil {
		return nil
	}
	return program.Close()
}
