package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

const (
	MainContext  = "main"
	CacheContext = "cache"
	ScanContext  = "scan"
	TempContext  = "temp"
)

// LoadMainIndex loads the main index file into a skiplist with "main" context
func (dc *DirectoryCache) LoadMainIndex() (*SkiplistWrapper, error) {
	if _, err := os.Stat(dc.IndexFile); os.IsNotExist(err) {
		// Create empty main index if it doesn't exist
		if err := dc.createEmptyIndex(); err != nil {
			return nil, fmt.Errorf("failed to create empty main index: %w", err)
		}
	}

	skiplist := NewSkiplistWrapper(16, MainContext)

	// Save current skiplist
	oldSkiplist := dc.skiplist
	dc.skiplist = skiplist

	// Load the index using existing functionality
	if err := dc.loadIndexFromFile(dc.IndexFile, MainContext); err != nil {
		dc.skiplist = oldSkiplist // Restore on error
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// Set all entries to main context
	tempSkiplist := NewSkiplistWrapper(16, MainContext)
	dc.skiplist.ForEach(func(entry *binaryEntry, context string) bool {
		tempSkiplist.Insert(entry, MainContext)
		return true
	})

	// Restore original skiplist and return the loaded one
	dc.skiplist = oldSkiplist
	return tempSkiplist, nil
}

// LoadCacheIndex loads the cache index file into a skiplist with "cache" context
func (dc *DirectoryCache) LoadCacheIndex() (*SkiplistWrapper, error) {
	if _, err := os.Stat(dc.CacheFile); os.IsNotExist(err) {
		return NewSkiplistWrapper(16, CacheContext), nil
	}

	// Create temporary cache instance to load from cache file
	tempCache := &DirectoryCache{
		RootDir:       dc.RootDir,
		IndexFile:     dc.CacheFile, // Point to cache file
		CacheFile:     dc.CacheFile,
		skiplist:      NewSkiplistWrapper(16, CacheContext),
		signature:     dc.signature,
		version:       dc.version,
		hasher:        dc.hasher,
		ignoreManager: dc.ignoreManager,
	}

	// Use existing loadIndexFromFile functionality
	if err := tempCache.loadIndexFromFile(dc.CacheFile, CacheContext); err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Set all entries to cache context
	result := NewSkiplistWrapper(16, CacheContext)
	tempCache.skiplist.ForEach(func(entry *binaryEntry, context string) bool {
		result.Insert(entry, CacheContext)
		return true
	})

	return result, nil
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
		skiplist:      NewSkiplistWrapper(16, ScanContext),
		signature:     dc.signature,
		version:       dc.version,
		hasher:        dc.hasher,
		ignoreManager: dc.ignoreManager,
	}

	// Write new/changed entries using existing WriteIndex functionality
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

	// Load the temporary index using existing functionality
	processedSkiplist, err := tempCache.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load temp scan index: %w", err)
	}

	// Create final result by merging processed entries with unchanged entries
	result := NewSkiplistWrapper(16, ScanContext)

	// Add all processed entries
	processedSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
		result.Insert(entry, ScanContext)
		return true
	})

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

// WriteSkiplistToFile writes a skiplist to a file using existing index writing functionality
func (dc *DirectoryCache) WriteSkiplistToFile(skiplist *SkiplistWrapper, filePath string, flags uint32) error {
	// Convert skiplist entries to fileJobs for existing WriteIndex functionality
	var jobs []fileJob
	skiplist.ForEach(func(entry *binaryEntry, context string) bool {
		// Create a dummy fileJob from the existing entry
		// This is a bit of a hack, but allows us to reuse existing functionality
		job := fileJob{
			path:    filepath.Join(dc.RootDir, entry.RelativePath()),
			relPath: entry.RelativePath(),
			index:   len(jobs),
			// Note: info field will need special handling since we don't have os.FileInfo
		}
		jobs = append(jobs, job)
		return true
	})

	if len(jobs) == 0 {
		// Create empty index using existing functionality
		oldIndexFile := dc.IndexFile
		dc.IndexFile = filePath
		err := dc.createEmptyIndex()
		dc.IndexFile = oldIndexFile
		return err
	}

	// Use existing writeIndexWithFlags functionality
	oldIndexFile := dc.IndexFile
	dc.IndexFile = filePath
	defer func() { dc.IndexFile = oldIndexFile }()

	// For this to work properly, we need to modify the approach
	// Instead of trying to fake fileJobs, we should write entries directly
	return fmt.Errorf("WriteSkiplistToFile needs refactoring to work with existing index.go functions")
}

// WriteSkiplistToTmpIndex writes a skiplist to a temporary index file
func (dc *DirectoryCache) WriteSkiplistToTmpIndex(skiplist *SkiplistWrapper, tempPath string, defaultContext string) error {
	// Collect all entries from the skiplist
	var entries []*binaryEntry
	skiplist.ForEach(func(entry *binaryEntry, context string) bool {
		entries = append(entries, entry)
		return true
	})

	// Use existing WriteEntries functionality
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
	if err := workingSkiplist.Merge(cacheSkiplist, zcsl.MergeTheirs); err != nil {
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

	// Step 10 & 11: Write cache index using existing functionality
	// Create file jobs from cache entries
	var cacheJobs []fileJob
	cacheOnlySkiplist.ForEach(func(entry *binaryEntry, context string) bool {
		// We need to reconstruct file info from the entry
		// This is challenging since we need os.FileInfo but only have entry data
		job := fileJob{
			path:    filepath.Join(dc.RootDir, entry.RelativePath()),
			relPath: entry.RelativePath(),
			index:   len(cacheJobs),
			// info: needs to be reconstructed from entry data
		}
		cacheJobs = append(cacheJobs, job)
		return true
	})

	// Create temporary cache file
	tempCachePath := dc.generateTempFileName("cache")

	// Write cache using existing WriteSparseIndex functionality
	oldIndexFile := dc.IndexFile
	dc.IndexFile = tempCachePath

	// This approach has a fundamental problem - we need to refactor
	// the existing index writing to work with entries, not just fileJobs
	dc.IndexFile = oldIndexFile

	// For now, remove cache file to avoid corruption
	os.Remove(dc.CacheFile)

	return fmt.Errorf("cache writing needs refactoring to work with existing index.go functions")
}
