package cache

import (
	"sort"
	"sync"
)

// comparator returns true if a is better than b
type Comparator[V any] func(a, b V) bool

type Cache[K comparable, V any] struct {
	mu         sync.RWMutex
	data       map[K]V
	comparator Comparator[V]
}

// create generic cache
func New[K comparable, V any](cmp Comparator[V]) *Cache[K, V] {

	return &Cache[K, V]{
		data:       make(map[K]V),
		comparator: cmp,
	}
}

// update elements on the cache
func (c *Cache[K, V]) Update(key K, value V) {

	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = value
}

// retrive element by k
func (c *Cache[K, V]) Get(key K) (V, bool) {

	c.mu.RLock()
	defer c.mu.RUnlock()

	v, ok := c.data[key]
	return v, ok
}

// Get n elements with implemented criteria: max, min, best, etc..
// if n > len(c.data) return all elements but sorted
// check heaps for larger datasets
func (c *Cache[K, V]) GetWithCriteria(n int) []struct {
	Key   K
	Value V
} {

	c.mu.RLock()
	defer c.mu.RUnlock()

	entries := make([]struct {
		Key   K
		Value V
	}, 0, len(c.data))

	for k, v := range c.data {

		entries = append(entries, struct {
			Key   K
			Value V
		}{k, v})
	}

	sort.Slice(entries, func(i, j int) bool { return c.comparator(entries[i].Value, entries[j].Value) })

	if n > len(entries) {
		n = len(entries)
	}

	return entries[:n]
}
