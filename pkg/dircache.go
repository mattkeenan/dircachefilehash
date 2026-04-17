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
	dcfhDir := dc.DcfhDir

	entries, err := os.ReadDir(dcfhDir)
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

// initDirectoryCacheBase creates a partially-initialised DirectoryCache with
// struct fields set but no I/O performed (no directory creation, no config loading).
// Returns the DirectoryCache and the resolved dcfhDir (defaulted to rootDir if empty).
func initDirectoryCacheBase(rootDir, dcfhDir string) (*DirectoryCache, string) {
	if dcfhDir == "" {
		dcfhDir = rootDir
	}

	return &DirectoryCache{
		RootDir:       rootDir,
		DcfhDir:       filepath.Join(dcfhDir, ".dcfh"),
		IndexFile:     filepath.Join(dcfhDir, ".dcfh", "main.idx"),
		CacheFile:     filepath.Join(dcfhDir, ".dcfh", "cache.idx"),
		signature:     [4]byte{'d', 'c', 'f', 'h'},
		version:       CurrentIndexVersion,
		hasher:        sha1.New(),
		mmapIndex:     nil,
		ignoreManager: NewIgnoreManager(dcfhDir),
	}, dcfhDir
}

// configureDirectoryCache loads config and ignore patterns from an existing .dcfh directory.
// All errors are non-fatal (logged to stderr) to match existing behaviour.
func configureDirectoryCache(dc *DirectoryCache, dcfhPath string) {
	config, err := LoadConfig(dcfhPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load config from %s: %v\n", dcfhPath, err)
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
func CreateDirectoryCache(rootDir, dcfhDir string) *DirectoryCache {
	dc, dcfhDir := initDirectoryCacheBase(rootDir, dcfhDir)

	// Prevent creating .dcfh inside .dcfh (nested repositories)
	if filepath.Base(dcfhDir) == ".dcfh" {
		fmt.Fprintf(os.Stderr, "Error: Cannot create .dcfh repository inside another .dcfh directory: %s\n", dcfhDir)
		return dc
	}

	// Check if we're trying to create .dcfh inside any .dcfh subdirectory
	dir := dcfhDir
	for {
		if filepath.Base(dir) == ".dcfh" {
			fmt.Fprintf(os.Stderr, "Error: Cannot create .dcfh repository inside .dcfh directory tree: %s\n", dcfhDir)
			return dc
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Create the .dcfh directory
	dcfhPath := filepath.Join(dcfhDir, ".dcfh")
	if err := os.MkdirAll(dcfhPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to create .dcfh directory %s: %v\n", dcfhPath, err)
		return dc
	}

	configureDirectoryCache(dc, dcfhPath)

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
func OpenDirectoryCache(rootDir, dcfhDir string) (*DirectoryCache, error) {
	dc, dcfhDir := initDirectoryCacheBase(rootDir, dcfhDir)

	dcfhPath := filepath.Join(dcfhDir, ".dcfh")
	info, err := os.Stat(dcfhPath)
	if err != nil {
		return nil, fmt.Errorf("repository not found: %s does not exist", dcfhPath)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository not found: %s is not a directory", dcfhPath)
	}

	configureDirectoryCache(dc, dcfhPath)

	return dc, nil
}

// NewDirectoryCache creates a new directory cache instance.
//
// Deprecated: Use CreateDirectoryCache for new repositories or OpenDirectoryCache for existing ones.
func NewDirectoryCache(rootDir, dcfhDir string) *DirectoryCache {
	return CreateDirectoryCache(rootDir, dcfhDir)
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

// repoDir returns the repository root directory by searching upward for .dcfh
func repoDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// First check if current directory IS a .dcfh directory
	if filepath.Base(cwd) == ".dcfh" {
		// We're inside a .dcfh directory, return its parent as repo root
		repoRoot := filepath.Dir(cwd)
		realDir, err := filepath.EvalSymlinks(repoRoot)
		if err != nil {
			// If symlink resolution fails, fall back to original path
			realDir = repoRoot
		}
		return realDir, nil
	}

	dir := cwd
	for {
		dcfhPath := filepath.Join(dir, ".dcfh")
		if info, err := os.Stat(dcfhPath); err == nil && info.IsDir() {
			// Resolve symlinks to get the real path
			realDir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				// If symlink resolution fails, fall back to original path
				realDir = dir
			}
			return realDir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("not a dcfh repository (or any of the parent directories): .dcfh directory not found")
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
