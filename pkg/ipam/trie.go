package ipam

import (
	"net"
)

type TrieNode struct {
	Prefix    *net.IPNet
	Children  [2]*TrieNode
	Allocated bool
}

func NewTrie(root *net.IPNet) *TrieNode {
	return &TrieNode{
		Prefix: root,
	}
}
