package dircachefilehash

import (
	"crypto/sha1"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ScanIndexInfo tracks memory-mapped scan index files for cleanup
type ScanIndexInfo struct {
	FilePath string // Path to scan index file
	MmapData []byte // Memory-mapped data (if currently mapped)
	FileSize int    // Size of the file
}

// DirectoryCache manages the .dcfh metadata directory: the maps
// (main.idx, cache.idx, snapshots), the ignore manager, and the loaded
// index memo. Instrument fields (walker, file hasher, symlink mode,
// hash workers, scan-ignore predicate, scan synchronisation state) live
// on the Repo impl, not here — DirectoryCache is the *folder of maps*
// in the system metaphor, not the actor that reads the territory.
type DirectoryCache struct {
	RootDir         string
	MetaDir         string // Path to .dcfh metadata directory
	IndexFile       string
	CacheFile       string         // Path to index.cache file
	signature       [4]byte        // "dcfh" signature
	version         uint32         // Index version
	hasher          hash.Hash      // SHA-1 hasher for index-file checksums (MetaDir-side)
	mmapIndex       *mmapIndex     // Memory-mapped index file
	ignoreManager   *IgnoreManager // Ignore pattern manager
	config          *Config        // Configuration manager
	ignoreIsDeindex bool           // Whether newly ignored files should be marked as deleted

	// Index tracking for memory protection during hash calculations
	mainIndex        *mmapIndexFile // Main index file (if loaded)
	cacheIndex       *mmapIndexFile // Cache index file (if loaded)
	indexLockTimeout int            // Timeout in seconds for index memory locks

	// Read-only mmap memo: dedups loadIndexFromFileWithTracking calls for
	// canonical paths (main.idx, cache.idx, timestamped cache files,
	// snapshot main.idx). Keyed by absolute path. Stat-checked on every
	// lookup so atomic-rename writes invalidate naturally.
	//
	// Lifetime: the memo owns the mappings. Each entry is constructed with
	// refCount=1; eviction or Close DecRefs to 0 → cleanup. Stat-mismatch
	// evictions move the old entry into orphanIndices instead of unmapping
	// immediately — skiplists handed out earlier may still hold refs into
	// that mapping. orphanIndices is drained in Close.
	loadedIndices map[string]*loadedIndex
	orphanIndices []*loadedIndex
	loadedMu      sync.Mutex
}

// loadedIndex is one entry in the read-only mmap memo. The mmap is owned
// by the memo: holding a *loadedIndex implies a live mapping. Eviction
// (stale stat or Close) DecRefs file. The refs slice is the parsed entry
// list — sharing it across consumers is safe because main/cache/snapshot
// indices are PROT_READ and never mutated in memory.
type loadedIndex struct {
	file *mmapIndexFile
	refs []binaryEntryRef
	stat cachedStat
}

// cachedStat captures the identity of an on-disk index file for the memo's
// invalidation check. Comparing dev+inode+size+mtime catches atomic
// renames (inode changes), in-place truncation (size changes), and any
// other rewrite (mtime changes).
type cachedStat struct {
	dev   uint64
	ino   uint64
	size  int64
	mtime int64 // unix nanoseconds
}

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
		dc.indexLockTimeout = performanceConfig.IndexLockTimeout
	} else {
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

	// Create default config if it doesn't exist
	if _, err := LoadConfig(dc.MetaDir); err != nil {
		if _, err := CreateDefaultConfig(dc.MetaDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to create default config: %v\n", err)
		}
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

// ResolvedOverrides bundles the post-flag instrument settings produced
// by ApplyConfigOverrides. The caller (Repo impl or test) writes these
// onto its own ScanRun-bearing storage; DirectoryCache no longer holds
// instrument state.
type ResolvedOverrides struct {
	SymlinkMode string
	HashWorkers int
}

// ApplyConfigOverrides applies configuration overrides from the flags
// map. MetaDir-side state (Config, ignoreIsDeindex, indexLockTimeout)
// is mutated on dc as before. Instrument-side state (symlink mode,
// hash workers) is returned in ResolvedOverrides — the caller decides
// where to store it.
//
// Resolved values are populated even when err != nil so callers
// honour --symlinks / --hash-workers when no config is loaded.
func (dc *DirectoryCache) ApplyConfigOverrides(flags map[string]string) (ResolvedOverrides, error) {
	out := ResolvedOverrides{}

	// Symlink mode: flags > config > default. Resolved regardless of
	// whether config is present, so the caller can apply it even if
	// the config-loading path fails below.
	if symlinkMode, exists := flags["symlinks"]; exists {
		out.SymlinkMode = symlinkMode
	} else if dc.config != nil {
		out.SymlinkMode = dc.config.GetSymlinkConfig().Mode
	} else {
		out.SymlinkMode = "none"
	}

	// Hash workers: flags > config > default.
	hashWorkersFromFlags := 0
	if hashWorkersStr, exists := flags["hash_workers"]; exists {
		n, err := strconv.Atoi(hashWorkersStr)
		if err != nil {
			return out, fmt.Errorf("invalid hash workers value '%s': %w", hashWorkersStr, err)
		}
		if err := ValidateHashWorkers(n); err != nil {
			return out, fmt.Errorf("invalid hash workers configuration: %w", err)
		}
		hashWorkersFromFlags = n
		out.HashWorkers = n
	} else if dc.config != nil {
		out.HashWorkers = dc.config.GetPerformanceConfig().HashWorkers
	} else {
		out.HashWorkers = 2
	}

	if dc.config == nil {
		return out, fmt.Errorf("no configuration loaded, cannot apply overrides")
	}

	var allOverrides []string

	if filehashOverride, exists := flags["filehash"]; exists {
		allOverrides = append(allOverrides, filehashOverride)
	}

	// Ignore-deindex behaviour stays on dc (MetaDir-side).
	dc.ignoreIsDeindex = dc.config.GetIgnoreConfig().IgnoreIsDeindex

	if hashWorkersFromFlags > 0 {
		allOverrides = append(allOverrides, "hash_workers:"+strconv.Itoa(hashWorkersFromFlags))
	}

	if indexLockTimeoutStr, exists := flags["index_lock_timeout"]; exists {
		indexLockTimeout, err := strconv.Atoi(indexLockTimeoutStr)
		if err != nil {
			return out, fmt.Errorf("invalid index lock timeout value '%s': %w", indexLockTimeoutStr, err)
		}
		if err := ValidateIndexLockTimeout(indexLockTimeout); err != nil {
			return out, fmt.Errorf("invalid index lock timeout configuration: %w", err)
		}
		dc.indexLockTimeout = indexLockTimeout
		allOverrides = append(allOverrides, "index_lock_timeout:"+indexLockTimeoutStr)
	}

	if len(allOverrides) > 0 {
		if err := dc.config.ApplyOverrides(allOverrides); err != nil {
			return out, fmt.Errorf("failed to apply configuration overrides: %w", err)
		}
		if err := dc.validateAllConfigs(); err != nil {
			return out, fmt.Errorf("invalid configuration after overrides: %w", err)
		}
	}

	return out, nil
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

// ResolveRepository resolves rootDir for a known metaDir (typically from
// --meta-dir or because cwd is a *.dcfh directory). For internal .dcfh
// directories, rootDir is the parent. For external *.dcfh directories, reads
// [repository] root from the config, falling back to the parent directory.
// Returns (rootDir, metaDir, error).
func ResolveRepository(metaDir string) (string, string, error) {
	base := filepath.Base(metaDir)
	if base == ".dcfh" {
		repoRoot := filepath.Dir(metaDir)
		realDir, err := filepath.EvalSymlinks(repoRoot)
		if err != nil {
			realDir = repoRoot
		}
		return realDir, metaDir, nil
	}
	if strings.HasSuffix(base, ".dcfh") {
		if root, ok := ResolveExternalRoot(metaDir); ok {
			// Remote URIs are returned as-is; callers that only want a
			// local rootDir must check for a scheme themselves (the
			// Repo factory swaps the wire walker/hasher pair in).
			return root, metaDir, nil
		}
		return filepath.Dir(metaDir), metaDir, nil
	}
	return "", "", fmt.Errorf("not a .dcfh directory: %s", metaDir)
}

// DiscoverRepository walks up from startDir (or cwd if empty) looking for a
// .dcfh subdirectory. Local filesystem only. If startDir itself IS a *.dcfh
// directory, delegates to ResolveRepository. Returns (rootDir, metaDir, error).
func DiscoverRepository(startDir string) (string, string, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	if strings.HasSuffix(filepath.Base(startDir), ".dcfh") {
		return ResolveRepository(startDir)
	}

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
	}
}
