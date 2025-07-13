package nodescore

import (
	"github/setera/pkg/cache"
	"sync"
)

type NodeScoreCache struct {
	scoreCache *cache.Cache[string, int]
	mu         sync.RWMutex
}

func NewNodeScoreCache() *NodeScoreCache {

	return &NodeScoreCache{
		// Create a cache for nodeID --> score
		scoreCache: cache.New[string, int](func(a, b int) bool { return a > b }),
	}
}

func (n *NodeScoreCache) Update(nodeID string, score int) {

	n.mu.Lock()
	defer n.mu.Unlock()

	n.scoreCache.Update(nodeID, score)

}

func (n *NodeScoreCache) Get(nodeID string) (int, bool) {

	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.scoreCache.Get(nodeID)
}

func (n *NodeScoreCache) GetWithCriteria(interval int) []struct {
	Key   string
	Value int
} {

	n.mu.RLock()
	defer n.mu.Unlock()

	return n.scoreCache.GetWithCriteria(interval)
}
