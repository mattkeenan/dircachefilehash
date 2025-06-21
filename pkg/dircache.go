package dircachefilehash

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// Import merge strategies from zerocopyskiplist
const (
	MergeTheirs = zcsl.MergeTheirs
	MergeOurs   = zcsl.MergeOurs
	MergeError  = zcsl.MergeError
)

// CleanupTempFiles removes temporary files from the .dcfh directory
func (dc *DirectoryCache) CleanupTempFiles() error {
	dcfhDir := filepath.Dir(dc.IndexFile)

	entries, err := os.ReadDir(dcfhDir)
	if err != nil {
		return fmt.Errorf("failed to read .dcfh directory: %w", err)
	}

	var errors []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			fullPath := filepath.Join(dcfhDir, entry.Name())
			if err := os.Remove(fullPath); err != nil {
				errors = append(errors, fmt.Sprintf("failed to remove %s: %v", fullPath, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

// CleanupTempFilesOnExit ensures cleanup happens when the program exits
func (dc *DirectoryCache) CleanupTempFilesOnExit() {
	// This is called from defer statements to ensure cleanup
	dc.CleanupTempFiles() // Ignore errors during cleanup
}

// NewDirectoryCache creates a new directory cache instance
// rootDir: the directory to be indexed
// dcfhDir: the directory containing the .dcfh repository (if empty, uses rootDir)
// Automatically creates the .dcfh directory and empty index file if they don't exist
func NewDirectoryCache(rootDir, dcfhDir string) *DirectoryCache {
	// If dcfhDir is empty, use rootDir as the repository location
	if dcfhDir == "" {
		dcfhDir = rootDir
	}

	// The index file is always at dcfhDir/.dcfh/index
	indexFile := filepath.Join(dcfhDir, ".dcfh", "main.idx")
	cacheFile := filepath.Join(dcfhDir, ".dcfh", "cache.idx")

	dc := &DirectoryCache{
		RootDir:       rootDir,
		IndexFile:     indexFile,
		CacheFile:     cacheFile,
		skiplist:      NewSkiplistWrapper(16, MainContext),
		signature:     [4]byte{'d', 'c', 'f', 'h'},
		version:       0,
		hasher:        sha1.New(),
		mmapIndex:     nil,
		ignoreManager: NewIgnoreManager(dcfhDir),
	}

	// Ensure the .dcfh directory exists
	dcfhPath := filepath.Join(dcfhDir, ".dcfh")
	if err := os.MkdirAll(dcfhPath, 0755); err != nil {
		// Non-fatal error - log but continue
		fmt.Fprintf(os.Stderr, "Warning: Failed to create .dcfh directory %s: %v\n", dcfhPath, err)
		return dc
	}

	// Check if index file exists, create empty one if not
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
