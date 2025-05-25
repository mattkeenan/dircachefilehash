package dircachefilehash

import (
	"sync"
)

// BPlusTreeNode represents a node in the B+ tree
type BPlusTreeNode struct {
	isLeaf   bool
	keys     []string         // filenames for sorting
	entries  []FileEntry      // actual file entries (leaf nodes only)
	children []*BPlusTreeNode // child nodes (internal nodes only)
	next     *BPlusTreeNode   // next leaf node (for range queries)
}

// BPlusTree represents a B+ tree indexed by filename
type BPlusTree struct {
	root  *BPlusTreeNode
	order int // maximum number of keys per node
	mutex sync.RWMutex
}

// NewBPlusTree creates a new B+ tree with the specified order
func NewBPlusTree(order int) *BPlusTree {
	if order < 3 {
		order = 4 // minimum practical order
	}
	return &BPlusTree{
		root:  &BPlusTreeNode{isLeaf: true},
		order: order,
	}
}

// Insert adds a file entry to the B+ tree
func (tree *BPlusTree) Insert(entry FileEntry) {
	tree.mutex.Lock()
	defer tree.mutex.Unlock()

	if tree.root == nil {
		tree.root = &BPlusTreeNode{isLeaf: true}
	}

	tree.insertEntry(tree.root, entry)
}

// insertEntry recursively inserts an entry into the tree
func (tree *BPlusTree) insertEntry(node *BPlusTreeNode, entry FileEntry) *BPlusTreeNode {
	if node.isLeaf {
		// Insert into leaf node
		pos := tree.findInsertPosition(node.keys, entry.RelativePath)

		// Insert key and entry at position
		node.keys = append(node.keys, "")
		node.entries = append(node.entries, FileEntry{})

		// Shift elements to make room
		copy(node.keys[pos+1:], node.keys[pos:])
		copy(node.entries[pos+1:], node.entries[pos:])

		node.keys[pos] = entry.RelativePath
		node.entries[pos] = entry

		// Check if node needs to be split
		if len(node.keys) >= tree.order {
			return tree.splitLeafNode(node)
		}
		return nil
	} else {
		// Find child to insert into
		childIndex := tree.findChildIndex(node.keys, entry.RelativePath)
		newChild := tree.insertEntry(node.children[childIndex], entry)

		if newChild != nil {
			// Child was split, need to add separator to this node
			separator := newChild.keys[0]
			return tree.insertInternalNode(node, separator, newChild)
		}
		return nil
	}
}

// findInsertPosition finds the position to insert a key using binary search
func (tree *BPlusTree) findInsertPosition(keys []string, key string) int {
	left, right := 0, len(keys)
	for left < right {
		mid := (left + right) / 2
		if keys[mid] < key {
			left = mid + 1
		} else {
			right = mid
		}
	}
	return left
}

// findChildIndex finds the child index for a key
func (tree *BPlusTree) findChildIndex(keys []string, key string) int {
	for i, k := range keys {
		if key < k {
			return i
		}
	}
	return len(keys)
}

// splitLeafNode splits a full leaf node
func (tree *BPlusTree) splitLeafNode(node *BPlusTreeNode) *BPlusTreeNode {
	mid := len(node.keys) / 2

	newNode := &BPlusTreeNode{
		isLeaf:  true,
		keys:    make([]string, len(node.keys)-mid),
		entries: make([]FileEntry, len(node.entries)-mid),
		next:    node.next,
	}

	copy(newNode.keys, node.keys[mid:])
	copy(newNode.entries, node.entries[mid:])

	node.keys = node.keys[:mid]
	node.entries = node.entries[:mid]
	node.next = newNode

	return newNode
}

// insertInternalNode inserts a key and child into an internal node
func (tree *BPlusTree) insertInternalNode(node *BPlusTreeNode, key string, child *BPlusTreeNode) *BPlusTreeNode {
	pos := tree.findInsertPosition(node.keys, key)

	// Insert key
	node.keys = append(node.keys, "")
	copy(node.keys[pos+1:], node.keys[pos:])
	node.keys[pos] = key

	// Insert child
	node.children = append(node.children, nil)
	copy(node.children[pos+2:], node.children[pos+1:])
	node.children[pos+1] = child

	// Check if node needs to be split
	if len(node.keys) >= tree.order {
		return tree.splitInternalNode(node)
	}
	return nil
}

// splitInternalNode splits a full internal node
func (tree *BPlusTree) splitInternalNode(node *BPlusTreeNode) *BPlusTreeNode {
	mid := len(node.keys) / 2

	newNode := &BPlusTreeNode{
		isLeaf:   false,
		keys:     make([]string, len(node.keys)-mid-1),
		children: make([]*BPlusTreeNode, len(node.children)-mid-1),
	}

	copy(newNode.keys, node.keys[mid+1:])
	copy(newNode.children, node.children[mid+1:])

	promotedKey := node.keys[mid]
	node.keys = node.keys[:mid]
	node.children = node.children[:mid+1]

	// Create new root if necessary
	if tree.root == node {
		newRoot := &BPlusTreeNode{
			isLeaf:   false,
			keys:     []string{promotedKey},
			children: []*BPlusTreeNode{node, newNode},
		}
		tree.root = newRoot
		return nil
	}

	return newNode
}

// GetSortedEntries returns all entries in sorted order by filename
func (tree *BPlusTree) GetSortedEntries() []FileEntry {
	tree.mutex.RLock()
	defer tree.mutex.RUnlock()

	var entries []FileEntry
	tree.collectLeafEntries(tree.root, &entries)
	return entries
}

// collectLeafEntries recursively collects all entries from leaf nodes
func (tree *BPlusTree) collectLeafEntries(node *BPlusTreeNode, entries *[]FileEntry) {
	if node == nil {
		return
	}

	if node.isLeaf {
		*entries = append(*entries, node.entries...)
	} else {
		for _, child := range node.children {
			tree.collectLeafEntries(child, entries)
		}
	}
}
