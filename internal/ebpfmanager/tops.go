package ebpfmanager

import (
	"context"
	"fmt"
)

var _ TenantOps = (*EbpfManagerImpl)(nil)

func (em *EbpfManagerImpl) EnsureTenantMaps(ctx context.Context, tenantID string) error {
	_ = ctx
	if tenantID == "" {
		return fmt.Errorf("ensure tenant maps: empty tenantID")
	}

	em.mu.RLock()
	_, exists := em.TenantRecords[tenantID]
	em.mu.RUnlock()
	if exists {
		return nil
	}

	if em.TP != nil {
		if err := em.TP.EnsureTenantChains(tenantID); err != nil {
			return fmt.Errorf("ensure tenant chains: %w", err)
		}
	}

	em.mu.Lock()
	defer em.mu.Unlock()
	if _, exists := em.TenantRecords[tenantID]; exists {
		return nil
	}
	em.TenantRecords[tenantID] = &TenantRecord{
		TenantID: tenantID,
		State:    TenantStateReady,
		Pods:     make(map[string]*PodRecord),
	}
	return nil
}

func (em *EbpfManagerImpl) RemoveTenantMaps(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		return fmt.Errorf("remove tenant maps: empty tenantID")
	}

	em.mu.Lock()
	rec, exists := em.TenantRecords[tenantID]
	if !exists || rec == nil {
		em.mu.Unlock()
		return nil
	}
	rec.State = TenantStateClosing
	pods := make([]string, 0, len(rec.Pods))
	for podName := range rec.Pods {
		pods = append(pods, podName)
	}
	em.mu.Unlock()

	var firstErr error
	for _, podName := range pods {
		if err := em.PP.RemovePodProgram(ctx, tenantID, podName); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("remove pod program %s/%s: %w", tenantID, podName, err)
		}
	}

	if em.TP != nil {
		if err := em.TP.DeleteTenantChains(tenantID, "", ""); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("delete tenant chains: %w", err)
		}
	}

	em.mu.Lock()
	delete(em.TenantRecords, tenantID)
	em.mu.Unlock()

	return firstErr
}
