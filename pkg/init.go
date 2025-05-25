// Package dircachefilehash provides functionality to scan directories,
// hash file contents, and maintain a sorted index file for file integrity
// checking and change detection.
package dircachefilehash

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
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
		entries:   make([]FileEntry, 0),
		signature: [4]byte{'d', 'c', 'f', 'h'}, // "dcfh" signature
		version:   1,                           // Version 1 format
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

// createEmptyIndex creates an empty index file with proper header
func (dc *DirectoryCache) createEmptyIndex() error {
	file, err := os.Create(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to create index file %s: %w", dc.IndexFile, err)
	}
	defer file.Close()

	// Write header with zero entries
	if err := binary.Write(file, binary.BigEndian, dc.signature); err != nil {
		return fmt.Errorf("failed to write signature: %w", err)
	}
	if err := binary.Write(file, binary.BigEndian, dc.version); err != nil {
		return fmt.Errorf("failed to write version: %w", err)
	}
	if err := binary.Write(file, binary.BigEndian, uint32(0)); err != nil {
		return fmt.Errorf("failed to write entry count: %w", err)
	}

	// Write empty checksum (SHA-1 of header only)
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}

	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}

	// Seek to end to append checksum
	if _, err := file.Seek(0, 2); err != nil {
		return err
	}

	checksum := hasher.Sum(nil)
	if _, err := file.Write(checksum); err != nil {
		return err
	}

	return nil
}
