package trie

import (
	"fmt"
	"net"
)

type IPTrie struct {
	Root     *TrieNode            // Stores the Root/Head node
	RootCIDR *net.IPNet           // The node CIDR that is subneted across tenants
	AllocMap map[string]*TrieNode // TenantID <--> TrieNode
}

// Creates a trie by initializing its root
func NewTrie(rootNet *net.IPNet) *IPTrie {

	root := NewTrieNode(rootNet, nil)
	return &IPTrie{
		Root:     root,
		RootCIDR: rootNet,
		AllocMap: make(map[string]*TrieNode),
	}
}

// Recursively build trie until it reaches maxMaskSize
func (trie *IPTrie) Build(maxMaskSize int) {

	if trie.Root == nil {
		return
	}
	buildRecursive(trie.Root, maxMaskSize)
}

// Deallocate a tenant from a trie node
func (trie *IPTrie) DeallocateTenant(tenantID string) error {
	node := trie.GetNodeByTenantID(tenantID)
	if node == nil {
		return fmt.Errorf("tenant %q not found", tenantID)
	}

	// Prevent root node deallocation
	if node.Parent == nil {
		return fmt.Errorf("cannot deallocate root node directly")
	}

	// Clear allocation info
	node.Allocated = false
	node.TenantID = ""
	delete(trie.AllocMap, tenantID)

	// Rebuild children if this is not a /30 leaf
	maskSize, _ := node.Prefix.Mask.Size()
	if maskSize < 30 {
		node.Children = [2]*TrieNode{nil, nil} // Clear old children if any
		node.Build(30)                         // Rebuild down to /30
	}

	return nil
}

// Allocate a tenant to a trie node, returns false and with error if allocation fails
func (trie *IPTrie) AllocateTenantSubnet(tenantID string) (*TrieNode, error) {

	// check if the root exists
	if trie.Root == nil {
		return nil, fmt.Errorf("trie root is nil")
	}

	// check if the tenantID already exists and has an allocated subnet
	if _, exists := trie.AllocMap[tenantID]; exists {
		return nil, fmt.Errorf("tenant %q, aleardy exists and has an allocated subnet", tenantID)
	}

	freeSubnets := trie.FindAllFreeSubnets()

	// no more space for tenants
	if len(freeSubnets) == 0 {

		return nil, fmt.Errorf("no free /30 subnet available")

	}

	var bestNode *TrieNode // bestAllocatable leaf node
	maxExpansion := -1     // initial max expansion is -1, since the minimum possible is 0

	for _, leaf := range freeSubnets {

		expansion := leaf.MaxExpandableDepth()
		if expansion > maxExpansion {

			bestNode = leaf
			maxExpansion = expansion

		}
	}

	if bestNode == nil {
		return nil, fmt.Errorf("no suitable subnet found")
	}

	bestNode.Allocated = true
	bestNode.TenantID = tenantID
	trie.AllocMap[tenantID] = bestNode

	return bestNode, nil
}

func (trie *IPTrie) FindAllFreeSubnets() []*TrieNode {

	var freeSubnets []*TrieNode

	findFreeLeavesRecursive(trie.Root, &freeSubnets, false) // false because root node does not have ancestors

	return freeSubnets
}

func (trie *IPTrie) MergeSubnet(tenantID string) error {

	node := trie.GetNodeByTenantID(tenantID)

	// Node is empty, as its parent
	if node == nil || node.Parent == nil {

		return fmt.Errorf("tenant id returns empty node")
	}

	parent := node.Parent

	var sibling *TrieNode

	if parent.Children[0] == node {
		sibling = parent.Children[1]
	} else {
		sibling = parent.Children[0]
	}

	if sibling == nil {
		return fmt.Errorf("error of nil sibling should not appear")
	}

	if sibling.Allocated || sibling.hasAllocatedDescendants() {

		return fmt.Errorf("cannot merge tenant %s with subnet %v: tenant %s has subnet %v allocated", node.TenantID, node.Prefix, sibling.TenantID, sibling.Prefix)
	}

	//should never reach this case because allocation a leaf requires its parent to be empty
	if parent.Allocated {

		return fmt.Errorf("PANIC - UNSTABLE STATE")
	}

	//Merge the subnetworks
	parent.Allocated = true
	parent.TenantID = tenantID
	parent.Children = [2]*TrieNode{nil, nil}

	//Clear child state
	node.Allocated = false
	node.TenantID = ""
	sibling.Allocated = false
	sibling.TenantID = ""

	//Update the alloc map
	trie.AllocMap[tenantID] = parent

	return nil

}

func (trie *IPTrie) PrintTree() {
	if trie.Root != nil {
		trie.Root.printTree("", true)
	}
}

func (trie *IPTrie) GetNodeByTenantID(tenantID string) *TrieNode {
	var result *TrieNode

	var dfs func(node *TrieNode)
	dfs = func(node *TrieNode) {

		if node == nil || result != nil {

			return
		}
		if node.Allocated && node.TenantID == tenantID {
			result = node
			return
		}
		dfs(node.Children[0])
		dfs(node.Children[1])
	}

	dfs(trie.Root)

	return result
}
