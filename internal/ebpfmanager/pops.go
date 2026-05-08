package ebpfmanager

import (
	"context"
	"fmt"
	"log"
	"net"

	fw "github/setera/pkg/network/policy/loader"
)

var _ PodOps = (*EbpfManagerImpl)(nil)

func (em *EbpfManagerImpl) EnsurePodEndpoint(ctx context.Context, tenantID string, podName string, ifName string) error {
	if tenantID == "" || podName == "" {
		return fmt.Errorf("ensure pod endpoint: tenantID and podName are required")
	}
	if err := em.EnsureTenantMaps(ctx, tenantID); err != nil {
		return fmt.Errorf("ensure pod endpoint tenant maps: %w", err)
	}

	em.mu.RLock()
	rec, ok := em.TenantRecords[tenantID]
	em.mu.RUnlock()
	if !ok || rec == nil {
		return ErrTenantNotFound
	}
	if rec.State == TenantStateClosing {
		return ErrTenantClosing
	}

	// If caller provided an explicit interface name (host veth), prefer it; otherwise fall back to default derived name.
	if ifName == "" {
		ifName = em.defaultPodIfName(tenantID, podName)
	}

	// Skip eBPF TC program attachment if the interface name doesn't exist in host
	// (likely a remote pod or stale entry). This prevents attaching to invalid names.
	if _, err := net.InterfaceByName(ifName); err != nil {
		log.Printf("WARN: EnsurePodEndpoint skipped for %s/%s: interface %s not found (likely remote pod) - err: %v", tenantID, podName, ifName, err)
		return nil
	}

	if err := em.PP.EnsurePodProgram(ctx, tenantID, podName, ifName); err != nil {
		return fmt.Errorf("ensure pod program: %w", err)
	}

	em.mu.Lock()
	if rec.Pods == nil {
		rec.Pods = make(map[string]*PodRecord)
	}
	rec.Pods[podName] = &PodRecord{PodName: podName, IfName: ifName}
	em.mu.Unlock()

	return nil
}

func (em *EbpfManagerImpl) UpdatePodEndpoint(ctx context.Context, tenantID string, podName string) error {
	if tenantID == "" || podName == "" {
		return fmt.Errorf("update pod endpoint: tenantID and podName are required")
	}
	if err := em.EnsureTenantMaps(ctx, tenantID); err != nil {
		return fmt.Errorf("update pod endpoint tenant maps: %w", err)
	}

	em.mu.RLock()
	rec, ok := em.TenantRecords[tenantID]
	em.mu.RUnlock()
	if !ok || rec == nil {
		return ErrTenantNotFound
	}
	if rec.State == TenantStateClosing {
		return ErrTenantClosing
	}

	ifName := em.defaultPodIfName(tenantID, podName)
	if err := em.PP.UpdatePodProgram(ctx, tenantID, podName, ifName); err != nil {
		return fmt.Errorf("update pod program: %w", err)
	}

	em.mu.Lock()
	if rec.Pods == nil {
		rec.Pods = make(map[string]*PodRecord)
	}
	rec.Pods[podName] = &PodRecord{PodName: podName, IfName: ifName}
	em.mu.Unlock()

	return nil
}

func (em *EbpfManagerImpl) RemovePodEndpoint(ctx context.Context, tenantID string, podName string) error {
	if tenantID == "" || podName == "" {
		return fmt.Errorf("remove pod endpoint: tenantID and podName are required")
	}

	if err := em.PP.RemovePodProgram(ctx, tenantID, podName); err != nil {
		return fmt.Errorf("remove pod program: %w", err)
	}

	em.mu.Lock()
	if rec, ok := em.TenantRecords[tenantID]; ok && rec != nil && rec.Pods != nil {
		delete(rec.Pods, podName)
	}
	em.mu.Unlock()

	return nil
}

func (em *EbpfManagerImpl) UpsertPodMapEntry(ctx context.Context, tenantID string, podName string, podIP string, ifindex int) error {
	_ = ctx
	if tenantID == "" {
		return fmt.Errorf("upsert pod map entry: tenantID is required")
	}
	if podIP == "" {
		return fmt.Errorf("upsert pod map entry: podIP is required")
	}
	ip := net.ParseIP(podIP).To4()
	if ip == nil {
		return fmt.Errorf("upsert pod map entry: invalid IPv4 %q", podIP)
	}
	if err := fw.WritePodTenantVeth(ip, tenantID, ifindex); err != nil {
		return fmt.Errorf("upsert pod map entry tenant=%s pod=%s ip=%s ifindex=%d: %w", tenantID, podName, podIP, ifindex, err)
	}
	return nil
}

func (em *EbpfManagerImpl) DeletePodMapEntry(ctx context.Context, podIP string) error {
	_ = ctx
	if podIP == "" {
		return fmt.Errorf("delete pod map entry: podIP is required")
	}
	ip := net.ParseIP(podIP).To4()
	if ip == nil {
		return fmt.Errorf("delete pod map entry: invalid IPv4 %q", podIP)
	}
	if err := fw.DeletePodTenantVeth(ip); err != nil {
		return fmt.Errorf("delete pod map entry ip=%s: %w", podIP, err)
	}
	return nil
}
