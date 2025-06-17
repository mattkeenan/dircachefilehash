package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CacheManagement provides cache management utilities for DirectoryCache with context tracking

// GetIgnoreManager returns the ignore manager for this cache
func (dc *DirectoryCache) GetIgnoreManager() *IgnoreManager {
	return dc.ignoreManager
}

// AddIgnorePattern adds a new ignore pattern
func (dc *DirectoryCache) AddIgnorePattern(pattern string) error {
	return dc.ignoreManager.AddPattern(pattern)
}

// SaveIgnorePatterns saves current ignore patterns to file
func (dc *DirectoryCache) SaveIgnorePatterns() error {
	return dc.ignoreManager.SaveIgnorePatterns()
}

// ReloadIgnorePatterns forces a reload of ignore patterns
func (dc *DirectoryCache) ReloadIgnorePatterns() error {
	return dc.ignoreManager.Reload()
}

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

// CleanupOldTempFiles removes temporary files older than the specified duration
func (dc *DirectoryCache) CleanupOldTempFiles(maxAge time.Duration) error {
	dcfhDir := filepath.Dir(dc.IndexFile)

	entries, err := os.ReadDir(dcfhDir)
	if err != nil {
		return fmt.Errorf("failed to read .dcfh directory: %w", err)
	}

	cutoff := time.Now().Add(-maxAge)
	var errors []string

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			fullPath := filepath.Join(dcfhDir, entry.Name())

			info, err := entry.Info()
			if err != nil {
				continue
			}

			if info.ModTime().Before(cutoff) {
				if err := os.Remove(fullPath); err != nil {
					errors = append(errors, fmt.Sprintf("failed to remove %s: %v", fullPath, err))
				}
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

// GetCacheStats returns statistics about the cache with context tracking awareness
func (dc *DirectoryCache) GetCacheStats() (CacheStats, error) {
	stats := CacheStats{}

	// Get main index stats
	if dc.skiplist != nil {
		stats.MainIndexEntries, stats.MainIndexDeleted, stats.MainIndexActive = dc.skiplist.Stats()
	}

	// Check cache index
	if _, err := os.Stat(dc.CacheFile); err == nil {
		cacheSkiplist, err := dc.LoadCacheIndex("cache")
		if err != nil {
			return stats, fmt.Errorf("failed to load cache index: %w", err)
		}
		stats.CacheIndexEntries, stats.CacheIndexDeleted, stats.CacheIndexActive = cacheSkiplist.Stats()
		stats.HasCacheIndex = true
	}

	// Get file sizes
	if info, err := os.Stat(dc.IndexFile); err == nil {
		stats.MainIndexSize = info.Size()
	}

	if info, err := os.Stat(dc.CacheFile); err == nil {
		stats.CacheIndexSize = info.Size()
	}

	// Count temp files
	dcfhDir := filepath.Dir(dc.IndexFile)
	if entries, err := os.ReadDir(dcfhDir); err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".tmp") {
				stats.TempFileCount++
				if info, err := entry.Info(); err == nil {
					stats.TempFileSize += info.Size()
				}
			}
		}
	}

	return stats, nil
}

// CacheStats represents cache statistics
type CacheStats struct {
	MainIndexEntries int   `json:"main_index_entries"`
	MainIndexDeleted int   `json:"main_index_deleted"`
	MainIndexActive  int   `json:"main_index_active"`
	MainIndexSize    int64 `json:"main_index_size"`

	HasCacheIndex     bool  `json:"has_cache_index"`
	CacheIndexEntries int   `json:"cache_index_entries"`
	CacheIndexDeleted int   `json:"cache_index_deleted"`
	CacheIndexActive  int   `json:"cache_index_active"`
	CacheIndexSize    int64 `json:"cache_index_size"`

	TempFileCount int   `json:"temp_file_count"`
	TempFileSize  int64 `json:"temp_file_size"`
}

// ValidateIndex performs validation checks on the index with context tracking support
func (dc *DirectoryCache) ValidateIndex() error {
	// Check if main index file exists and is readable
	if _, err := os.Stat(dc.IndexFile); err != nil {
		return fmt.Errorf("main index file not accessible: %w", err)
	}

	// Try to load the index with context
	testCache := NewDirectoryCache(dc.RootDir, filepath.Dir(dc.IndexFile))
	if err := testCache.LoadIndex(testCache.IndexFile, "test"); err != nil {
		return fmt.Errorf("index file corrupted: %w", err)
	}
	defer testCache.Close()

	// Check for consistency
	entries := testCache.GetEntries()
	if len(entries) == 0 {
		return nil // Empty index is valid
	}

	// Check if entries are sorted
	for i := 1; i < len(entries); i++ {
		if entries[i-1].RelativePath() >= entries[i].RelativePath() {
			return fmt.Errorf("index entries not properly sorted at position %d", i)
		}
	}

	return nil
}

// RepairIndex attempts to repair a corrupted index with context tracking
func (dc *DirectoryCache) RepairIndex() error {
	// Backup existing index
	backupPath := dc.IndexFile + ".backup." + fmt.Sprintf("%d", time.Now().Unix())
	if err := os.Rename(dc.IndexFile, backupPath); err != nil {
		return fmt.Errorf("failed to backup corrupted index: %w", err)
	}

	// Remove cache index as it may also be corrupted
	os.Remove(dc.CacheFile)

	// Create new empty index
	if err := dc.createEmptyIndex(); err != nil {
		// Try to restore backup
		os.Rename(backupPath, dc.IndexFile)
		return fmt.Errorf("failed to create new index: %w", err)
	}

	// Perform full update to rebuild index
	if err := dc.Update(); err != nil {
		// Try to restore backup
		os.Remove(dc.IndexFile)
		os.Rename(backupPath, dc.IndexFile)
		return fmt.Errorf("failed to rebuild index: %w", err)
	}

	return nil
}

// OptimizeIndex optimizes the index by removing deleted entries and defragmenting with context preservation
func (dc *DirectoryCache) OptimizeIndex() error {
	// Load current index with context
	if err := dc.LoadIndex(dc.IndexFile, "main"); err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}

	// Filter out deleted entries while preserving context
	optimizedSkiplist := dc.skiplist.FilterDeleted()

	// Write optimized index
	tempIndexPath := dc.generateTempFileName("optimized")
	if err := dc.writeCompleteIndexFromSkiplist(optimizedSkiplist, tempIndexPath); err != nil {
		return fmt.Errorf("failed to write optimized index: %w", err)
	}

	// Replace current index
	if err := os.Rename(tempIndexPath, dc.IndexFile); err != nil {
		os.Remove(tempIndexPath) // Cleanup on failure
		return fmt.Errorf("failed to replace index with optimized version: %w", err)
	}

	// Update skiplist with proper context
	dc.skiplist = optimizedSkiplist.Copy("main")

	// Remove cache index as it's now obsolete
	os.Remove(dc.CacheFile)

	return nil
}

// GetRepositoryInfo returns information about the repository with context tracking awareness
func (dc *DirectoryCache) GetRepositoryInfo() (RepositoryInfo, error) {
	info := RepositoryInfo{
		RootDirectory: dc.RootDir,
		IndexFile:     dc.IndexFile,
		CacheFile:     dc.CacheFile,
	}

	// Get ignore patterns
	if dc.ignoreManager.IsLoaded() {
		patterns := dc.ignoreManager.GetPatterns()
		info.IgnorePatterns = make([]string, len(patterns))
		for i, pattern := range patterns {
			info.IgnorePatterns[i] = pattern.String()
		}
	}

	// Get statistics
	var err error
	info.Stats, err = dc.GetCacheStats()
	if err != nil {
		return info, fmt.Errorf("failed to get cache stats: %w", err)
	}

	return info, nil
}

// RepositoryInfo represents repository information
type RepositoryInfo struct {
	RootDirectory  string     `json:"root_directory"`
	IndexFile      string     `json:"index_file"`
	CacheFile      string     `json:"cache_file"`
	IgnorePatterns []string   `json:"ignore_patterns"`
	Stats          CacheStats `json:"stats"`
}

// IsRepositoryHealthy performs basic health checks with context tracking awareness
func (dc *DirectoryCache) IsRepositoryHealthy() (bool, []string) {
	var issues []string

	// Check if root directory exists
	if _, err := os.Stat(dc.RootDir); err != nil {
		issues = append(issues, fmt.Sprintf("Root directory not accessible: %v", err))
	}

	// Check if .dcfh directory exists
	dcfhDir := filepath.Dir(dc.IndexFile)
	if _, err := os.Stat(dcfhDir); err != nil {
		issues = append(issues, fmt.Sprintf(".dcfh directory not accessible: %v", err))
	}

	// Check index validity
	if err := dc.ValidateIndex(); err != nil {
		issues = append(issues, fmt.Sprintf("Index validation failed: %v", err))
	}

	// Check ignore patterns
	if err := dc.ignoreManager.LoadIgnorePatterns(); err != nil {
		issues = append(issues, fmt.Sprintf("Ignore patterns invalid: %v", err))
	}

	return len(issues) == 0, issues
}
