package trie

import (
	"fmt"
	"net"
)

type TrieNode struct {
	Prefix    *net.IPNet
	Parent    *TrieNode
	Children  [2]*TrieNode
	Allocated bool
	ID        string
}

func NewTrieNode(prefix *net.IPNet, parent *TrieNode) *TrieNode {

	return &TrieNode{
		Prefix:    prefix,
		Parent:    parent,
		Allocated: false,
		ID:        "",
	}
}

func (n *TrieNode) MaxExpandableDepth() int {
	depth := 0
	current := n
	for current.Parent != nil {
		sibling := getSibling(current)
		if sibling != nil && sibling.Allocated {
			break
		}
		if hasAllocatedDescendant(sibling) {
			break
		}
		depth++
		current = current.Parent
	}
	return depth
}

func (node *TrieNode) printTree(prefix string, isTail bool) {
	if node == nil {
		return
	}
	connector := "├── "
	if isTail {
		connector = "└── "
	}
	if node.ID == "" {
		fmt.Printf("%s%s%s %v \n", prefix, connector, node.Prefix.String(), node.Allocated)

	} else {
		fmt.Printf("%s%s%s %v %s\n", prefix, connector, node.Prefix.String(), node.Allocated, node.ID)
	}

	children := []*TrieNode{node.Children[0], node.Children[1]}
	numChildren := 0
	for _, child := range children {
		if child != nil {
			numChildren++
		}
	}

	for i, child := range children {
		if child == nil {
			continue
		}
		childIsTail := (i == numChildren-1)
		newPrefix := prefix
		if isTail {
			newPrefix += "    "
		} else {
			newPrefix += "│   "
		}
		child.printTree(newPrefix, childIsTail)
	}
}

func (n *TrieNode) hasAllocatedDescendants() bool {
	if n == nil {
		return false
	}
	if n.Allocated {
		return true
	}
	return n.Children[0].hasAllocatedDescendants() || n.Children[1].hasAllocatedDescendants()
}

func (node *TrieNode) Build(maxMaskSize int) {
	maskSize, _ := node.Prefix.Mask.Size()
	if maskSize >= maxMaskSize {
		return
	}

	subnets, err := SplitSubnet(node.Prefix, maxMaskSize)
	if err != nil {
		panic(err)
	}

	for i, subnet := range subnets {
		child := &TrieNode{
			Prefix:    subnet,
			Parent:    node,
			Allocated: false,
			ID:        "",
		}
		node.Children[i] = child
		child.Build(maxMaskSize)
	}
}
