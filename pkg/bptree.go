// insertEntryUnsafe inserts an entry without locking (internal use during rebuild)package dircachefilehash

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

// Merge merges another B+ tree into this tree using the Hwang-Lin algorithm
// This is optimized for merging two sorted datasets efficiently
func (tree *BPlusTree) Merge(other *BPlusTree) {
	if other == nil || other.root == nil {
		return
	}

	// Lock both trees during merge operation
	tree.mutex.Lock()
	defer tree.mutex.Unlock()
	other.mutex.RLock()
	defer other.mutex.RUnlock()

	// Get sorted entries from both trees
	thisEntries := tree.getSortedEntriesUnsafe()
	otherEntries := other.getSortedEntriesUnsafe()

	// Use Hwang-Lin merge algorithm to combine the sorted sequences
	mergedEntries := tree.hwangLinMerge(thisEntries, otherEntries)

	// Rebuild tree with merged entries
	tree.rebuildFromSortedEntries(mergedEntries)
}

// getSortedEntriesUnsafe returns sorted entries without locking (internal use)
func (tree *BPlusTree) getSortedEntriesUnsafe() []FileEntry {
	var entries []FileEntry
	tree.collectLeafEntries(tree.root, &entries)
	return entries
}

// hwangLinMerge implements the Hwang-Lin merge algorithm for two sorted sequences
// This algorithm is optimal for merging sorted sequences of different sizes
func (tree *BPlusTree) hwangLinMerge(a, b []FileEntry) []FileEntry {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}

	// Ensure 'a' is the larger sequence for optimal performance
	if len(a) < len(b) {
		a, b = b, a
	}

	m, n := len(a), len(b)
	result := make([]FileEntry, 0, m+n)

	// Hwang-Lin algorithm: use binary search to find merge points
	i, j := 0, 0

	for i < m && j < n {
		// For small remaining sequences, use traditional merge
		if n-j <= 16 { // Threshold for switching to traditional merge
			result = append(result, tree.traditionalMerge(a[i:], b[j:])...)
			break
		}

		// Find the position in 'a' where b[j] should be inserted
		insertPos := tree.binarySearchInsertPosition(a[i:], b[j].RelativePath)
		actualPos := i + insertPos

		// Add elements from 'a' that come before b[j]
		result = append(result, a[i:actualPos]...)
		i = actualPos

		// Add b[j]
		if i < m && a[i].RelativePath == b[j].RelativePath {
			// Same file - use the newer entry (from 'b' which represents updates)
			result = append(result, b[j])
			i++ // Skip the old entry in 'a'
		} else {
			result = append(result, b[j])
		}
		j++

		// If the remaining part of 'b' is small, find a block in 'a' to process
		if n-j > 1 && m-i > 16 {
			// Find how many consecutive elements from 'b' can be inserted
			blockEnd := j
			for blockEnd < n && blockEnd-j < 8 { // Process blocks of up to 8 elements
				nextInsertPos := tree.binarySearchInsertPosition(a[i:], b[blockEnd].RelativePath)
				if nextInsertPos > insertPos+32 { // If too far apart, process individually
					break
				}
				blockEnd++
			}

			// Process the block if it's beneficial
			if blockEnd > j+1 {
				blockResult := tree.mergeBlock(a[i:], b[j:blockEnd])
				result = append(result, blockResult...)

				// Update positions based on what was consumed
				consumed := tree.countConsumedFromA(a[i:], b[j:blockEnd])
				i += consumed
				j = blockEnd
			}
		}
	}

	// Add remaining elements
	if i < m {
		result = append(result, a[i:]...)
	}
	if j < n {
		result = append(result, b[j:]...)
	}

	return result
}

// binarySearchInsertPosition finds the position where key should be inserted in sorted slice
func (tree *BPlusTree) binarySearchInsertPosition(entries []FileEntry, key string) int {
	left, right := 0, len(entries)
	for left < right {
		mid := (left + right) / 2
		if entries[mid].RelativePath < key {
			left = mid + 1
		} else {
			right = mid
		}
	}
	return left
}

// traditionalMerge performs traditional merge for small sequences
func (tree *BPlusTree) traditionalMerge(a, b []FileEntry) []FileEntry {
	result := make([]FileEntry, 0, len(a)+len(b))
	i, j := 0, 0

	for i < len(a) && j < len(b) {
		if a[i].RelativePath < b[j].RelativePath {
			result = append(result, a[i])
			i++
		} else if a[i].RelativePath > b[j].RelativePath {
			result = append(result, b[j])
			j++
		} else {
			// Same file - use the newer entry (from 'b')
			result = append(result, b[j])
			i++
			j++
		}
	}

	// Add remaining elements
	result = append(result, a[i:]...)
	result = append(result, b[j:]...)

	return result
}

// mergeBlock merges a small block of entries from b with corresponding section of a
func (tree *BPlusTree) mergeBlock(a, bBlock []FileEntry) []FileEntry {
	if len(bBlock) == 0 {
		return a
	}
	if len(a) == 0 {
		return bBlock
	}

	// Find the range in 'a' that overlaps with bBlock
	startKey := bBlock[0].RelativePath
	endKey := bBlock[len(bBlock)-1].RelativePath

	startPos := tree.binarySearchInsertPosition(a, startKey)
	endPos := tree.binarySearchInsertPosition(a, endKey)
	if endPos < len(a) && a[endPos].RelativePath == endKey {
		endPos++ // Include the matching element
	}

	// Merge the relevant section
	relevantA := a[startPos:endPos]
	merged := tree.traditionalMerge(relevantA, bBlock)

	// Combine with the parts of 'a' that weren't involved in the merge
	result := make([]FileEntry, 0, len(a)+len(bBlock))
	result = append(result, a[:startPos]...)
	result = append(result, merged...)
	result = append(result, a[endPos:]...)

	return result
}

// countConsumedFromA counts how many elements from 'a' were consumed during block merge
func (tree *BPlusTree) countConsumedFromA(a, bBlock []FileEntry) int {
	if len(bBlock) == 0 {
		return 0
	}

	startKey := bBlock[0].RelativePath
	endKey := bBlock[len(bBlock)-1].RelativePath

	startPos := tree.binarySearchInsertPosition(a, startKey)
	endPos := tree.binarySearchInsertPosition(a, endKey)
	if endPos < len(a) && a[endPos].RelativePath == endKey {
		endPos++
	}

	return endPos - startPos
}

// rebuildFromSortedEntries rebuilds the B+ tree from a sorted list of entries
func (tree *BPlusTree) rebuildFromSortedEntries(entries []FileEntry) {
	// Clear the current tree
	tree.root = &BPlusTreeNode{isLeaf: true}

	// Insert entries in order (they're already sorted)
	for _, entry := range entries {
		tree.insertEntryUnsafe(tree.root, entry)
	}
}

// Delete removes entries from this tree that exist in the other tree using Hwang-Lin algorithm
// This is optimized for bulk deletion operations
func (tree *BPlusTree) Delete(other *BPlusTree) {
	if other == nil || other.root == nil {
		return
	}

	// Lock both trees during delete operation
	tree.mutex.Lock()
	defer tree.mutex.Unlock()
	other.mutex.RLock()
	defer other.mutex.RUnlock()

	// Get sorted entries from both trees
	thisEntries := tree.getSortedEntriesUnsafe()
	deleteEntries := other.getSortedEntriesUnsafe()

	// Use Hwang-Lin algorithm to efficiently find and remove entries
	remainingEntries := tree.hwangLinDelete(thisEntries, deleteEntries)

	// Rebuild tree with remaining entries
	tree.rebuildFromSortedEntries(remainingEntries)
}

// hwangLinDelete implements Hwang-Lin algorithm for efficient bulk deletion
// Returns entries from 'this' that are NOT in 'toDelete'
func (tree *BPlusTree) hwangLinDelete(thisEntries, toDelete []FileEntry) []FileEntry {
	if len(toDelete) == 0 {
		return thisEntries
	}
	if len(thisEntries) == 0 {
		return []FileEntry{}
	}

	result := make([]FileEntry, 0, len(thisEntries))
	i, j := 0, 0 // i for thisEntries, j for toDelete

	for i < len(thisEntries) && j < len(toDelete) {
		// For small remaining sequences, use traditional approach
		if len(toDelete)-j <= 16 {
			result = append(result, tree.traditionalDelete(thisEntries[i:], toDelete[j:])...)
			break
		}

		thisKey := thisEntries[i].RelativePath
		deleteKey := toDelete[j].RelativePath

		if thisKey < deleteKey {
			// Current entry in 'this' is not in delete list

			// Use binary search to find how many consecutive entries we can keep
			nextDeletePos := tree.binarySearchInsertPosition(toDelete[j:], thisKey)
			actualNextDeletePos := j + nextDeletePos

			// Find the range of entries we can safely keep
			keepUntil := i + 1
			if actualNextDeletePos < len(toDelete) {
				nextDeleteKey := toDelete[actualNextDeletePos].RelativePath
				// Find position where next delete key would be inserted
				keepUntilPos := tree.binarySearchInsertPositionInEntries(thisEntries[i:], nextDeleteKey)
				keepUntil = i + keepUntilPos
			} else {
				// No more delete keys, keep all remaining
				keepUntil = len(thisEntries)
			}

			// Add the range of entries that should be kept
			result = append(result, thisEntries[i:keepUntil]...)
			i = keepUntil

		} else if thisKey > deleteKey {
			// Advance delete pointer
			j++
		} else {
			// thisKey == deleteKey: skip this entry (delete it)
			i++
			j++
		}
	}

	// Add any remaining entries from 'this' (they're not in delete list)
	if i < len(thisEntries) {
		result = append(result, thisEntries[i:]...)
	}

	return result
}

// binarySearchInsertPositionInEntries finds insert position in FileEntry slice by RelativePath
func (tree *BPlusTree) binarySearchInsertPositionInEntries(entries []FileEntry, key string) int {
	left, right := 0, len(entries)
	for left < right {
		mid := (left + right) / 2
		if entries[mid].RelativePath < key {
			left = mid + 1
		} else {
			right = mid
		}
	}
	return left
}

// traditionalDelete performs traditional deletion for small sequences
func (tree *BPlusTree) traditionalDelete(thisEntries, toDelete []FileEntry) []FileEntry {
	result := make([]FileEntry, 0, len(thisEntries))
	i, j := 0, 0

	for i < len(thisEntries) && j < len(toDelete) {
		thisKey := thisEntries[i].RelativePath
		deleteKey := toDelete[j].RelativePath

		if thisKey < deleteKey {
			// Keep this entry
			result = append(result, thisEntries[i])
			i++
		} else if thisKey > deleteKey {
			// Advance delete pointer
			j++
		} else {
			// thisKey == deleteKey: skip (delete) this entry
			i++
			j++
		}
	}

	// Add remaining entries from 'this'
	result = append(result, thisEntries[i:]...)
	return result
}
func (tree *BPlusTree) insertEntryUnsafe(node *BPlusTreeNode, entry FileEntry) *BPlusTreeNode {
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
		newChild := tree.insertEntryUnsafe(node.children[childIndex], entry)

		if newChild != nil {
			// Child was split, need to add separator to this node
			separator := newChild.keys[0]
			return tree.insertInternalNode(node, separator, newChild)
		}
		return nil
	}
}
