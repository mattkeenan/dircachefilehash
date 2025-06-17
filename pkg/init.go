// Package dircachefilehash provides functionality to scan directories,
// hash file contents, and maintain a sorted index file for file integrity
// checking and change detection using memory-mapped files for zero-copy operation.
package dircachefilehash

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
)

// NewDirectoryCache creates a new directory cache instance with context tracking support
// rootDir: the directory to be indexed
// dcfhDir: the directory containing the .dcfh repository (if empty, uses rootDir)
// Automatically creates the .dcfh directory and empty index file if they don't exist
// NOTE: index.cache is only created when needed by status/dupes/update operations
func NewDirectoryCache(rootDir, dcfhDir string) *DirectoryCache {
	// If dcfhDir is empty, use rootDir as the repository location
	if dcfhDir == "" {
		dcfhDir = rootDir
	}

	// The index file is always at dcfhDir/.dcfh/index
	indexFile := filepath.Join(dcfhDir, ".dcfh", "index")
	cacheFile := filepath.Join(dcfhDir, ".dcfh", "index.cache")

	dc := &DirectoryCache{
		RootDir:       rootDir,
		IndexFile:     indexFile,
		CacheFile:     cacheFile,
		skiplist:      NewSkiplistWrapper(16, "main"), // Initialize with main context
		signature:     [4]byte{'d', 'c', 'f', 'h'},    // "dcfh" signature
		version:       0,                              // Version 0 format (pre-v1 release)
		hasher:        sha1.New(),                     // SHA-1 hasher for checksums
		mmapIndex:     nil,                            // Will be set when loading mmap'd index
		ignoreManager: NewIgnoreManager(dcfhDir),      // Initialize ignore manager
	}

	// Ensure the .dcfh directory exists
	dcfhPath := filepath.Join(dcfhDir, ".dcfh")
	if err := os.MkdirAll(dcfhPath, 0755); err != nil {
		// Non-fatal error - log but continue
		fmt.Fprintf(os.Stderr, "Warning: Failed to create .dcfh directory %s: %v\n", dcfhPath, err)
		return dc
	}

	// Check if index file exists, create empty one if not (but no cache file yet)
	if _, err := os.Stat(indexFile); os.IsNotExist(err) {
		// Create empty main index file only
		if err := dc.createEmptyIndex(); err != nil {
			// Non-fatal error - log but continue
			fmt.Fprintf(os.Stderr, "Warning: Failed to create empty index file %s: %v\n", indexFile, err)
		}
	}

	// Initialize ignore patterns
	if err := dc.ignoreManager.LoadIgnorePatterns(); err != nil {
		// Non-fatal error - log but continue
		fmt.Fprintf(os.Stderr, "Warning: Failed to load ignore patterns: %v\n", err)
	}

	return dc
}
