package dircachefilehash

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// checkForOrphanedIndexFiles checks for temporary index files from dead processes
func (dc *DirectoryCache) checkForOrphanedIndexFiles() error {
	metaDir := dc.MetaDir

	entries, err := os.ReadDir(metaDir)
	if err != nil {
		return fmt.Errorf("failed to read .dcfh directory: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()

		// Check for our temporary index file patterns
		if (strings.HasPrefix(name, "tmp-") || strings.HasPrefix(name, "scan-")) && strings.HasSuffix(name, ".idx") {
			pid := extractPidFromIndexFileName(name)
			if pid > 0 && !isProcessRunning(pid) {
				fmt.Fprintf(os.Stderr, "Warning: found orphaned index file from dead process: %s (PID %d no longer running)\n", name, pid)
			}
		}
	}

	return nil
}

// extractPidFromIndexFileName extracts the PID from index filenames like "tmp-1234-5678.idx" or "scan-1234-5678.idx"
func extractPidFromIndexFileName(filename string) int {
	// Remove .idx suffix
	if !strings.HasSuffix(filename, ".idx") {
		return 0
	}
	base := strings.TrimSuffix(filename, ".idx")

	// Split on dashes
	parts := strings.Split(base, "-")
	if len(parts) < 3 {
		return 0
	}

	// PID is the second part (index 1)
	pidStr := parts[1]
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0
	}

	return pid
}

// isProcessRunning checks if a process with the given PID is currently running
func isProcessRunning(pid int) bool {
	// Use kill(pid, 0) to check if process exists without sending a signal
	// This is a standard Unix way to check process existence
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true // Process exists and we can signal it
	}

	// Check the specific error — syscall.Kill returns raw syscall.Errno,
	// never wrapped, so direct type assertion is correct and cheaper than errors.As.
	if errno, ok := err.(syscall.Errno); ok { //nolint:errorlint // syscall.Kill never wraps errors
		if errno == syscall.ESRCH {
			return false // No such process
		}
		// EPERM means process exists but we don't have permission to signal it
		// This still means the process is running
		if errno == syscall.EPERM {
			return true
		}
	}

	// For any other error, assume process doesn't exist
	return false
}

// Stats returns statistics about the cache by loading the main index
func (dc *DirectoryCache) Stats() (int, int64, error) {
	skiplist, err := dc.LoadMainIndex()
	if err != nil {
		return 0, 0, err
	}

	var totalSize int64
	count := 0

	skiplist.ForEach(func(entry *binaryEntry, context string) bool {
		if !entry.IsDeleted() {
			totalSize += int64(entry.FileSize)
			count++
		}
		return true // Continue iteration
	})

	return count, totalSize, nil
}

// Length returns the total number of entries in the index (including deleted)
func (dc *DirectoryCache) Length() int {
	skiplist, err := dc.LoadMainIndex()
	if err != nil {
		return 0
	}
	return skiplist.Length()
}

// IndexTimestamp returns the timestamp stored in the main index header (v3+).
// Returns zero time and false if the index is not loaded or is v2.
func (dc *DirectoryCache) IndexTimestamp() (time.Time, bool) {
	if dc.mainIndex == nil {
		return time.Time{}, false
	}
	dc.mainIndex.mutex.RLock()
	defer dc.mainIndex.mutex.RUnlock()
	if dc.mainIndex.Data == nil {
		return time.Time{}, false
	}
	header := (*indexHeader)(unsafe.Pointer(&dc.mainIndex.Data[0]))
	if header.Version < TimestampMinVersion || header.Timestamp == 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(header.Timestamp), 0).UTC(), true
}

// ResolveMetaDir determines the metadata directory path.
// If dir ends with ".dcfh", it IS the metadata directory (external repo).
// Otherwise, ".dcfh" is appended (standard internal layout).
// If dir is empty, defaults to rootDir.
func ResolveMetaDir(dir, rootDir string) string {
	if dir == "" {
		dir = rootDir
	}
	if strings.HasSuffix(dir, ".dcfh") {
		return dir
	}
	return filepath.Join(dir, ".dcfh")
}

// initDirectoryCacheBase creates a partially-initialised DirectoryCache with
// struct fields set but no I/O performed (no directory creation, no config loading).
// metaDir must be the fully resolved metadata directory path.
func initDirectoryCacheBase(rootDir, metaDir string) *DirectoryCache {
	return &DirectoryCache{
		RootDir:       rootDir,
		MetaDir:       metaDir,
		IndexFile:     filepath.Join(metaDir, "main.idx"),
		CacheFile:     filepath.Join(metaDir, "cache.idx"),
		signature:     [4]byte{'d', 'c', 'f', 'h'},
		version:       CurrentIndexVersion,
		hasher:        sha1.New(),
		mmapIndex:     nil,
		ignoreManager: NewIgnoreManager(metaDir),
	}
}

// configureDirectoryCache loads config and ignore patterns from an existing .dcfh directory.
// All errors are non-fatal (logged to stderr) to match existing behaviour.
func configureDirectoryCache(dc *DirectoryCache, metaDir string) {
	config, err := LoadConfig(metaDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load config from %s: %v\n", metaDir, err)
	}
	dc.config = config

	if config != nil {
		performanceConfig := config.GetPerformanceConfig()
		dc.hashWorkers = performanceConfig.HashWorkers
		dc.indexLockTimeout = performanceConfig.IndexLockTimeout
	} else {
		dc.hashWorkers = 2      // fallback default
		dc.indexLockTimeout = 5 // fallback default (5 seconds)
	}

	if err := dc.ignoreManager.LoadIgnorePatterns(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load ignore patterns: %v\n", err)
	}
}

// CreateDirectoryCache creates a new dcfh repository on disk.
// Creates the .dcfh directory and empty index file if they don't exist.
// Use this for repository initialisation (dcfh init).
func CreateDirectoryCache(rootDir, metaDir string) *DirectoryCache {
	metaDir = ResolveMetaDir(metaDir, rootDir)
	dc := initDirectoryCacheBase(rootDir, metaDir)

	// Prevent creating .dcfh inside .dcfh (nested repositories).
	// External repos place metaDir elsewhere, so only check internal layout.
	if metaDir == filepath.Join(rootDir, ".dcfh") {
		if filepath.Base(rootDir) == ".dcfh" {
			fmt.Fprintf(os.Stderr, "Error: Cannot create .dcfh repository inside another .dcfh directory: %s\n", rootDir)
			return dc
		}

		dir := rootDir
		for {
			if filepath.Base(dir) == ".dcfh" {
				fmt.Fprintf(os.Stderr, "Error: Cannot create .dcfh repository inside .dcfh directory tree: %s\n", rootDir)
				return dc
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	if err := os.MkdirAll(dc.MetaDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to create .dcfh directory %s: %v\n", dc.MetaDir, err)
		return dc
	}

	configureDirectoryCache(dc, dc.MetaDir)

	// Create empty index if it doesn't exist
	if _, err := os.Stat(dc.IndexFile); os.IsNotExist(err) {
		if err := dc.createEmptyIndex(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to create empty index file %s: %v\n", dc.IndexFile, err)
		}
	}

	return dc
}

// OpenDirectoryCache opens an existing dcfh repository.
// Returns an error if the .dcfh directory does not exist.
// Use this for operations on existing repositories (status, update, dupes).
func OpenDirectoryCache(rootDir, metaDir string) (*DirectoryCache, error) {
	metaDir = ResolveMetaDir(metaDir, rootDir)
	dc := initDirectoryCacheBase(rootDir, metaDir)

	info, err := os.Stat(dc.MetaDir)
	if err != nil {
		return nil, fmt.Errorf("repository not found: %s does not exist", dc.MetaDir)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository not found: %s is not a directory", dc.MetaDir)
	}

	configureDirectoryCache(dc, dc.MetaDir)

	// For external repos, resolve rootDir from the config we just loaded
	if rootDir == "" && dc.config != nil {
		repoConfig := dc.config.GetRepositoryConfig()
		if repoConfig.Root != "" {
			dc.RootDir = repoConfig.Root
		}
	}

	return dc, nil
}

// NewDirectoryCache creates a new directory cache instance.
//
// Deprecated: Use CreateDirectoryCache for new repositories or OpenDirectoryCache for existing ones.
func NewDirectoryCache(rootDir, metaDir string) *DirectoryCache {
	return CreateDirectoryCache(rootDir, metaDir)
}

// ApplyConfigOverrides applies configuration overrides from the flags map
func (dc *DirectoryCache) ApplyConfigOverrides(flags map[string]string) error {
	if dc.config == nil {
		return fmt.Errorf("no configuration loaded, cannot apply overrides")
	}

	var allOverrides []string

	// Collect hash algorithm override
	if filehashOverride, exists := flags["filehash"]; exists {
		allOverrides = append(allOverrides, filehashOverride)
	}

	// Set symlink mode from flags or config
	if symlinkMode, exists := flags["symlinks"]; exists {
		dc.symlinkMode = symlinkMode
	} else if dc.config != nil {
		symlinkConfig := dc.config.GetSymlinkConfig()
		dc.symlinkMode = symlinkConfig.Mode
	} else {
		dc.symlinkMode = "none" // default fallback
	}

	// Set ignore deindex behavior from config
	if dc.config != nil {
		ignoreConfig := dc.config.GetIgnoreConfig()
		dc.ignoreIsDeindex = ignoreConfig.IgnoreIsDeindex
	} else {
		dc.ignoreIsDeindex = true // default fallback
	}

	// Set hash workers from flags or keep current config value
	if hashWorkersStr, exists := flags["hash_workers"]; exists {
		hashWorkers, err := strconv.Atoi(hashWorkersStr)
		if err != nil {
			return fmt.Errorf("invalid hash workers value '%s': %w", hashWorkersStr, err)
		}
		if err := ValidateHashWorkers(hashWorkers); err != nil {
			return fmt.Errorf("invalid hash workers configuration: %w", err)
		}
		dc.hashWorkers = hashWorkers
		allOverrides = append(allOverrides, "hash_workers:"+hashWorkersStr)
	}

	// Set index lock timeout from flags or keep current config value
	if indexLockTimeoutStr, exists := flags["index_lock_timeout"]; exists {
		indexLockTimeout, err := strconv.Atoi(indexLockTimeoutStr)
		if err != nil {
			return fmt.Errorf("invalid index lock timeout value '%s': %w", indexLockTimeoutStr, err)
		}
		if err := ValidateIndexLockTimeout(indexLockTimeout); err != nil {
			return fmt.Errorf("invalid index lock timeout configuration: %w", err)
		}
		dc.indexLockTimeout = indexLockTimeout
		allOverrides = append(allOverrides, "index_lock_timeout:"+indexLockTimeoutStr)
	}

	// Apply all overrides
	if len(allOverrides) > 0 {
		if err := dc.config.ApplyOverrides(allOverrides); err != nil {
			return fmt.Errorf("failed to apply configuration overrides: %w", err)
		}

		// Validate all configurations
		if err := dc.validateAllConfigs(); err != nil {
			return fmt.Errorf("invalid configuration after overrides: %w", err)
		}
	}

	return nil
}

// validateAllConfigs validates all configuration options
func (dc *DirectoryCache) validateAllConfigs() error {
	allConfig := dc.config.GetAllConfig()

	// Validate hash algorithm
	if err := ValidateHashAlgorithm(allConfig.Hash.Default); err != nil {
		return err
	}

	// Validate output format
	if err := ValidateOutputFormat(allConfig.Output.Format); err != nil {
		return err
	}

	// Validate verbose level
	if err := ValidateVerboseLevel(allConfig.Verbose.Level); err != nil {
		return err
	}

	// Validate debug flags
	if err := ValidateDebugFlags(allConfig.Verbose.Debug); err != nil {
		return err
	}

	// Validate symlink mode
	if err := ValidateSymlinkMode(allConfig.Symlink.Mode); err != nil {
		return err
	}

	// Validate hash workers
	if err := ValidateHashWorkers(allConfig.Performance.HashWorkers); err != nil {
		return err
	}

	// Validate index lock timeout
	if err := ValidateIndexLockTimeout(allConfig.Performance.IndexLockTimeout); err != nil {
		return err
	}

	return nil
}

// GetConfig returns the configuration instance
func (dc *DirectoryCache) GetConfig() *Config {
	return dc.config
}

// ResolveRepository finds the dcfh repository from startDir (or cwd if empty).
// Returns (rootDir, metaDir, error).
// Handles both internal (.dcfh subdirectory) and external (*.dcfh directory) repos.
func ResolveRepository(startDir string) (string, string, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Check if startDir itself IS a .dcfh directory (normal or external)
	base := filepath.Base(startDir)
	if strings.HasSuffix(base, ".dcfh") {
		if base == ".dcfh" {
			// Normal .dcfh directory — parent is repo root
			repoRoot := filepath.Dir(startDir)
			realDir, err := filepath.EvalSymlinks(repoRoot)
			if err != nil {
				realDir = repoRoot
			}
			return realDir, startDir, nil
		}
		// External .dcfh directory — read rootDir from config
		if root, ok := ResolveExternalRoot(startDir); ok {
			return root, startDir, nil
		}
		// Has no [repository] root — fall back to parent
		return filepath.Dir(startDir), startDir, nil
	}

	// Walk up the directory tree looking for .dcfh subdirectory
	dir := startDir
	for {
		metaDir := filepath.Join(dir, ".dcfh")
		if info, err := os.Stat(metaDir); err == nil && info.IsDir() {
			realDir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				realDir = dir
			}
			return realDir, filepath.Join(realDir, ".dcfh"), nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", "", fmt.Errorf("not a dcfh repository (or any of the parent directories): .dcfh directory not found")
}

// registerIndex tracks an mmap'd index file for memory protection
func (dc *DirectoryCache) registerIndex(indexType string, indexFile *mmapIndexFile) {
	if indexFile == nil {
		return
	}

	switch indexType {
	case "main":
		dc.mainIndex = indexFile
	case "cache":
		dc.cacheIndex = indexFile
	case "scan":
		// Scan indices are handled differently - add to the slice
		dc.scanIndices = append(dc.scanIndices, indexFile)
	}
}

// unregisterIndex removes tracking of an mmap'd index file
func (dc *DirectoryCache) unregisterIndex(indexType string, indexFile *mmapIndexFile) {
	if indexFile == nil {
		return
	}

	switch indexType {
	case "main":
		if dc.mainIndex == indexFile {
			dc.mainIndex = nil
		}
	case "cache":
		if dc.cacheIndex == indexFile {
			dc.cacheIndex = nil
		}
	case "scan":
		// Remove from scan indices slice
		for i, idx := range dc.scanIndices {
			if idx == indexFile {
				// Remove by swapping with last and truncating
				dc.scanIndices[i] = dc.scanIndices[len(dc.scanIndices)-1]
				dc.scanIndices = dc.scanIndices[:len(dc.scanIndices)-1]
				break
			}
		}
	}
}
