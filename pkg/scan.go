package dircachefilehash

import (
	"context"
	"errors"
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
func (dc *DirectoryCache) scanPath(ctx context.Context, paths []string, resultChan chan<- *scannedPath) error {
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
		if err := dc.scanPathRecursive(ctx, absPath, resultChan); err != nil {
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

	// Legacy callback pipeline only has the path; stat-using predicates
	// silently no-op via scanIgnoreDrops's error swallow.
	if dc.scanIgnoreDrops(relPath, nil, "shouldIndex") {
		return false
	}

	return true
}

// scanPathRecursive recursively scans a path and streams results as they're found
// This provides significant performance benefits:
// 1. No memory buildup - results are streamed immediately
// 2. Hwang-Lin comparison can start before scanning is complete
// 3. Maintains sorted order by processing paths alphabetically
func (dc *DirectoryCache) scanPathRecursive(ctx context.Context, rootPath string, resultChan chan<- *scannedPath) error {
	if IsDebugEnabled("scan") {
		VerboseLog(3, "scanPathRecursive: starting scan of rootPath: %s", rootPath)
	}
	pathQueue := []string{rootPath}
	metaDir := dc.MetaDir

	for len(pathQueue) > 0 {
		if err := ctx.Err(); err != nil {
			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Filesystem scan interrupted by shutdown\n")
			}
			return fmt.Errorf("scan interrupted: %w", err)
		}

		currentPath := pathQueue[0]
		pathQueue = pathQueue[1:]

		info, relPath, ok := dc.statAndFilter(currentPath)
		if !ok {
			continue
		}

		if info.Mode()&os.ModeSymlink != 0 {
			resolved, skip := dc.resolveSymlinkForScan(currentPath, info)
			if skip {
				continue
			}
			info = resolved
		}

		switch {
		case info.IsDir():
			if currentPath == metaDir {
				continue
			}
			pathQueue = dc.enqueueDirChildren(pathQueue, currentPath)
		case info.Mode().IsRegular(), info.Mode()&os.ModeSymlink != 0:
			if currentPath == dc.IndexFile || currentPath == dc.CacheFile {
				continue
			}
			resultChan <- makeScannedPath(currentPath, relPath, info)
		}
	}

	return nil
}

// statAndFilter lstat's path, computes the relative path, and applies
// the ignore-manager filter. Returns (info, relPath, true) to
// process, or a zero (nil, "", false) to skip entirely.
func (dc *DirectoryCache) statAndFilter(currentPath string) (os.FileInfo, string, bool) {
	info, err := os.Lstat(currentPath)
	if err != nil {
		return nil, "", false
	}
	relPath, err := filepath.Rel(dc.RootDir, currentPath)
	if err != nil {
		return nil, "", false
	}
	if dc.ignoreManager.ShouldIgnore(relPath) {
		return nil, "", false
	}
	if dc.scanIgnoreDrops(relPath, info, "statAndFilter") {
		return nil, "", false
	}
	return info, relPath, true
}

// scanIgnoreDrops evaluates the scan-time --ignore predicate against
// (relPath, info) and returns true when the entry should be filtered
// out. Errors from the predicate (hash predicates always; stat
// predicates when info is nil) are swallowed so we never drop on
// uncertainty — output-time evaluation in the comparison sink is
// authoritative. Reuses scratch storage on the DirectoryCache to keep
// the scan-walker hot path allocation-free.
func (dc *DirectoryCache) scanIgnoreDrops(relPath string, info os.FileInfo, where string) bool {
	if dc.scanIgnore == nil {
		return false
	}
	dc.scanFilterEnt.relPath = relPath
	dc.scanFilterEnt.info = info
	matched, err := dc.scanIgnore.Evaluate(&dc.scanFilterEnt, &dc.scanFilterCtx)
	if err != nil || !matched {
		return false
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "%s: ignoring path due to --ignore predicate: %s", where, relPath)
	}
	return true
}

// scanFilterEntry adapts (relPath, optional os.FileInfo) into a
// FilterEntry. Hash and stat accessors return errScanFilterUnavailable
// when their data isn't reachable; scanIgnoreDrops swallows the error
// so an --ignore --hash X predicate is a scan-time no-op rather than
// a false drop.
type scanFilterEntry struct {
	relPath string
	info    os.FileInfo
}

var errScanFilterUnavailable = errors.New("scan filter: predicate data not available at scan-time")

func (e *scanFilterEntry) RelativePath() (string, error) { return e.relPath, nil }
func (e *scanFilterEntry) IsDeleted() (bool, error)      { return false, nil }
func (e *scanFilterEntry) HashType() (uint16, error)     { return 0, errScanFilterUnavailable }
func (e *scanFilterEntry) HashString() (string, error)   { return "", errScanFilterUnavailable }

func (e *scanFilterEntry) FileSize() (uint64, error) {
	if e.info == nil {
		return 0, errScanFilterUnavailable
	}
	return uint64(e.info.Size()), nil
}

func (e *scanFilterEntry) Mode() (uint32, error) {
	if e.info == nil {
		return 0, errScanFilterUnavailable
	}
	return uint32(e.info.Mode()), nil
}

func (e *scanFilterEntry) MTimeWall() (uint64, error) {
	if e.info == nil {
		return 0, errScanFilterUnavailable
	}
	return timeWall(e.info.ModTime()), nil
}

func (e *scanFilterEntry) UID() (uint32, error) {
	sys, ok := e.statSys()
	if !ok {
		return 0, errScanFilterUnavailable
	}
	return sys.Uid, nil
}

func (e *scanFilterEntry) GID() (uint32, error) {
	sys, ok := e.statSys()
	if !ok {
		return 0, errScanFilterUnavailable
	}
	return sys.Gid, nil
}

func (e *scanFilterEntry) Dev() (uint32, error) {
	sys, ok := e.statSys()
	if !ok {
		return 0, errScanFilterUnavailable
	}
	return uint32(sys.Dev), nil
}

func (e *scanFilterEntry) CTimeWall() (uint64, error) {
	sys, ok := e.statSys()
	if !ok {
		return 0, errScanFilterUnavailable
	}
	return encodeWallTime(sys.Ctim.Sec, sys.Ctim.Nsec), nil
}

func (e *scanFilterEntry) statSys() (*syscall.Stat_t, bool) {
	if e.info == nil {
		return nil, false
	}
	sys, ok := e.info.Sys().(*syscall.Stat_t)
	if !ok || sys == nil {
		return nil, false
	}
	return sys, true
}

// resolveSymlinkForScan applies the symlink-mode policy to a symlink
// entry. For directory symlinks it returns the target's FileInfo so
// the scanner recurses in; for file symlinks it returns the original
// lstat info unchanged. Returns skip=true for symlinks that should be
// dropped (broken target, policy rejection, etc.).
func (dc *DirectoryCache) resolveSymlinkForScan(currentPath string, info os.FileInfo) (os.FileInfo, bool) {
	targetInfo, err := os.Stat(currentPath)
	if err != nil {
		return nil, true
	}
	if !targetInfo.IsDir() {
		// File symlinks stay as lstat'd symlink info; target is hashed separately.
		return info, false
	}
	if dc.shouldFollowDirSymlink(currentPath) {
		return targetInfo, false
	}
	return nil, true
}

// shouldFollowDirSymlink applies the symlink-mode policy (none /
// internal / external / all) to a directory symlink. Debug logs
// describe every decision.
func (dc *DirectoryCache) shouldFollowDirSymlink(currentPath string) bool {
	baseMode, strict := parseSymlinkMode(dc.symlinkMode)
	switch baseMode {
	case "none":
		if IsDebugEnabled("symlinks") {
			fmt.Fprintf(os.Stderr, "[SYMLINK] Skipping directory symlink (mode=none): %s\n", currentPath)
		}
		return false
	case "internal":
		return dc.checkDirSymlinkChain(currentPath, strict, true)
	case "external":
		return dc.checkDirSymlinkChain(currentPath, strict, false)
	case "all":
		if IsDebugEnabled("symlinks") {
			finalTarget, _ := filepath.EvalSymlinks(currentPath)
			fmt.Fprintf(os.Stderr, "[SYMLINK] Following directory symlink (mode=all): %s -> %s\n", currentPath, finalTarget)
		}
		return true
	default:
		if IsDebugEnabled("symlinks") {
			fmt.Fprintf(os.Stderr, "[SYMLINK] Unknown mode '%s', defaulting to 'all'\n", baseMode)
		}
		return true
	}
}

// checkDirSymlinkChain drives the internal/external symlink-chain
// check. internal=true requires all links in the chain to point
// inside dc.RootDir; internal=false requires all external.
func (dc *DirectoryCache) checkDirSymlinkChain(currentPath string, strict, internal bool) bool {
	allInternal, allExternal, err := dc.checkSymlinkChain(currentPath, strict)
	if err != nil {
		if IsDebugEnabled("symlinks") {
			fmt.Fprintf(os.Stderr, "[SYMLINK] Error checking symlink chain: %s - %v\n", currentPath, err)
		}
		return false
	}
	ok := allInternal
	label := "internal"
	if !internal {
		ok = allExternal
		label = "external"
	}
	if !ok {
		if IsDebugEnabled("symlinks") {
			finalTarget, _ := filepath.EvalSymlinks(currentPath)
			fmt.Fprintf(os.Stderr, "[SYMLINK] Skipping directory symlink (not %s): %s -> %s (root: %s, strict: %v)\n",
				label, currentPath, finalTarget, dc.RootDir, strict)
		}
		return false
	}
	if IsDebugEnabled("symlinks") {
		finalTarget, _ := filepath.EvalSymlinks(currentPath)
		fmt.Fprintf(os.Stderr, "[SYMLINK] Following %s directory symlink: %s -> %s (root: %s, strict: %v)\n",
			label, currentPath, finalTarget, dc.RootDir, strict)
	}
	return true
}

// enqueueDirChildren reads the given directory's entries, sorts them,
// and merges the new paths into the sorted pathQueue.
func (dc *DirectoryCache) enqueueDirChildren(pathQueue []string, currentPath string) []string {
	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return pathQueue
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	newPaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		newPaths = append(newPaths, filepath.Join(currentPath, entry.Name()))
	}
	return dc.insertSorted(pathQueue, newPaths)
}

// makeScannedPath builds the *scannedPath sent downstream. Debug
// logging lives here so both the regular-file and file-symlink
// branches emit consistent breadcrumbs.
func makeScannedPath(currentPath, relPath string, info os.FileInfo) *scannedPath {
	stat := info.Sys().(*syscall.Stat_t)
	kind := "file"
	if info.Mode()&os.ModeSymlink != 0 {
		kind = "symlink"
	}
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Scanned %s: %s\n", kind, relPath)
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "scanPathRecursive: found %s %s", kind, relPath)
	}
	return &scannedPath{
		AbsPath:  currentPath,
		RelPath:  relPath,
		Info:     info,
		StatInfo: stat,
	}
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
