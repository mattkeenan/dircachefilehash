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
	indexFile := filepath.Join(dcfhDir, ".dcfh", "index")

	dc := &DirectoryCache{
		RootDir:   rootDir,
		IndexFile: indexFile,
		entries:   make([]*binaryEntry, 0),     // Direct pointers to mmap'd entries
		signature: [4]byte{'d', 'c', 'f', 'h'}, // "dcfh" signature
		version:   1,                           // Version 1 format
		hasher:    sha1.New(),                  // SHA-1 hasher for checksums
		mmapIndex: nil,                         // Will be set when loading mmap'd index
	}

	// Ensure the .dcfh directory exists
	dcfhPath := filepath.Join(dcfhDir, ".dcfh")
	if err := os.MkdirAll(dcfhPath, 0755); err != nil {
		// Non-fatal error - log but continue
		fmt.Fprintf(os.Stderr, "Warning: Failed to create .dcfh directory %s: %v\n", dcfhPath, err)
		return dc
	}

	// Check if index file exists
	if _, err := os.Stat(indexFile); os.IsNotExist(err) {
		// Create empty index file
		if err := dc.createEmptyIndex(); err != nil {
			// Non-fatal error - log but continue
			fmt.Fprintf(os.Stderr, "Warning: Failed to create empty index file %s: %v\n", indexFile, err)
		}
	}

	return dc
}
