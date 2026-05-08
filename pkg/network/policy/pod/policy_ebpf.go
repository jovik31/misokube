package pod

import (
	"context"
	"fmt"
	"net"
	"sync"

	tp "github/setera/pkg/network/policy"
	ebpffw "github/setera/pkg/network/policy/loader"
)

var _ tp.PodPolicyManager = (*ebpfPodPolicyManager)(nil)

type ebpfPodPolicyManager struct {
	mu   sync.Mutex
	pods map[string]*podProgram
}

type podProgram struct {
	ifName  string
	ifIndex int
	fw      *ebpffw.TCFirewall
}

func NewEBPFPodPolicyManager() tp.PodPolicyManager {
	return &ebpfPodPolicyManager{
		pods: make(map[string]*podProgram),
	}
}

func init() {
	tp.RegisterPodPolicyManager(NewEBPFPodPolicyManager())
}

func (m *ebpfPodPolicyManager) EnsurePodProgram(ctx context.Context, tenantID string, podName string, ifName string) error {
	_ = ctx
	if tenantID == "" || podName == "" || ifName == "" {
		return fmt.Errorf("ensure pod program: tenantID, podName and ifName are required")
	}
	key := podKey(tenantID, podName)

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.pods[key]; ok && existing != nil && existing.ifName == ifName {
		return nil
	}

	prog, err := m.attachProgram(tenantID, ifName)
	if err != nil {
		return err
	}

	if old := m.pods[key]; old != nil {
		_ = old.fw.RemoveInterfaceTenantBinding(old.ifIndex)
		_ = old.fw.Close()
	}
	m.pods[key] = prog
	return nil
}

func (m *ebpfPodPolicyManager) UpdatePodProgram(ctx context.Context, tenantID string, podName string, ifName string) error {
	return m.EnsurePodProgram(ctx, tenantID, podName, ifName)
}

func (m *ebpfPodPolicyManager) RemovePodProgram(ctx context.Context, tenantID string, podName string) error {
	_ = ctx
	if tenantID == "" || podName == "" {
		return fmt.Errorf("remove pod program: tenantID and podName are required")
	}
	key := podKey(tenantID, podName)

	m.mu.Lock()
	defer m.mu.Unlock()

	prog := m.pods[key]
	if prog == nil {
		return nil
	}

	if err := prog.fw.RemoveInterfaceTenantBinding(prog.ifIndex); err != nil {
		return fmt.Errorf("remove interface binding %s: %w", prog.ifName, err)
	}
	if err := prog.fw.Close(); err != nil {
		return fmt.Errorf("close tc firewall %s: %w", prog.ifName, err)
	}
	delete(m.pods, key)
	return nil
}

func (m *ebpfPodPolicyManager) attachProgram(tenantID string, ifName string) (*podProgram, error) {
	fw, err := ebpffw.NewTCFirewall(ifName, tenantID)
	if err != nil {
		return nil, fmt.Errorf("attach tc firewall to %s: %w", ifName, err)
	}
	if err := fw.SetDefaultAction(ebpffw.ActionDrop, true); err != nil {
		_ = fw.Close()
		return nil, fmt.Errorf("set default action on %s: %w", ifName, err)
	}

	iface, err := net.InterfaceByName(ifName)
	if err != nil {
		_ = fw.Close()
		return nil, fmt.Errorf("lookup interface %s: %w", ifName, err)
	}
	if err := fw.AddInterfaceTenantBinding(iface.Index, tenantID); err != nil {
		_ = fw.Close()
		return nil, fmt.Errorf("set tenant binding on %s: %w", ifName, err)
	}

	return &podProgram{
		ifName:  ifName,
		ifIndex: iface.Index,
		fw:      fw,
	}, nil
}

func podKey(tenantID string, podName string) string {
	return tenantID + "/" + podName
}
