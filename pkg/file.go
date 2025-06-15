package dircachefilehash

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"syscall"
)

// processFileJob processes a single file job and returns hash, hash type, and file info
func (dc *DirectoryCache) processFileJob(job fileJob) ([]byte, uint16, *syscall.Stat_t, error) {
	// Hash the file contents (currently defaults to SHA1)
	hashStr, err := dc.hashFile(job.path)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to hash file %s: %w", job.path, err)
	}

	// Convert hash string to bytes
	hashBytes, err := hex.DecodeString(hashStr)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("invalid hash %s: %w", hashStr, err)
	}

	// Currently using SHA1 - TODO: make configurable
	hashType := HashTypeSHA1

	// Get system-specific file information
	stat := job.info.Sys().(*syscall.Stat_t)

	return hashBytes, hashType, stat, nil
}

// hashFile calculates SHA-1 hash of a file's contents
func (dc *DirectoryCache) hashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to hash file %s: %w", filePath, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
