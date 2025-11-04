package nmanager

import (
	"context"
	"net"
)

var _ PodOps = (*NetworkManagerImpl)(nil)

// Exposed contracts for pod lifecycle management. -> Called by the CNI plugin
func (nm *NetworkManagerImpl) AllocateNet(ctx context.Context, tenantID, epKey string) error {
	return nil
}

func (nm *NetworkManagerImpl) RemoveNet(ctx context.Context, tenantID, epKey string) error {
	return nil
}

func (nm *NetworkManagerImpl) AllocateIP(ctx context.Context, tenantID, epKey string) (net.IP, error) {
	return nil, nil
}

func (nm *NetworkManagerImpl) ReleaseIP(ctx context.Context, tenantID, epKey string) error {
	return nil
}
func (nm *NetworkManagerImpl) AttachEndpoint(ctx context.Context, tenantID, epKey string) error {
	return nil
}

func (nm *NetworkManagerImpl) DetachEndpoint(ctx context.Context, tenantID, epKey string) error {
	return nil
}
