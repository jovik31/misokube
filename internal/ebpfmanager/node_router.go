package ebpfmanager

import (
	"fmt"

	fw "github/setera/pkg/network/policy/loader"
)

// EnsureNodeRouter attaches the node-level TC program to the given interface.
func (em *EbpfManagerImpl) EnsureNodeRouter(ifaceName string) error {
	if ifaceName == "" {
		return fmt.Errorf("ensure node router: ifaceName is required")
	}

	em.nodeRouterMu.Lock()
	defer em.nodeRouterMu.Unlock()

	if em.nodeRouters == nil {
		em.nodeRouters = make(map[string]interface{ Close() error })
	}
	if _, ok := em.nodeRouters[ifaceName]; ok {
		return nil
	}

	router, err := fw.NewNodeRouter(ifaceName, "default")
	if err != nil {
		return fmt.Errorf("ensure node router on %s: %w", ifaceName, err)
	}

	em.nodeRouters[ifaceName] = router
	return nil
}
