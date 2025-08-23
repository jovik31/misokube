package trie

import (
	"fmt"
	"github/setera/pkg/data/trie"
	"github/setera/pkg/network/subnet"
	"net"
	"sync"
)

var _ subnet.SubnetManager = (*TrieManager)(nil)
var Default TrieManager

type TrieManager struct {
	mu       sync.Mutex
	t        *trie.IPTrie
	leafSize int
}

func NewTrieManager(root *net.IPNet, leafMask int) *TrieManager {

	t := trie.NewTrie(root)
	t.Build(leafMask)
	return &TrieManager{
		t:        t,
		leafSize: leafMask,
	}

}

func (t *TrieManager) Allocate(id string) (*net.IPNet, error) {

	t.mu.Lock()
	defer t.mu.Unlock()

	if id == "" {
		return nil, fmt.Errorf("id is empty")
	}

	if existing := t.t.GetNodeByID(id); existing != nil {
		return existing.Prefix, nil
	}

	n, err := t.t.Allocate(id)
	if err != nil {
		return nil, err
	}
	return n.Prefix, nil
}

func (t *TrieManager) Deallocate(id string) error {

	t.mu.Lock()
	defer t.mu.Unlock()
	if id == "" {
		return fmt.Errorf("id is empty")
	}
	if t.t.GetNodeByID(id) == nil {

		//no-op
		return nil

	}
	return t.t.Deallocate(id)

}

func (t *TrieManager) Expand(id string) (*net.IPNet, error) {

	t.mu.Lock()
	defer t.mu.Unlock()

	if id == "" {
		return nil, fmt.Errorf("id is empty")
	}

	n := t.t.GetNodeByID(id)
	if n == nil {
		return nil, fmt.Errorf("tenant %q not found", id)
	}

	//if parent is nil, cannot expand
	if n.Parent == nil {
		return nil, fmt.Errorf("tenant %q not found", id)
	}

	rt := t.Root()
	// n is root
	if n.Prefix == rt {
		return nil, fmt.Errorf("tenant %q has the same prefix has root %v", id, rt)
	}

	if err := t.t.Merge(id); err != nil {
		return nil, err
	}

	nM := t.t.GetNodeByID(id)
	if nM == nil {
		return nil, fmt.Errorf("expand: internal error, missing node after merge. tenant %q", id)
	}

	return nM.Prefix, nil

}

func (t *TrieManager) Reduce(id string) (*net.IPNet, error) {

	t.mu.Lock()
	defer t.mu.Unlock()

	if id == "" {
		return nil, fmt.Errorf("id is empty")
	}

	n := t.t.GetNodeByID(id)
	if n == nil {
		return nil, fmt.Errorf("tenant %q not found", id)
	}

	maskSize, _ := n.Prefix.Mask.Size()
	if maskSize >= t.leafSize {
		return nil, fmt.Errorf("reduce: already at leaf /%d (%s)", t.leafSize, n.Prefix)
	}
	child, err := trie.SplitSubnet(n.Prefix, maskSize+1)
	if err != nil {
		return nil, fmt.Errorf("reduce: split %s: %w", n.Prefix, err)
	}
	// care magic number - binary trees always have 2 children
	if len(child) != 2 {
		return nil, fmt.Errorf("reduce: unexpected number of children %d", len(child))
	}

	//rebuild node
	left := trie.NewTrieNode(child[0], n)
	right := trie.NewTrieNode(child[1], n)

	n.Children = [2]*trie.TrieNode{left, right}

	// Demote current allocation to left child.
	oldID := n.ID
	n.Allocated, n.ID = false, ""
	left.Allocated, left.ID = true, oldID

	t.t.AllocMap[oldID] = left

	return left.Prefix, nil

}

func (t *TrieManager) Release(id string) error {

	t.mu.Lock()
	defer t.mu.Unlock()

	if id == "" {
		return fmt.Errorf("release: id is empty")
	}
	n := t.t.GetNodeByID(id)
	if n == nil {
		return nil
	}

	if err := t.t.Deallocate(id); err != nil {
		return err
	}
	return nil

}

func (t *TrieManager) Get(id string) (*net.IPNet, error) {

	t.mu.Lock()
	defer t.mu.Unlock()
	n := t.t.GetNodeByID(id)
	if n == nil {
		return nil, fmt.Errorf("tenant %q not found", id)
	}
	return n.Prefix, nil
}

func (t *TrieManager) List() map[string]*net.IPNet {

	t.mu.Lock()
	defer t.mu.Unlock()

	snap := make(map[string]*net.IPNet, len(t.t.AllocMap))
	for id, n := range t.t.AllocMap {
		if n != nil {
			snap[id] = n.Prefix
		}

	}
	return snap
}

func (t *TrieManager) Root() *net.IPNet {

	t.mu.Lock()
	defer t.mu.Unlock()
	return t.t.RootCIDR
}
