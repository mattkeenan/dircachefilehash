package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ============================================================================
// TYPE DEFINITIONS
// ============================================================================

// scannedPath represents a file found during filesystem scanning
type scannedPath struct {
	AbsPath  string
	RelPath  string
	Info     os.FileInfo
	StatInfo *syscall.Stat_t
}

// hwangLinResult represents the result of Hwang-Lin comparison
type hwangLinResult struct {
	Type        hwangLinType
	ScannedPath *scannedPath // nil for deletions
	IndexEntry  *binaryEntry // nil for new files
	JobID       uint64       // for tracking hash jobs
	Hash        []byte       // computed hash (for new/modified files)
	HashType    uint16       // hash algorithm type
}

// hwangLinType represents the type of change detected
type hwangLinType int

const (
	HLUnchanged hwangLinType = iota // File exists in both and is unchanged
	HLNew                           // File only exists in scan (new file)
	HLModified                      // File exists in both but is modified
	HLDeleted                       // File only exists in index (deleted file)
)

// processedEntry represents a processed file ready for index writing
type processedEntry struct {
	RelPath   string
	Hash      []byte
	HashType  uint16
	FileInfo  os.FileInfo
	StatInfo  *syscall.Stat_t
	IsDeleted bool
}

// hashJobStart represents a hash job being started
type hashJobStart struct {
	JobID       uint64
	Cookie      uint64         // External cookie for caller tracking
	FilePath    string
	IndexEntry  binaryEntryRef // Entry to update with hash (mremap-safe)
	ScannedPath *scannedPath
}

// mockFileInfo implements os.FileInfo for deleted entries
type mockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return m.modTime }
func (m *mockFileInfo) IsDir() bool        { return m.mode.IsDir() }
func (m *mockFileInfo) Sys() interface{}   { return nil }

// Helper function for efficient slice removal (order doesn't matter)
func remove(s []uint64, i int) []uint64 {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

// simpleHashManager - coordinates hash job completion
type simpleHashManager struct {
	hashJobChan    chan *hashJobStart
	callFinishChan chan uint64 // job completion notifications
	wg             sync.WaitGroup
	shutdownChan   <-chan struct{} // shutdown notification
	closed         bool            // track if channel is closed
	closeMutex     sync.Mutex      // protect closed flag
}

// ============================================================================
// FILESYSTEM SCANNING FUNCTIONS
// ============================================================================

// scanPath scans filesystem paths in sorted order and sends them via channel as they're found
func (dc *DirectoryCache) scanPath(paths []string, resultChan chan<- *scannedPath, shutdownChan <-chan struct{}) error {
	defer VerboseEnter()()
	defer close(resultChan)

	// If empty paths, scan entire root directory
	if len(paths) == 0 {
		// Use "." to represent current directory relative to RootDir
		paths = []string{"."}
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "scanPath: scanning paths %v", paths)
	}

	// Load ignore patterns if not already loaded
	if err := dc.ignoreManager.LoadIgnorePatterns(); err != nil {
		return fmt.Errorf("failed to load ignore patterns: %w", err)
	}

	// Convert to absolute paths and clean them
	var absPaths []string
	if IsDebugEnabled("scan") {
		VerboseLog(3, "scanPath: dc.RootDir = %s", dc.RootDir)
	}
	for _, inputPath := range paths {
		absPath := inputPath
		if IsDebugEnabled("scan") {
			VerboseLog(3, "scanPath: processing inputPath = %s, IsAbs = %t", inputPath, filepath.IsAbs(inputPath))
		}
		if !filepath.IsAbs(inputPath) {
			absPath = filepath.Join(dc.RootDir, inputPath)
			if IsDebugEnabled("scan") {
				VerboseLog(3, "scanPath: joined to absPath = %s", absPath)
			}
		}
		cleanPath := filepath.Clean(absPath)
		if IsDebugEnabled("scan") {
			VerboseLog(3, "scanPath: cleaned to = %s", cleanPath)
		}
		absPaths = append(absPaths, cleanPath)
	}

	// Sort paths and remove redundant ones (subdirectories/subfiles of other paths)
	dedupedPaths := dc.deduplicatePaths(absPaths)
	if IsDebugEnabled("scan") {
		VerboseLog(3, "scanPath: deduplicated paths: %v", dedupedPaths)
	}

	// Scan each deduplicated path in sorted order, streaming results as found
	for _, absPath := range dedupedPaths {
		if IsDebugEnabled("scan") {
			VerboseLog(3, "scanPath: scanning deduplicated path: %s", absPath)
		}
		if err := dc.scanPathRecursive(absPath, resultChan, shutdownChan); err != nil {
			return fmt.Errorf("failed to scan path %s: %w", absPath, err)
		}
	}

	return nil
}

// deduplicatePaths sorts paths and removes any that are subdirectories/subfiles of others
// Example: ["/home/user/docs", "/home/user/docs/file.txt", "/home/user/photos"]
//
//	-> ["/home/user/docs", "/home/user/photos"]
//
// This optimisation reduces redundant scanning since "/home/user/docs/file.txt"
// will be found when we scan "/home/user/docs" anyway.
func (dc *DirectoryCache) deduplicatePaths(paths []string) []string {
	if len(paths) <= 1 {
		return paths
	}

	// Sort paths - this ensures parent directories come before their children
	sort.Strings(paths)

	var deduplicated []string
	for i, path := range paths {
		isRedundant := false

		// Check if this path is a subdirectory/subfile of any previous path
		for j := 0; j < i; j++ {
			prevPath := paths[j]

			// Check if current path is under the previous path
			if dc.isPathUnder(path, prevPath) {
				isRedundant = true
				break
			}
		}

		if !isRedundant {
			deduplicated = append(deduplicated, path)
		}
	}

	return deduplicated
}

// isPathUnder checks if childPath is under parentPath
func (dc *DirectoryCache) isPathUnder(childPath, parentPath string) bool {
	// Make sure both paths are clean
	childPath = filepath.Clean(childPath)
	parentPath = filepath.Clean(parentPath)

	// If paths are identical, child is not "under" parent
	if childPath == parentPath {
		return false
	}

	// Check if childPath starts with parentPath + separator
	parentWithSep := parentPath + string(filepath.Separator)
	return strings.HasPrefix(childPath, parentWithSep)
}

// isPathContained checks if targetPath is contained within containerPath
// This is used for symlink containment checking
func (dc *DirectoryCache) isPathContained(targetPath, containerPath string) bool {
	// Clean and make both paths absolute for proper comparison
	targetPath = filepath.Clean(targetPath)
	containerPath = filepath.Clean(containerPath)

	// Make both paths absolute
	if !filepath.IsAbs(targetPath) {
		var err error
		targetPath, err = filepath.Abs(targetPath)
		if err != nil {
			return false
		}
	}

	if !filepath.IsAbs(containerPath) {
		var err error
		containerPath, err = filepath.Abs(containerPath)
		if err != nil {
			return false
		}
	}

	// If paths are identical, target is contained
	if targetPath == containerPath {
		return true
	}

	// Check if targetPath starts with containerPath + separator
	containerWithSep := containerPath + string(filepath.Separator)
	return strings.HasPrefix(targetPath, containerWithSep)
}

// parseSymlinkMode parses the symlink mode string into base mode and strict flag
func parseSymlinkMode(mode string) (baseMode string, strict bool) {
	parts := strings.Split(mode, ",")
	if len(parts) == 0 {
		return "none", false
	}
	
	baseMode = strings.TrimSpace(parts[0])
	for i := 1; i < len(parts); i++ {
		if strings.TrimSpace(parts[i]) == "strict" {
			strict = true
		}
	}
	
	// Handle legacy "contained" mode by converting to "internal"
	if baseMode == "contained" {
		baseMode = "internal"
	}
	
	return baseMode, strict
}

// checkSymlinkChain checks all symlinks in a chain and returns whether they are all internal or all external
// Returns: isInternal, isExternal, error
// If strict mode is not needed, this just checks the final target
func (dc *DirectoryCache) checkSymlinkChain(symlinkPath string, strict bool) (allInternal, allExternal bool, err error) {
	// Start with assumption that chain could be either
	allInternal = true
	allExternal = true
	
	currentPath := symlinkPath
	visited := make(map[string]bool) // Prevent infinite loops
	
	for {
		// Check if we've seen this path before (loop detection)
		if visited[currentPath] {
			return false, false, fmt.Errorf("symlink loop detected at %s", currentPath)
		}
		visited[currentPath] = true
		
		// Check if current path is a symlink
		info, err := os.Lstat(currentPath)
		if err != nil {
			return false, false, err
		}
		
		if info.Mode()&os.ModeSymlink == 0 {
			// Not a symlink, we've reached the end
			break
		}
		
		// Read the symlink target
		target, err := os.Readlink(currentPath)
		if err != nil {
			return false, false, err
		}
		
		// Make target absolute if it's relative
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(currentPath), target)
		}
		
		// Check if this link is internal or external
		isInternal := dc.isPathContained(target, dc.RootDir)
		
		if strict {
			// In strict mode, all links must be of the same type
			if isInternal {
				allExternal = false
			} else {
				allInternal = false
			}
			
			// Early exit if we've determined the chain is mixed
			if !allInternal && !allExternal {
				return false, false, nil
			}
		}
		
		currentPath = target
	}
	
	// For non-strict mode, only check the final target
	if !strict {
		finalTarget, err := filepath.EvalSymlinks(symlinkPath)
		if err != nil {
			return false, false, err
		}
		isInternal := dc.isPathContained(finalTarget, dc.RootDir)
		return isInternal, !isInternal, nil
	}
	
	return allInternal, allExternal, nil
}

// detectUnfollowedSymlinkDirs scans the index to find directories that are now unfollowed symlinks
func (dc *DirectoryCache) detectUnfollowedSymlinkDirs(compareSkiplist *skiplistWrapper) map[string]bool {
	unfollowedDirs := make(map[string]bool)
	
	// Parse current symlink mode
	baseMode, _ := parseSymlinkMode(dc.symlinkMode)
	
	// If we're in "all" mode, no symlinks are unfollowed
	if baseMode == "all" {
		return unfollowedDirs
	}
	
	// Scan through all entries in the comparison skiplist
	current := compareSkiplist.skiplist.First()
	for current != nil {
		ref := current.Item()
		entry := ref.GetBinaryEntry()
		if entry != nil && !entry.IsDeleted() {
			path := entry.RelativePath()
			// Check each directory component in the path
			dir := filepath.Dir(path)
			for dir != "." && dir != "/" {
				// Check if this directory is a symlink that we would not follow
				fullPath := filepath.Join(dc.RootDir, dir)
				if info, err := os.Lstat(fullPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
					// This is a symlink - check if we would follow it
					shouldFollow := dc.shouldFollowSymlink(fullPath)
					if !shouldFollow {
						unfollowedDirs[dir] = true
						if IsDebugEnabled("symlinks") {
							fmt.Fprintf(os.Stderr, "[SYMLINK] Detected unfollowed symlink directory: %s\n", dir)
						}
					}
				}
				dir = filepath.Dir(dir)
			}
		}
		current = current.Next()
	}
	
	return unfollowedDirs
}

// shouldFollowSymlink checks if a symlink should be followed based on current mode
func (dc *DirectoryCache) shouldFollowSymlink(symlinkPath string) bool {
	baseMode, strict := parseSymlinkMode(dc.symlinkMode)
	
	if IsDebugEnabled("scan") {
		VerboseLog(3, "shouldFollowSymlink: path=%s, mode=%s (base=%s, strict=%v)", symlinkPath, dc.symlinkMode, baseMode, strict)
	}
	
	switch baseMode {
	case "none":
		return false
	case "all":
		return true
	case "internal":
		allInternal, _, err := dc.checkSymlinkChain(symlinkPath, strict)
		return err == nil && allInternal
	case "external":
		_, allExternal, err := dc.checkSymlinkChain(symlinkPath, strict)
		return err == nil && allExternal
	default:
		return true // Default to following for unknown modes
	}
}

// shouldIndex determines if a file should be included in the index based on:
// - Symlink following rules (for directory symlinks in the path)
// - Ignore patterns
// Returns false if the file should be treated as deleted/not indexed
func (dc *DirectoryCache) shouldIndex(relPath string) bool {
	// Check if any parent directory is an unfollowed symlink
	dir := filepath.Dir(relPath)
	for dir != "." && dir != "/" && dir != "" {
		fullPath := filepath.Join(dc.RootDir, dir)
		if info, err := os.Lstat(fullPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			// This is a symlink - check if we would follow it
			if !dc.shouldFollowSymlink(fullPath) {
				if IsDebugEnabled("symlinks") {
					fmt.Fprintf(os.Stderr, "[SYMLINK] File %s under unfollowed symlink %s\n", relPath, dir)
				}
				return false
			}
		}
		dir = filepath.Dir(dir)
	}

	// Check ignore patterns if deindexing is enabled
	if dc.ignoreIsDeindex && dc.ignoreManager.ShouldIgnore(relPath) {
		if IsDebugEnabled("scan") {
			VerboseLog(3, "shouldIndex: ignoring path due to ignore pattern: %s", relPath)
		}
		return false
	}

	return true
}


// scanPathRecursive recursively scans a path and streams results as they're found
// This provides significant performance benefits:
// 1. No memory buildup - results are streamed immediately
// 2. Hwang-Lin comparison can start before scanning is complete
// 3. Maintains sorted order by processing paths alphabetically
func (dc *DirectoryCache) scanPathRecursive(rootPath string, resultChan chan<- *scannedPath, shutdownChan <-chan struct{}) error {
	if IsDebugEnabled("scan") {
		VerboseLog(3, "scanPathRecursive: starting scan of rootPath: %s", rootPath)
	}
	// Use a priority queue (sorted slice) to ensure we process paths in alphabetical order
	// This ensures the output is naturally sorted
	pathQueue := []string{rootPath}

	for len(pathQueue) > 0 {
		// Check for shutdown
		select {
		case <-shutdownChan:
			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Filesystem scan interrupted by shutdown\n")
			}
			return fmt.Errorf("scan interrupted by shutdown")
		default:
		}

		// Always process the first path (lexicographically smallest)
		currentPath := pathQueue[0]
		pathQueue = pathQueue[1:]

		info, err := os.Lstat(currentPath)
		if err != nil {
			continue // Skip inaccessible paths
		}

		// Get relative path for ignore checking
		relPath, err := filepath.Rel(dc.RootDir, currentPath)
		if err != nil {
			continue
		}

		// Check if path should be ignored
		if dc.ignoreManager.ShouldIgnore(relPath) {
			continue
		}

		// Handle symlinks - determine if it's a file or directory symlink
		if info.Mode()&os.ModeSymlink != 0 {
			// Get info for the target to determine if it's a file or directory
			targetInfo, err := os.Stat(currentPath)
			if err != nil {
				continue // Skip broken symlinks
			}

			if targetInfo.IsDir() {
				// This is a directory symlink - apply symlink mode logic
				baseMode, strict := parseSymlinkMode(dc.symlinkMode)
				
				switch baseMode {
				case "none":
					// Don't follow directory symlinks - skip them
					if IsDebugEnabled("symlinks") {
						fmt.Fprintf(os.Stderr, "[SYMLINK] Skipping directory symlink (mode=none): %s\n", currentPath)
					}
					continue
					
				case "internal":
					// Only follow if symlink chain is internal to rootDir
					allInternal, _, err := dc.checkSymlinkChain(currentPath, strict)
					if err != nil {
						if IsDebugEnabled("symlinks") {
							fmt.Fprintf(os.Stderr, "[SYMLINK] Error checking symlink chain: %s - %v\n", currentPath, err)
						}
						continue // Skip problematic symlinks
					}
					
					if !allInternal {
						if IsDebugEnabled("symlinks") {
							finalTarget, _ := filepath.EvalSymlinks(currentPath)
							fmt.Fprintf(os.Stderr, "[SYMLINK] Skipping directory symlink (not internal): %s -> %s (root: %s, strict: %v)\n", 
								currentPath, finalTarget, dc.RootDir, strict)
						}
						continue
					}
					
					if IsDebugEnabled("symlinks") {
						finalTarget, _ := filepath.EvalSymlinks(currentPath)
						fmt.Fprintf(os.Stderr, "[SYMLINK] Following internal directory symlink: %s -> %s (root: %s, strict: %v)\n", 
							currentPath, finalTarget, dc.RootDir, strict)
					}
					info = targetInfo
					
				case "external":
					// Only follow if symlink chain is external to rootDir
					_, allExternal, err := dc.checkSymlinkChain(currentPath, strict)
					if err != nil {
						if IsDebugEnabled("symlinks") {
							fmt.Fprintf(os.Stderr, "[SYMLINK] Error checking symlink chain: %s - %v\n", currentPath, err)
						}
						continue // Skip problematic symlinks
					}
					
					if !allExternal {
						if IsDebugEnabled("symlinks") {
							finalTarget, _ := filepath.EvalSymlinks(currentPath)
							fmt.Fprintf(os.Stderr, "[SYMLINK] Skipping directory symlink (not external): %s -> %s (root: %s, strict: %v)\n", 
								currentPath, finalTarget, dc.RootDir, strict)
						}
						continue
					}
					
					if IsDebugEnabled("symlinks") {
						finalTarget, _ := filepath.EvalSymlinks(currentPath)
						fmt.Fprintf(os.Stderr, "[SYMLINK] Following external directory symlink: %s -> %s (root: %s, strict: %v)\n", 
							currentPath, finalTarget, dc.RootDir, strict)
					}
					info = targetInfo
					
				case "all":
					// Follow all directory symlinks
					if IsDebugEnabled("symlinks") {
						finalTarget, _ := filepath.EvalSymlinks(currentPath)
						fmt.Fprintf(os.Stderr, "[SYMLINK] Following directory symlink (mode=all): %s -> %s\n", 
							currentPath, finalTarget)
					}
					info = targetInfo
					
				default:
					// Default to "all" for unknown modes
					if IsDebugEnabled("symlinks") {
						fmt.Fprintf(os.Stderr, "[SYMLINK] Unknown mode '%s', defaulting to 'all'\n", baseMode)
					}
					info = targetInfo
				}
			}
			// For file symlinks, keep the original symlink info (don't replace with targetInfo)
			// The symlink will be recorded as a symlink, but we'll hash the target content
		}

		if info.IsDir() {
			// Skip the .dcfh directory
			indexDir := filepath.Dir(dc.IndexFile)
			if currentPath == indexDir {
				continue
			}

			// Read directory entries and add to queue in sorted order
			entries, err := os.ReadDir(currentPath)
			if err != nil {
				continue
			}

			// Sort entries for consistent ordering
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() < entries[j].Name()
			})

			// Add directory entries to queue, inserting in sorted position
			var newPaths []string
			for _, entry := range entries {
				fullPath := filepath.Join(currentPath, entry.Name())
				newPaths = append(newPaths, fullPath)
			}

			// Insert new paths into queue maintaining sorted order
			pathQueue = dc.insertSorted(pathQueue, newPaths)

		} else if info.Mode().IsRegular() {
			// Skip index files
			if currentPath == dc.IndexFile || currentPath == dc.CacheFile {
				continue
			}

			// Get system-specific file information
			stat := info.Sys().(*syscall.Stat_t)

			scannedPath := &scannedPath{
				AbsPath:  currentPath,
				RelPath:  relPath,
				Info:     info,
				StatInfo: stat,
			}

			// Stream result immediately - this gives us better performance
			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Scanned file: %s\n", relPath)
			}
			if IsDebugEnabled("scan") {
				VerboseLog(3, "scanPathRecursive: found file %s", relPath)
			}
			resultChan <- scannedPath
		} else if info.Mode()&os.ModeSymlink != 0 {
			// Handle file symlinks (directory symlinks were already handled above)
			// Skip index files
			if currentPath == dc.IndexFile || currentPath == dc.CacheFile {
				continue
			}

			// Get system-specific file information
			stat := info.Sys().(*syscall.Stat_t)

			scannedPath := &scannedPath{
				AbsPath:  currentPath,
				RelPath:  relPath,
				Info:     info,
				StatInfo: stat,
			}

			// Stream result immediately - this gives us better performance
			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Scanned symlink: %s\n", relPath)
			}
			if IsDebugEnabled("scan") {
				VerboseLog(3, "scanPathRecursive: found symlink %s", relPath)
			}
			resultChan <- scannedPath
		}
	}

	return nil
}

// insertSorted inserts new paths into an existing sorted slice maintaining order
func (dc *DirectoryCache) insertSorted(existing []string, newPaths []string) []string {
	if len(newPaths) == 0 {
		return existing
	}
	if len(existing) == 0 {
		// Just sort and return new paths
		sort.Strings(newPaths)
		return newPaths
	}

	// Merge the two sorted slices
	result := make([]string, 0, len(existing)+len(newPaths))

	// Sort new paths first
	sort.Strings(newPaths)

	i, j := 0, 0
	for i < len(existing) && j < len(newPaths) {
		if existing[i] <= newPaths[j] {
			result = append(result, existing[i])
			i++
		} else {
			result = append(result, newPaths[j])
			j++
		}
	}

	// Append remaining elements
	for i < len(existing) {
		result = append(result, existing[i])
		i++
	}
	for j < len(newPaths) {
		result = append(result, newPaths[j])
		j++
	}

	return result
}

// ============================================================================
// HWANG-LIN COMPARISON ALGORITHM
// ============================================================================

// hwangLinCompareToSkiplist performs Hwang-Lin comparison and builds scan index + skiplist
func (dc *DirectoryCache) hwangLinCompareToSkiplist(
	scanChan <-chan *scannedPath,
	compareSkiplist *skiplistWrapper,
	scanSkiplist *skiplistWrapper,
	scanFileName string,
	hashJobManager *simpleHashManager,
	callStartChan chan<- uint64,
) error {
	defer VerboseEnter()()
	
	// Track if we exit early to drain the channel
	earlyExit := false
	defer func() {
		if earlyExit {
			// Drain any remaining items from scanChan in a separate goroutine
			go func() {
				for range scanChan {
					// Just consume to prevent blocking
				}
			}()
		}
	}()
	
	var currentScanned *scannedPath
	var scanChanOpen bool = true
	currentIndex := compareSkiplist.skiplist.First()
	jobIDCounter := uint64(1)

	if IsDebugEnabled("scan") {
		VerboseLog(3, "hwangLinCompareToSkiplist: starting comparison, compareSkiplist length = %d", compareSkiplist.Length())
	}

	// Read first scanned path
	if scanChanOpen {
		currentScanned, scanChanOpen = <-scanChan
		if IsDebugEnabled("scan") && currentScanned != nil {
			VerboseLog(3, "hwangLinCompareToSkiplist: first scanned file = %s", currentScanned.RelPath)
		}
	}

	for scanChanOpen || currentIndex != nil {

		var cmp int
		if !scanChanOpen {
			cmp = 1 // No more scanned files, only index entries remain (deletions)
		} else if currentIndex == nil {
			cmp = -1 // No more index entries, only scanned files remain (new files)
		} else {
			// Compare paths
			indexRef := currentIndex.Item()
			indexEntry := indexRef.GetBinaryEntry()
			if indexEntry == nil {
				return fmt.Errorf("GetBinaryEntry returned nil for index entry - this should never happen")
			}
			// Create string copy to avoid use-after-free when scan memory is unmapped
			indexPath := string([]byte(indexEntry.RelativePath()))
			cmp = strings.Compare(currentScanned.RelPath, indexPath)

		}

		if cmp == 0 {
			// File exists in both - check if changed
			indexRef := currentIndex.Item()
			indexEntry := indexRef.GetBinaryEntry()
			if indexEntry == nil {
				return fmt.Errorf("GetBinaryEntry returned nil for index entry - this should never happen")
			}

			// Skip deleted entries in the index
			if indexEntry.IsDeleted() {
				currentIndex = currentIndex.Next()
				continue
			}
			
			// Check if this file should still be indexed
			if !dc.shouldIndex(currentScanned.RelPath) {
				// File exists but should no longer be indexed (ignored or under unfollowed symlink)
				// Create a deleted entry
				deletedEntry, err := dc.appendEntryToScanIndex(scanFileName, currentScanned)
				if err != nil {
					return fmt.Errorf("failed to create deleted scan index entry: %w", err)
				}
				
				// Mark as deleted and preserve existing hash
				deletedEntry.SetDeleted()
				copy(deletedEntry.Hash[:], indexEntry.Hash[:])
				deletedEntry.HashType = indexEntry.HashType
				
				// Insert into scan skiplist
				deletedRef := createBinaryEntryRef(deletedEntry, dc.currentScan)
				scanSkiplist.Insert(deletedRef, ScanContext)
				
				// Advance both
				if scanChanOpen {
					currentScanned, scanChanOpen = <-scanChan
				}
				currentIndex = currentIndex.Next()
				continue
			}

			if dc.isFileChangedFromScanned(indexEntry, currentScanned) {
				// File modified - create scan index entry and submit for hashing
				scanEntry, err := dc.appendEntryToScanIndex(scanFileName, currentScanned)
				if err != nil {
					return fmt.Errorf("failed to create scan index entry: %w", err)
				}

				// Insert into scan skiplist using binaryEntryRef
				scanRef := createBinaryEntryRef(scanEntry, dc.currentScan)
				scanSkiplist.Insert(scanRef, ScanContext)

				// Submit for async hashing
				jobID := jobIDCounter
				jobIDCounter++

				hashJob := &hashJobStart{
					JobID:       jobID,
					FilePath:    currentScanned.AbsPath,
					IndexEntry:  createBinaryEntryRef(scanEntry, dc.currentScan), // Hash worker will update this safely
					ScannedPath: currentScanned,
				}

				// Check for shutdown before submitting new job
				if hashJobManager.IsShuttingDown() {
					if IsDebugEnabled("scanning") {
						fmt.Fprintf(os.Stderr, "[SCAN] Skipping hash job submission during shutdown for file: %s\n", currentScanned.RelPath)
					}
					// Don't return error - just stop submitting new jobs and continue with what we have
					// The scan skiplist already has the entry, we just won't hash it
					earlyExit = true
					break
				}

				hashJobManager.SubmitHashJob(hashJob, callStartChan)

			} else {
				// File unchanged - DO NOT create scan entry
				// The existing entry in main/cache index is already correct
				// Just continue to next file
				
				if IsDebugEnabled("scan") {
					VerboseLog(3, "hwangLinCompareToSkiplist: file unchanged, skipping: %s", currentScanned.RelPath)
				}
			}

			// Advance both
			if scanChanOpen {
				currentScanned, scanChanOpen = <-scanChan
			}
			currentIndex = currentIndex.Next()

		} else if cmp < 0 {
			// File only in scan - new file
			// Check if this file should be indexed
			if !dc.shouldIndex(currentScanned.RelPath) {
				// File should not be indexed (ignored or under unfollowed symlink)
				// Skip without creating entry
				if scanChanOpen {
					currentScanned, scanChanOpen = <-scanChan
				}
				continue
			}
			
			// Create scan index entry and submit for hashing
			scanEntry, err := dc.appendEntryToScanIndex(scanFileName, currentScanned)
			if err != nil {
				return fmt.Errorf("failed to create scan index entry: %w", err)
			}

			// Insert into scan skiplist using binaryEntryRef
			scanRef := createBinaryEntryRef(scanEntry, dc.currentScan)
			scanSkiplist.Insert(scanRef, ScanContext)

			// Submit for async hashing
			jobID := jobIDCounter
			jobIDCounter++

			hashJob := &hashJobStart{
				JobID:       jobID,
				FilePath:    currentScanned.AbsPath,
				IndexEntry:  createBinaryEntryRef(scanEntry, dc.currentScan), // Hash worker will update this safely
				ScannedPath: currentScanned,
			}

			// Check for shutdown before submitting new job
			if hashJobManager.IsShuttingDown() {
				if IsDebugEnabled("scanning") {
					fmt.Fprintf(os.Stderr, "[SCAN] Skipping hash job submission during shutdown for file: %s\n", currentScanned.RelPath)
				}
				// Don't return error - just stop submitting new jobs and continue with what we have
				// The scan skiplist already has the entry, we just won't hash it
				earlyExit = true
				break
			}

			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Submitting hash job %d for file: %s\n", jobID, currentScanned.RelPath)
			}
			hashJobManager.SubmitHashJob(hashJob, callStartChan)

			// Advance scan
			if scanChanOpen {
				currentScanned, scanChanOpen = <-scanChan
			}

		} else {
			// File only in index - deleted file, mark as deleted in scan skiplist
			indexRef := currentIndex.Item()
			indexEntry := indexRef.GetBinaryEntry()
			if indexEntry == nil {
				return fmt.Errorf("GetBinaryEntry returned nil for index entry - this should never happen")
			}
			
			// Get the relative path for checking
			relPath := string([]byte(indexEntry.RelativePath()))
			
			// Check if this file should still be indexed based on symlink and ignore rules
			if dc.shouldIndex(relPath) {
				// File should be indexed but isn't present - it's been deleted from disk
				// Fall through to create deleted entry
			} else {
				// File should not be indexed (due to symlink or ignore rules)
				// Skip without creating deleted entry
				currentIndex = currentIndex.Next()
				continue
			}

			// Skip already deleted entries
			if !indexEntry.IsDeleted() {
				// Create a deleted entry in scan index using metadata from existing entry
				// We need to reconstruct os.FileInfo and syscall.Stat_t from the index entry
				mockInfo := &mockFileInfo{
					name:    filepath.Base(indexEntry.RelativePath()),
					size:    int64(indexEntry.FileSize),
					mode:    os.FileMode(indexEntry.Mode),
					modTime: timeFromWall(indexEntry.MTimeWall),
				}
				mockStat := &syscall.Stat_t{
					Dev:  uint64(indexEntry.Dev),
					Ino:  uint64(indexEntry.Ino),
					Mode: indexEntry.Mode,
					Uid:  indexEntry.UID,
					Gid:  indexEntry.GID,
					Ctim: syscall.Timespec{Sec: timeFromWall(indexEntry.CTimeWall).Unix(), Nsec: 0},
					Mtim: syscall.Timespec{Sec: timeFromWall(indexEntry.MTimeWall).Unix(), Nsec: 0},
				}

				// Create string copy to avoid use-after-free when scan memory is unmapped
				deletedEntry, err := dc.appendEntryToScanIndex(scanFileName, &scannedPath{
					RelPath:  string([]byte(indexEntry.RelativePath())),
					Info:     mockInfo,
					StatInfo: mockStat,
				})
				if err != nil {
					return fmt.Errorf("failed to create deleted scan index entry: %w", err)
				}

				// Mark as deleted and copy hash
				deletedEntry.SetDeleted()
				copy(deletedEntry.Hash[:], indexEntry.Hash[:])
				deletedEntry.HashType = indexEntry.HashType

				// Insert into scan skiplist using binaryEntryRef
				deletedRef := createBinaryEntryRef(deletedEntry, dc.currentScan)
				scanSkiplist.Insert(deletedRef, ScanContext)
			}

			// Advance index
			currentIndex = currentIndex.Next()
		}
	}

	return nil
}

// hwangLinCompare performs Hwang-Lin algorithm comparison between scanned filesystem and skiplist
// Now with asynchronous hash job processing - hash jobs don't block the comparison

// isFileChangedFromScanned checks if a file has changed by comparing with scanned info
func (dc *DirectoryCache) isFileChangedFromScanned(indexEntry *binaryEntry, scanned *scannedPath) bool {
	stat := scanned.StatInfo

	// Quick size check
	if indexEntry.FileSize != uint64(scanned.Info.Size()) {
		return true
	}

	// Check ownership
	if indexEntry.UID != stat.Uid || indexEntry.GID != stat.Gid {
		return true
	}

	// Check mode
	if indexEntry.Mode != uint32(scanned.Info.Mode()) {
		return true
	}

	// Check timestamps using wall time encoding
	currentCTime := encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	currentMTime := encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)

	return indexEntry.CTimeWall != currentCTime || indexEntry.MTimeWall != currentMTime
}

// ============================================================================
// HASH JOB MANAGEMENT
// ============================================================================

// NewSimpleHashManager creates a new simple hash manager
func (dc *DirectoryCache) newSimpleHashManager(numWorkers int, callFinishChan chan uint64, shutdownChan <-chan struct{}) *simpleHashManager {
	manager := &simpleHashManager{
		hashJobChan:    make(chan *hashJobStart, 100),
		callFinishChan: callFinishChan,
		shutdownChan:   shutdownChan,
	}

	// Start workers
	for i := 0; i < numWorkers; i++ {
		manager.wg.Add(1)
		go manager.hashWorker(dc)
	}

	return manager
}

// IsShuttingDown checks if shutdown has been requested
func (hjm *simpleHashManager) IsShuttingDown() bool {
	select {
	case <-hjm.shutdownChan:
		return true
	default:
		return false
	}
}

// SubmitHashJob submits a hash job and signals the start
func (hjm *simpleHashManager) SubmitHashJob(job *hashJobStart, callStartChan chan<- uint64) {
	hjm.hashJobChan <- job
	callStartChan <- job.JobID // Signal job started
}

// FinishSubmitting signals that no more hash jobs will be submitted
func (hjm *simpleHashManager) FinishSubmitting() {
	hjm.closeMutex.Lock()
	defer hjm.closeMutex.Unlock()

	if !hjm.closed {
		close(hjm.hashJobChan)
		hjm.closed = true
	}
}

// hashWorker processes hash jobs and updates entries directly in scan index mmap
func (hjm *simpleHashManager) hashWorker(dc *DirectoryCache) {
	defer hjm.wg.Done()
	
	var currentJob *hashJobStart // Track current job for interruption handling

	for {
		select {
		case job, ok := <-hjm.hashJobChan:
			if !ok {
				// Channel closed, worker should exit
				return
			}

			currentJob = job
			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Hashing file: %s (job %d)\n", job.ScannedPath.RelPath, job.JobID)
			}

			// Hash the file and update binaryEntry directly in mmap memory
			// For symlinks, we hash the target path, not the target file contents
			var hashBytes []byte
			var hashType uint16
			var err error

			// Check if this is a symlink by examining the file mode
			if job.ScannedPath.Info.Mode()&os.ModeSymlink != 0 {
				// This is a symlink - hash the target path
				hashBytes, hashType, err = dc.hashSymlinkTargetToBytes(job.FilePath)
			} else {
				// Regular file - hash the file contents with interruptible hashing
				hashBytes, hashType, err = dc.HashFileInterruptibleToBytes(job.FilePath, hjm.shutdownChan)
			}

			if err == nil {
				// Update the binaryEntry directly in the scan index mmap memory
				// This provides zero-copy updates to the scan index file
				if updateErr := dc.updateBinaryEntryHash(job.IndexEntry, hashBytes, hashType); updateErr != nil {
					fmt.Fprintf(os.Stderr, "[ERROR] Failed to update binary entry hash: %v\n", updateErr)
				}
			}

			if IsDebugEnabled("scanning") {
				if err != nil {
					fmt.Fprintf(os.Stderr, "[SCAN] Hash failed for file: %s (job %d) - %v\n", job.ScannedPath.RelPath, job.JobID, err)
				} else {
					fmt.Fprintf(os.Stderr, "[SCAN] Hash completed for file: %s (job %d)\n", job.ScannedPath.RelPath, job.JobID)
				}
			}

			// Signal completion
			hjm.callFinishChan <- job.JobID
			currentJob = nil

		case <-hjm.shutdownChan:
			// Shutdown requested
			if currentJob != nil {
				// We were processing a job - send interruption signal
				// Use a special job ID (0) to indicate interruption
				if IsDebugEnabled("scanning") {
					fmt.Fprintf(os.Stderr, "[SCAN] Worker interrupted during job %d, sending interruption signal\n", currentJob.JobID)
				}
				// Send the actual job ID so monitor knows which job was interrupted
				hjm.callFinishChan <- currentJob.JobID
			}
			return
		}
	}
}

// Shutdown gracefully shuts down the hash manager
func (hjm *simpleHashManager) Shutdown() {
	hjm.closeMutex.Lock()
	if !hjm.closed {
		close(hjm.hashJobChan)
		hjm.closed = true
	}
	hjm.closeMutex.Unlock()
	
	// Wait for all workers to exit with a timeout
	done := make(chan struct{})
	go func() {
		hjm.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		// All workers exited normally
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] All hash workers exited cleanly\n")
		}
	case <-time.After(60 * time.Second):
		// Timeout - workers didn't exit in time
		fmt.Fprintf(os.Stderr, "[WARNING] Hash workers did not exit within 60 seconds, proceeding anyway\n")
	}
	
	// After workers have exited (or timed out), close the completion channel
	// This signals to the monitor that no more completions will come
	close(hjm.callFinishChan)
}

// updateBinaryEntryHash safely updates the hash in a binaryEntry
func (dc *DirectoryCache) updateBinaryEntryHash(entryRef binaryEntryRef, hash []byte, hashType uint16) error {
	entry := entryRef.GetBinaryEntry()
	if entry == nil {
		return fmt.Errorf("GetBinaryEntry returned nil for hash update - this should never happen")
	}

	// Clear the hash field first
	for i := range entry.Hash {
		entry.Hash[i] = 0
	}

	// Copy the new hash
	copy(entry.Hash[:], hash)
	entry.HashType = hashType

	return nil
}

// monitorJobs tracks hash job starts and completions
func (dc *DirectoryCache) monitorJobs(
	callStartChan <-chan uint64,
	callFinishChan <-chan uint64,
	collectionStop <-chan struct{},
	shutdownChan <-chan struct{},
) {
	var jobs []uint64 // Track pending hash jobs
	var preCompletions []uint64 // Queue for completions that arrived before their start
	stopped := false
	var stopTimer *time.Timer

	for {
		var timerChan <-chan time.Time
		if stopTimer != nil {
			timerChan = stopTimer.C
		}

		select {
		case jobID := <-callStartChan:
			jobs = append(jobs, jobID)
			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Job %d started, pending jobs: %d\n", jobID, len(jobs))
			}
			
			// Check if this job already completed prematurely
			for i, preCompletedID := range preCompletions {
				if preCompletedID == jobID {
					// Remove from jobs and preCompletions
					jobs = remove(jobs, len(jobs)-1) // Remove the job we just added
					preCompletions = remove(preCompletions, i)
					if IsDebugEnabled("scanning") {
						fmt.Fprintf(os.Stderr, "[SCAN] Job %d completed prematurely, removing from pending\n", jobID)
					}
					break
				}
			}

		case completedJobID, ok := <-callFinishChan:
			if !ok {
				// Channel closed - no more completions will come
				if IsDebugEnabled("scanning") {
					fmt.Fprintf(os.Stderr, "[SCAN] Completion channel closed, %d jobs still pending\n", len(jobs))
				}
				return
			}
			
			// Remove completed job from jobs slice
			found := false
			for i, id := range jobs {
				if id == completedJobID {
					jobs = remove(jobs, i)
					found = true
					break
				}
			}
			if IsDebugEnabled("scanning") {
				if found {
					fmt.Fprintf(os.Stderr, "[SCAN] Job %d completed, pending jobs: %d\n", completedJobID, len(jobs))
				} else {
					fmt.Fprintf(os.Stderr, "[SCAN] Job %d completed prematurely, queuing completion\n", completedJobID)
				}
			}
			
			// If job not found, it completed before start signal - queue it
			if !found {
				preCompletions = append(preCompletions, completedJobID)
			}
			
			// Check if we're done after processing a completion
			if stopped && len(jobs) == 0 {
				if IsDebugEnabled("scanning") {
					fmt.Fprintf(os.Stderr, "[SCAN] Monitor exiting: stopped=true, pending jobs=0\n")
				}
				return
			}

		case <-collectionStop:
			stopped = true
			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Monitor received stop signal, pending jobs: %d", len(jobs))
				if len(jobs) > 0 {
					fmt.Fprintf(os.Stderr, " - stuck jobs: %v", jobs)
				}
				fmt.Fprintf(os.Stderr, "\n")
			}
			// Start timeout timer if we have pending jobs and timer not already started
			if len(jobs) > 0 && stopTimer == nil {
				stopTimer = time.NewTimer(5 * time.Second)
			}
			// Check if we're already done
			if len(jobs) == 0 {
				if IsDebugEnabled("scanning") {
					fmt.Fprintf(os.Stderr, "[SCAN] Monitor exiting: stopped=true, pending jobs=0\n")
				}
				return
			}

		case <-timerChan:
			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Timeout waiting for jobs to complete, pending jobs: %d - stuck jobs: %v\n", len(jobs), jobs)
			}
			return

		case <-shutdownChan:
			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Monitor received shutdown signal, exiting immediately with %d pending jobs\n", len(jobs))
			}
			return
		}
	}
}

// ============================================================================
// RESULT PROCESSING FUNCTIONS
// ============================================================================

// getHashSize returns hash size based on type
func (dc *DirectoryCache) getHashSize(hashType uint16) int {
	switch hashType {
	case HashTypeSHA1:
		return HashSizeSHA1
	case HashTypeSHA256:
		return HashSizeSHA256
	case HashTypeSHA512:
		return HashSizeSHA512
	default:
		return HashSizeSHA1
	}
}

// ============================================================================
// MAIN SCAN FUNCTION
// ============================================================================

// PerformHwangLinScan performs a complete Hwang-Lin scan with asynchronous hash job coordination

// PerformHwangLinScanToSkiplist performs Hwang-Lin scan and builds a skiplist directly with scan index files
func (dc *DirectoryCache) performHwangLinScanToSkiplist(shutdownChan <-chan struct{}, paths []string, compareSkiplist *skiplistWrapper) (*skiplistWrapper, error) {
	defer VerboseEnter()()
	// Synchronise concurrent scans - only one scan per DirectoryCache at a time
	dc.scanMutex.Lock()
	defer dc.scanMutex.Unlock()

	// If a scan is already in progress, wait for it and return the same results
	if dc.scanInProgress {
		// TODO: Handle race condition where files change between when the first scan
		// started and when this concurrent caller started. Currently we return the
		// results from the first scan, but ideally we should detect if files changed
		// and re-run the scan if necessary.
		if dc.lastScanError != nil {
			return nil, dc.lastScanError
		}
		return dc.lastScanResult, nil
	}

	// Mark scan as in progress
	dc.scanInProgress = true
	defer func() {
		dc.scanInProgress = false
	}()

	// Create result skiplist for scan entries
	scanSkiplist := NewSkiplistWrapper(16, ScanContext)

	// Generate scan index filename for this operation
	scanFileName := dc.generateScanFileName()

	// Initialise scan index with mmap
	if err := dc.initialiseScanIndex(scanFileName); err != nil {
		return nil, fmt.Errorf("failed to initialise scan index: %w", err)
	}

	// Create channels for streaming data
	scanChan := make(chan *scannedPath, 50)
	callStartChan := make(chan uint64, 100)
	callFinishChan := make(chan uint64, 100)
	collectionStop := make(chan struct{})

	// Create hash job manager for concurrent hashing
	hashJobManager := dc.newSimpleHashManager(dc.hashWorkers, callFinishChan, shutdownChan)
	defer hashJobManager.Shutdown()

	// Start filesystem scan
	var scanWg sync.WaitGroup
	scanWg.Add(1)
	go func() {
		defer scanWg.Done()
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Starting filesystem scan\n")
		}
		if err := dc.scanPath(paths, scanChan, shutdownChan); err != nil {
			fmt.Fprintf(os.Stderr, "Scan error: %v\n", err)
		}
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Filesystem scan completed\n")
		}
	}()

	// Start modified Hwang-Lin comparison that builds scan index and skiplist
	var compareWg sync.WaitGroup
	compareWg.Add(1)
	go func() {
		defer compareWg.Done()
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Starting Hwang-Lin comparison\n")
		}
		if err := dc.hwangLinCompareToSkiplist(scanChan, compareSkiplist, scanSkiplist, scanFileName, hashJobManager, callStartChan); err != nil {
			fmt.Fprintf(os.Stderr, "Compare error: %v\n", err)
		}
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Hwang-Lin comparison completed\n")
		}
	}()

	// Monitor hash jobs
	var monitorWg sync.WaitGroup
	monitorWg.Add(1)
	go func() {
		defer monitorWg.Done()
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Starting job monitor\n")
		}
		dc.monitorJobs(callStartChan, callFinishChan, collectionStop, shutdownChan)
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Job monitor completed\n")
		}
	}()

	// Wait for scan to complete
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Waiting for filesystem scan to complete\n")
	}
	scanWg.Wait()
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Filesystem scan wait completed\n")
	}

	// Check if shutdown occurred during scan
	select {
	case <-shutdownChan:
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[SCAN] Shutdown detected after filesystem scan, returning partial skiplist with %d entries\n", scanSkiplist.Length())
		}
		// Return partial skiplist with error to indicate incomplete scan
		return scanSkiplist, fmt.Errorf("operation interrupted by shutdown")
	default:
	}

	// Wait for comparison to complete
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Waiting for comparison to complete\n")
	}
	compareWg.Wait()
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Comparison wait completed\n")
	}

	// Check if shutdown occurred during comparison
	select {
	case <-shutdownChan:
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[SCAN] Shutdown detected after comparison, returning partial skiplist with %d entries\n", scanSkiplist.Length())
		}
		// Return partial skiplist with error to indicate incomplete scan
		return scanSkiplist, fmt.Errorf("operation interrupted by shutdown")
	default:
	}

	// Signal that no more hash jobs will be submitted
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Finishing hash job submission\n")
	}
	hashJobManager.FinishSubmitting()
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Hash job submission finished\n")
	}

	// Signal monitoring to stop and wait for all jobs to finish
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Stopping job monitor\n")
	}
	close(collectionStop)
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Waiting for job monitor to complete\n")
	}
	monitorWg.Wait()
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Job monitor wait completed\n")
	}

	if GetVerboseLevel() > 1 {
		fmt.Printf("Scan to skiplist completed\n")
	}

	// Store results for concurrent callers
	dc.lastScanResult = scanSkiplist
	dc.lastScanError = nil

	return scanSkiplist, nil
}
