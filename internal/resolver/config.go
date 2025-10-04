package resolver

import (
	"log/slog"
	"time"
)

type Config struct {
	TenantLabelKey     string
	NodeName           string // index only pods scheduled on this node
	Logger             *slog.Logger
	InitialSyncTimeout time.Duration // timeout for the initial sync
}

func (c *Config) Validate() error {
	if c.TenantLabelKey == "" {
		return ErrEmptyTenantLabelKey
	}
	if c.NodeName == "" {
		return ErrEmptyNodeName
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return nil
}

func (c *Config) SetDefaults() {
	if c.TenantLabelKey == "" {
		c.TenantLabelKey = "setera.com/tenant"
	}
}
