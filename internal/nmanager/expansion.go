package nmanager

import (
	"context"
	"log"
	"time"
)

// requestTenantExpansion ensures only one expansion runs per tenant.
// If an expansion is already in-flight, callers wait for it to finish.
func (nm *NetworkManagerImpl) requestTenantExpansion(ctx context.Context, tenantID string) error {
	var waitCh chan struct{}

	nm.mu.Lock()
	rec := nm.TenantRecords[tenantID]
	if rec == nil {
		nm.mu.Unlock()
		return ErrTenantActorNotFound
	}
	if rec.State == TenantStateClosing {
		nm.mu.Unlock()
		return ErrTenantClosing
	}
	if rec.expandCh != nil {
		waitCh = rec.expandCh
		nm.mu.Unlock()
		return nm.waitForTenantExpansion(ctx, tenantID, waitCh)
	}

	waitCh = make(chan struct{})
	rec.expandCh = waitCh
	nm.mu.Unlock()

	start := time.Now()
	log.Printf("tenant=%s expansion requested", tenantID)
	err := nm.performTenantExpansion(ctx, tenantID)

	nm.mu.Lock()
	rec = nm.TenantRecords[tenantID]
	if rec != nil && rec.expandCh == waitCh {
		rec.lastExpandErr = err
		rec.lastExpandAt = time.Now()
		rec.expandCh = nil
	}
	nm.mu.Unlock()

	close(waitCh)
	if err != nil {
		log.Printf("tenant=%s expansion failed: %v", tenantID, err)
	} else {
		log.Printf("tenant=%s expansion succeeded in %s", tenantID, time.Since(start))
	}
	return err
}

func (nm *NetworkManagerImpl) waitForTenantExpansion(ctx context.Context, tenantID string, waitCh chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-waitCh:
		nm.mu.RLock()
		rec := nm.TenantRecords[tenantID]
		var err error = ErrTenantActorNotFound
		if rec != nil {
			err = rec.lastExpandErr
		}
		nm.mu.RUnlock()
		return err
	}
}

func (nm *NetworkManagerImpl) performTenantExpansion(ctx context.Context, tenantID string) error {
	_, err := nm.ExpandTenant(ctx, tenantID)
	return err
}
