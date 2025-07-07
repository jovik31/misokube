package trie

import (
	"fmt"
	"net"
)

// non-public func to recursively build the IPTrie recursively
func buildRecursive(node *TrieNode, maxMaskSize int) {

	maskSize, _ := node.Prefix.Mask.Size()
	if maskSize >= maxMaskSize {
		return
	}

	//split current node subnet into two
	subnets, err := SplitSubnet(node.Prefix, maxMaskSize)
	if err != nil {
		panic(err)
	}

	for i, subnet := range subnets {

		child := &TrieNode{
			Prefix:    subnet,
			Parent:    node,
			Allocated: false,
			TenantID:  "",
		}
		node.Children[i] = child
		buildRecursive(child, maxMaskSize)
	}

}

func findFreeLeavesRecursive(node *TrieNode, result *[]*TrieNode, hasAllocatedAncestor bool) {
	if node == nil {
		return
	}

	if hasAllocatedAncestor || node.Allocated {
		// If this node or any ancestor is allocated, skip its entire subtree
		hasAllocatedAncestor = true
	}

	// If it's a leaf and no ancestor is allocated, it's a valid free leaf
	if node.Children[0] == nil && node.Children[1] == nil {
		if !hasAllocatedAncestor {
			*result = append(*result, node)
		}
		return
	}

	findFreeLeavesRecursive(node.Children[0], result, hasAllocatedAncestor)
	findFreeLeavesRecursive(node.Children[1], result, hasAllocatedAncestor)
}

// public func to divide a network into two subnetworks with half the IP capacity
func SplitSubnet(network *net.IPNet, maskMaxSize int) ([]*net.IPNet, error) {

	// MaxTheoreticalMaskSize
	var MaxTheoreticalMaskSize = 32

	// get the network mask size and the number of bits
	maskSize, bits := network.Mask.Size()
	if maskSize >= maskMaxSize || maskSize >= MaxTheoreticalMaskSize {
		return nil, fmt.Errorf("already at max subnet size %d", maskMaxSize)
	}
	// Create new mask for the subnets of the network
	newMask := net.CIDRMask(maskSize+1, bits)

	firstIP := network.IP.Mask(network.Mask)
	secondIP := make(net.IP, len(firstIP))
	copy(secondIP, firstIP)

	// Set the next bit to 1 for second subnet
	byteIndex := maskSize / 8
	bitOffset := 7 - (maskSize % 8)
	secondIP[byteIndex] |= 1 << bitOffset

	return []*net.IPNet{
		{IP: firstIP, Mask: newMask},
		{IP: secondIP, Mask: newMask},
	}, nil
}

func getSibling(n *TrieNode) *TrieNode {
	if n == nil || n.Parent == nil {
		return nil
	}
	if n.Parent.Children[0] == n {
		return n.Parent.Children[1]
	}
	return n.Parent.Children[0]
}

func hasAllocatedDescendant(n *TrieNode) bool {
	if n == nil {
		return false
	}
	if n.Allocated {
		return true
	}
	return hasAllocatedDescendant(n.Children[0]) || hasAllocatedDescendant(n.Children[1])
}
