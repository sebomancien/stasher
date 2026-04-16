package label

import (
	"fmt"
	"strings"
)

// Node is one node in the label tree.
// It can hold a direct value (leaf), named children (interior), or both.
type Node struct {
	Value    string
	Children map[string]*Node
}

// Converts a flat map of dot-separated label keys into a tree.
// For example, the key "a.b.c" with value "v" becomes root["a"]["b"]["c"].Value = &"v".
func ParseTree(labels map[string]string) *Node {
	root := &Node{
		Children: make(map[string]*Node),
	}
	for key, val := range labels {
		root.insert(val, strings.Split(key, ".")...)
	}
	return root
}

// Inserts a nodes along a path.
// All nodes in the path will be created.
func (n *Node) insert(value string, path ...string) {
	for _, p := range path {
		child, ok := n.Children[p]
		if !ok {
			child = &Node{
				Children: make(map[string]*Node),
			}
			n.Children[p] = child
		}
		n = child
	}
	n.Value = value
}

// At navigates to the node at the path and returns it.
// An empty path returns the receiver unchanged.
func (n *Node) At(path ...string) (*Node, error) {
	for _, p := range path {
		child, ok := n.Children[p]
		if !ok {
			return nil, fmt.Errorf("path does not exist")
		}
		n = child
	}
	return n, nil
}
