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
)

// scanPath scans filesystem paths in sorted order and sends them via channel as they're found
func (dc *DirectoryCache) scanPath(ctx context.Context, sr *ScanRun, paths []string, resultChan chan<- *scannedPath) error {
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
		if err := dc.scanPathRecursive(ctx, sr, absPath, resultChan); err != nil {
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

// shouldIndex determines if a file should be included in the index based on:
// - Symlink following rules (for directory symlinks in the path)
// - Ignore patterns
// Returns false if the file should be treated as deleted/not indexed
func (dc *DirectoryCache) shouldIndex(sr *ScanRun, relPath string) bool {
	// Check if any parent directory is an unfollowed symlink
	dir := filepath.Dir(relPath)
	for dir != "." && dir != "/" && dir != "" {
		fullPath := filepath.Join(dc.RootDir, dir)
		if info, err := os.Lstat(fullPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			// This is a symlink - check if we would follow it
			if !dc.shouldFollowSymlink(sr, fullPath) {
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
	if sr.scanIgnoreDrops(relPath, nil, "shouldIndex") {
		return false
	}

	return true
}

// scanPathRecursive recursively scans a path and streams results as they're found
// This provides significant performance benefits:
// 1. No memory buildup - results are streamed immediately
// 2. Hwang-Lin comparison can start before scanning is complete
// 3. Maintains sorted order by processing paths alphabetically
func (dc *DirectoryCache) scanPathRecursive(ctx context.Context, sr *ScanRun, rootPath string, resultChan chan<- *scannedPath) error {
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

		info, relPath, ok := dc.statAndFilter(sr, currentPath)
		if !ok {
			continue
		}

		if info.Mode()&os.ModeSymlink != 0 {
			resolved, skip := dc.resolveSymlinkForScan(sr, currentPath, info)
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
func (dc *DirectoryCache) statAndFilter(sr *ScanRun, currentPath string) (os.FileInfo, string, bool) {
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
	if sr.scanIgnoreDrops(relPath, info, "statAndFilter") {
		return nil, "", false
	}
	return info, relPath, true
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
