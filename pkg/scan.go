package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// hwangLinType represents the type of change detected
type hwangLinType int

const (
	HLUnchanged hwangLinType = iota // File exists in both and is unchanged
	HLNew                           // File only exists in scan (new file)
	HLModified                      // File exists in both but is modified
	HLDeleted                       // File only exists in index (deleted file)
)

// hashJobStart represents a hash job being started
type hashJobStart struct {
	JobID       uint64
	Cookie      uint64 // External cookie for caller tracking
	FilePath    string
	IndexEntry  binaryEntryRef // Entry to update with hash (mremap-safe) - DEPRECATED for v0.7
	ScannedPath *scannedPath

	// v0.7 unified entry support - works for both mmap and heap entries
	Entry BinaryEntryInterface // Unified interface for all entry types
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
func (m *mockFileInfo) Sys() any           { return nil }

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
		for j := range i {
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
	dcfhDir := dc.DcfhDir

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
			if currentPath == dcfhDir {
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

// hwangLinCompare performs Hwang-Lin algorithm comparison between scanned filesystem and skiplist
// Note: The old hwangLinCompareToSkiplist function has been moved to v0.6/pkg/scan.go as part of
// the v0.7 unified architecture migration. Use hwangLinUnified() with CallbackScanCoordinator instead.

// ============================================================================
// RESULT PROCESSING FUNCTIONS
// ============================================================================

// ============================================================================
// MAIN SCAN FUNCTION
// ============================================================================

// PerformHwangLinScan performs a complete Hwang-Lin scan with asynchronous hash job coordination

// PerformHwangLinScanToSkiplist has been moved to v0.6/pkg/scan.go as part of the v0.7 unified
// architecture migration. Use runStatusWorkflowUnified() instead, which provides the
// same functionality using the hwangLinUnified() algorithm with proper callbacks.
