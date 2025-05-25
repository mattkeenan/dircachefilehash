package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
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

// Update scans the directory and updates the index file
// If paths are provided, only those files/directories are updated
// If no paths are provided, all files under rootDir are updated
func (dc *DirectoryCache) Update(paths ...string) error {
	// If no paths provided, update the entire root directory
	if len(paths) == 0 {
		paths = []string{dc.RootDir}
	}

	// Always use UpdatePaths to handle both cases
	if err := dc.UpdatePaths(paths); err != nil {
		return fmt.Errorf("failed to update paths: %w", err)
	}

	if err := dc.WriteIndex(); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	return nil
}

// UpdatePaths updates only the specified paths in the index
func (dc *DirectoryCache) UpdatePaths(paths []string) error {
	// Load existing index to preserve other entries
	existingEntries := make(map[string]FileEntry)
	if err := dc.LoadIndex(); err == nil {
		for _, entry := range dc.entries {
			existingEntries[entry.RelativePath] = entry
		}
	}

	// Collect files to process from specified paths
	var fileJobs []fileJob
	jobIndex := 0
	pathsToRemove := make(map[string]bool)

	for _, inputPath := range paths {
		// Convert to absolute path
		absPath := inputPath
		if !filepath.IsAbs(inputPath) {
			absPath = filepath.Join(dc.RootDir, inputPath)
		}

		// Clean the path
		absPath = filepath.Clean(absPath)

		// Check if path exists
		info, err := os.Lstat(absPath)
		if err != nil {
			// Path doesn't exist - mark for removal from index
			relPath, relErr := filepath.Rel(dc.RootDir, absPath)
			if relErr == nil {
				pathsToRemove[relPath] = true
				// Also mark any files under this path for removal
				for existingPath := range existingEntries {
					if strings.HasPrefix(existingPath, relPath+"/") || existingPath == relPath {
						pathsToRemove[existingPath] = true
					}
				}
			}
			continue
		}

		// Get relative path from root directory
		relPath, err := filepath.Rel(dc.RootDir, absPath)
		if err != nil {
			return fmt.Errorf("path %s is not under root directory %s", absPath, dc.RootDir)
		}

		// If it's a directory, scan it recursively
		if info.IsDir() {
			if err := dc.scanPathRecursively(absPath, &fileJobs, &jobIndex, pathsToRemove); err != nil {
				return fmt.Errorf("failed to scan directory %s: %w", absPath, err)
			}
		} else if info.Mode().IsRegular() {
			// Skip the index file itself
			if absPath == dc.IndexFile {
				continue
			}

			// Add single file
			fileJobs = append(fileJobs, fileJob{
				path:    absPath,
				info:    info,
				relPath: relPath,
				index:   jobIndex,
			})
			jobIndex++

			// Mark this path as being updated (remove from pathsToRemove if it was there)
			delete(pathsToRemove, relPath)
		}
	}

	// Process files in parallel
	var newEntries []FileEntry
	if len(fileJobs) > 0 {
		results, err := dc.processFilesParallel(fileJobs)
		if err != nil {
			return err
		}

		// Sort results by original index to maintain discovery order
		sort.Slice(results, func(i, j int) bool {
			return results[i].index < results[j].index
		})

		// Extract entries from results
		for _, result := range results {
			if result.err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to process file: %v\n", result.err)
				continue
			}
			newEntries = append(newEntries, *result.entry)
		}
	}

	// Merge with existing entries
	updatedPaths := make(map[string]bool)
	for _, entry := range newEntries {
		updatedPaths[entry.RelativePath] = true
	}

	// Start with new entries
	dc.entries = make([]FileEntry, 0, len(newEntries)+len(existingEntries))
	dc.entries = append(dc.entries, newEntries...)

	// Add existing entries that weren't updated or removed
	for path, entry := range existingEntries {
		if !updatedPaths[path] && !pathsToRemove[path] {
			dc.entries = append(dc.entries, entry)
		}
	}

	// Sort entries by filename for byte comparison order
	sort.Slice(dc.entries, func(i, j int) bool {
		return dc.entries[i].RelativePath < dc.entries[j].RelativePath
	})

	return nil
}

// scanPathRecursively scans a directory path recursively and adds files to the job list
func (dc *DirectoryCache) scanPathRecursively(rootPath string, fileJobs *[]fileJob, jobIndex *int, pathsToRemove map[string]bool) error {
	// FIFO slice for directory traversal
	pathQueue := []string{rootPath}

	for len(pathQueue) > 0 {
		// Pop the first entry from the FIFO slice
		currentPath := pathQueue[0]
		pathQueue = pathQueue[1:]

		// Get file info
		info, err := os.Lstat(currentPath)
		if err != nil {
			continue // Skip files we can't access
		}

		// Get relative path
		relPath, err := filepath.Rel(dc.RootDir, currentPath)
		if err != nil {
			continue
		}

		if info.IsDir() {
			// Skip the index directory
			indexDir := filepath.Dir(dc.IndexFile)
			if currentPath == indexDir {
				continue
			}

			// Remove this path from removal list since it exists
			delete(pathsToRemove, relPath)

			entries, err := os.ReadDir(currentPath)
			if err != nil {
				continue // Skip directories we can't read
			}

			// Sort entries by name using bytewise comparison
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() < entries[j].Name()
			})

			// Add all entries to the FIFO queue
			for _, entry := range entries {
				fullPath := filepath.Join(currentPath, entry.Name())
				pathQueue = append(pathQueue, fullPath)
			}
		} else if info.Mode().IsRegular() {
			// Skip the index file itself
			if currentPath == dc.IndexFile {
				continue
			}

			// Add to jobs list
			*fileJobs = append(*fileJobs, fileJob{
				path:    currentPath,
				info:    info,
				relPath: relPath,
				index:   *jobIndex,
			})
			*jobIndex++

			// Remove this path from removal list since it exists
			delete(pathsToRemove, relPath)
		}
	}

	return nil
}

// ScanDirectory scans the directory and creates file entries with hashes using parallel processing
func (dc *DirectoryCache) ScanDirectory() error {
	dc.entries = make([]FileEntry, 0)

	// Collect all regular files first
	var fileJobs []fileJob
	jobIndex := 0

	// FIFO slice for file paths - push to end, pop from front
	pathQueue := []string{dc.RootDir}

	// Process paths until queue is empty
	for len(pathQueue) > 0 {
		// Pop the first entry from the FIFO slice
		currentPath := pathQueue[0]
		pathQueue = pathQueue[1:]

		// Get file info
		info, err := os.Lstat(currentPath) // Use Lstat to handle symlinks properly
		if err != nil {
			// Skip files we can't access
			continue
		}

		// If it's a directory, read its contents and add to queue
		if info.IsDir() {
			// Skip the index directory if it's inside the scan directory
			indexDir := filepath.Dir(dc.IndexFile)
			if currentPath == indexDir {
				continue
			}

			entries, err := os.ReadDir(currentPath)
			if err != nil {
				// Skip directories we can't read
				continue
			}

			// Sort entries by name using bytewise comparison
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() < entries[j].Name()
			})

			// Add all entries to the FIFO queue
			for _, entry := range entries {
				fullPath := filepath.Join(currentPath, entry.Name())
				pathQueue = append(pathQueue, fullPath)
			}
		} else if info.Mode().IsRegular() {
			// Skip the index file itself
			if currentPath == dc.IndexFile {
				continue
			}

			// Calculate relative path from root directory
			relPath, err := filepath.Rel(dc.RootDir, currentPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get relative path for %s: %v\n", currentPath, err)
				continue
			}

			// Add to jobs list
			fileJobs = append(fileJobs, fileJob{
				path:    currentPath,
				info:    info,
				relPath: relPath,
				index:   jobIndex,
			})
			jobIndex++
		}
		// For other file types (symlinks, device files, etc.), we just skip them
		// since we only want to index regular files with content hashes
	}

	// Process files in parallel and get B+ tree
	if len(fileJobs) > 0 {
		resultTree, err := dc.processFilesParallel(fileJobs)
		if err != nil {
			return err
		}

		// Get sorted entries from B+ tree
		dc.entries = resultTree.GetSortedEntries()
	}

	return nil
}

// processFilesParallel processes files using device-specific worker pools and returns a B+ tree
func (dc *DirectoryCache) processFilesParallel(jobs []fileJob) (*BPlusTree, error) {
	if len(jobs) == 0 {
		return NewBPlusTree(16), nil // Return empty B+ tree with order 16
	}

	// Group jobs by device
	deviceJobs := make(map[uint64][]fileJob)
	for _, job := range jobs {
		// Get device ID from file info
		stat := job.info.Sys().(*syscall.Stat_t)
		deviceID := stat.Dev
		deviceJobs[deviceID] = append(deviceJobs[deviceID], job)
	}

	// Create B+ tree for results (order 16 for good performance)
	resultTree := NewBPlusTree(16)

	// Create channels for collecting results from all devices
	resultChan := make(chan fileResult, len(jobs))
	var wg sync.WaitGroup

	// Start goroutine to collect results and insert into B+ tree
	var insertWg sync.WaitGroup
	insertWg.Add(1)
	go func() {
		defer insertWg.Done()
		for result := range resultChan {
			if result.err == nil && result.entry != nil {
				resultTree.Insert(*result.entry)
			}
		}
	}()

	// Process each device with its own worker pool
	for deviceID, jobs := range deviceJobs {
		wg.Add(1)
		go func(devID uint64, deviceJobs []fileJob) {
			defer wg.Done()
			dc.processDeviceJobs(devID, deviceJobs, resultChan)
		}(deviceID, jobs)
	}

	// Wait for all devices to complete
	wg.Wait()
	close(resultChan)

	// Wait for all insertions to complete
	insertWg.Wait()

	return resultTree, nil
}

// processDeviceJobs processes jobs for a specific device using 2 workers
func (dc *DirectoryCache) processDeviceJobs(deviceID uint64, jobs []fileJob, resultChan chan<- fileResult) {
	// Create 2 workers per device for optimal I/O performance
	numWorkers := 2
	if numWorkers > len(jobs) {
		numWorkers = len(jobs)
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	// Create device-specific channels
	jobChan := make(chan fileJob, len(jobs))

	// Start workers for this device
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go dc.deviceHashWorker(deviceID, jobChan, resultChan, &wg)
	}

	// Send jobs to device workers
	for _, job := range jobs {
		jobChan <- job
	}
	close(jobChan)

	// Wait for device workers to complete
	wg.Wait()
}

// deviceHashWorker is a worker goroutine that processes file hashing jobs for a specific device
func (dc *DirectoryCache) deviceHashWorker(deviceID uint64, jobs <-chan fileJob, results chan<- fileResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		entry, err := dc.processFileJob(job)
		results <- fileResult{
			entry: entry,
			err:   err,
			index: job.index,
		}
	}
}
