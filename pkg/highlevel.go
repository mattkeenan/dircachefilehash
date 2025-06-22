package dircachefilehash

import (
	"fmt"
	"os"
)

// LoadMainIndex loads the main index file into a skiplist with "main" context
func (dc *DirectoryCache) LoadMainIndex() (*SkiplistWrapper, error) {
	if _, err := os.Stat(dc.IndexFile); os.IsNotExist(err) {
		// Create empty main index if it doesn't exist
		if err := dc.createEmptyIndex(); err != nil {
			return nil, fmt.Errorf("failed to create empty main index: %w", err)
		}
	}

	// Load entries from file using pure file I/O
	entries, err := dc.LoadIndexFromFile(dc.IndexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// Create skiplist and insert all entries with main context
	skiplist := NewSkiplistWrapper(16, MainContext)
	for _, entry := range entries {
		skiplist.Insert(entry, MainContext)
	}

	return skiplist, nil
}

// LoadCacheIndex loads the cache index file into a skiplist with "cache" context
func (dc *DirectoryCache) LoadCacheIndex() (*SkiplistWrapper, error) {
	if _, err := os.Stat(dc.CacheFile); os.IsNotExist(err) {
		return NewSkiplistWrapper(16, CacheContext), nil
	}

	// Load entries from file using pure file I/O
	entries, err := dc.LoadIndexFromFile(dc.CacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Create skiplist and insert all entries with cache context
	skiplist := NewSkiplistWrapper(16, CacheContext)
	for _, entry := range entries {
		skiplist.Insert(entry, CacheContext)
	}

	return skiplist, nil
}

// LoadIndex ensures the main index is loaded (moved from util.go)
func (dc *DirectoryCache) LoadIndex() (*SkiplistWrapper, error) {
	return dc.LoadMainIndex()
}

// CreateTmpIndexFromScan scans the directory and creates a temporary index using Hwang-Lin algorithm
// to efficiently compare against the provided comparison skiplist
func (dc *DirectoryCache) CreateTmpIndexFromScan(comparisonSkiplist *SkiplistWrapper) (*SkiplistWrapper, error) {
	// Collect all file jobs for scanning
	allJobs, err := dc.collectFileJobs([]string{dc.RootDir})
	if err != nil {
		return nil, fmt.Errorf("failed to collect file jobs: %w", err)
	}

	if len(allJobs) == 0 {
		return NewSkiplistWrapper(16, ScanContext), nil
	}

	// Use Hwang-Lin algorithm to filter jobs - only process changed/new files
	var jobsToProcess []fileJob
	sortJobsByPath(allJobs)

	// Use direct iteration to compare with existing entries
	comparisonCurrent := comparisonSkiplist.skiplist.First()
	jobIndex := 0

	for jobIndex < len(allJobs) && comparisonCurrent != nil {
		job := allJobs[jobIndex]
		existing := comparisonCurrent.Item()

		cmp := compareStrings(job.relPath, existing.RelativePath())

		if cmp == 0 {
			// Same file - check if changed
			if dc.fileChangedFromJob(existing, job) {
				// File changed - need to process it
				jobsToProcess = append(jobsToProcess, job)
			}
			// If unchanged, we'll reuse the existing entry later
			jobIndex++
			comparisonCurrent = comparisonCurrent.Next()
		} else if cmp < 0 {
			// New file not in existing set - need to process
			jobsToProcess = append(jobsToProcess, job)
			jobIndex++
		} else {
			// Existing file not in current scan - skip (effectively deleted)
			comparisonCurrent = comparisonCurrent.Next()
		}
	}

	// Handle remaining new files
	for jobIndex < len(allJobs) {
		jobsToProcess = append(jobsToProcess, allJobs[jobIndex])
		jobIndex++
	}

	// Create temporary index file for new/changed entries
	tempScanPath := dc.generateTempFileName("scan")
	defer os.Remove(tempScanPath)

	// Create temporary cache to write the jobs using existing WriteIndex functionality
	tempCache := &DirectoryCache{
		RootDir:       dc.RootDir,
		IndexFile:     tempScanPath,
		CacheFile:     tempScanPath,
		signature:     dc.signature,
		version:       dc.version,
		hasher:        dc.hasher,
		ignoreManager: dc.ignoreManager,
	}

	// Write new/changed entries using pure file I/O
	if len(jobsToProcess) > 0 {
		if err := tempCache.WriteIndex(jobsToProcess); err != nil {
			return nil, fmt.Errorf("failed to write temp scan index: %w", err)
		}
	} else {
		// Create empty index using existing functionality
		if err := tempCache.createEmptyIndex(); err != nil {
			return nil, fmt.Errorf("failed to create empty scan index: %w", err)
		}
	}

	// Load the temporary index and create skiplist
	processedEntries, err := tempCache.LoadIndexFromFile(tempScanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load temp scan index: %w", err)
	}

	// Create final result by merging processed entries with unchanged entries
	result := NewSkiplistWrapper(16, ScanContext)

	// Add all processed entries
	for _, entry := range processedEntries {
		result.Insert(entry, ScanContext)
	}

	// Add unchanged entries from comparison skiplist
	comparisonCurrent = comparisonSkiplist.skiplist.First()
	for comparisonCurrent != nil {
		existing := comparisonCurrent.Item()

		// Check if this entry was processed (and thus replaced)
		wasProcessed := false
		for _, job := range jobsToProcess {
			if job.relPath == existing.RelativePath() {
				wasProcessed = true
				break
			}
		}

		// If not processed, reuse the existing entry (zero-copy)
		if !wasProcessed {
			result.Insert(existing, ScanContext)
		}

		comparisonCurrent = comparisonCurrent.Next()
	}

	return result, nil
}

// WriteSkiplistToTmpIndex writes a skiplist to a temporary index file
func (dc *DirectoryCache) WriteSkiplistToTmpIndex(skiplist *SkiplistWrapper, tempPath string, defaultContext string) error {
	// Collect all entries from the skiplist
	var entries []*binaryEntry
	skiplist.ForEach(func(entry *binaryEntry, context string) bool {
		entries = append(entries, entry)
		return true
	})

	// Use existing WriteEntries functionality (pure file I/O)
	oldIndexFile := dc.IndexFile
	dc.IndexFile = tempPath
	defer func() { dc.IndexFile = oldIndexFile }()

	return dc.WriteEntries(entries, 0) // No special flags
}

// UpdateCacheIndexWithWorkflow implements the cache update workflow as specified
func (dc *DirectoryCache) UpdateCacheIndexWithWorkflow() error {
	// Step 1: Load main index
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return fmt.Errorf("failed to load main index: %w", err)
	}

	// Step 2: Load current cache index
	cacheSkiplist, err := dc.LoadCacheIndex()
	if err != nil {
		return fmt.Errorf("failed to load cache index: %w", err)
	}

	// Step 3: Make a copy of the main index skiplist
	workingSkiplist := mainSkiplist.Copy()

	// Step 4: Merge the cache index skiplist
	if err := workingSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
		return fmt.Errorf("failed to merge cache with main index: %w", err)
	}

	// Step 5: Create tmp index from scan using Hwang-Lin algorithm
	scanSkiplist, err := dc.CreateTmpIndexFromScan(workingSkiplist)
	if err != nil {
		return fmt.Errorf("failed to create scan index: %w", err)
	}

	// Steps 6-8 are handled inside CreateTmpIndexFromScan (Hwang-Lin, hashing, waiting)

	// Step 9: Filter cache entries (entries not in main context)
	cacheOnlySkiplist := scanSkiplist.FilterNotByContext(MainContext)

	// If no cache entries, remove cache file
	if cacheOnlySkiplist.IsEmpty() {
		os.Remove(dc.CacheFile)
		return nil
	}

	// Step 10 & 11: Write cache index using pure file I/O
	// Collect entries from cache-only skiplist
	var cacheEntries []*binaryEntry
	cacheOnlySkiplist.ForEach(func(entry *binaryEntry, context string) bool {
		cacheEntries = append(cacheEntries, entry)
		return true
	})

	// Create temporary cache file
	tempCachePath := dc.generateTempFileName("cache")

	// Write cache using existing WriteSparseEntries functionality
	if err := dc.WriteSparseEntries(cacheEntries, tempCachePath); err != nil {
		os.Remove(tempCachePath)
		return fmt.Errorf("failed to write cache index: %w", err)
	}

	// Atomic replace cache file
	if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
		os.Remove(tempCachePath) // Cleanup on failure
		return fmt.Errorf("failed to rename cache file: %w", err)
	}

	return nil
}
