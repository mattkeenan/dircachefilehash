package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Update scans the directory and updates the index file
// If paths are provided, only those files/directories are updated
// If no paths are provided, all files under rootDir are updated
func (dc *DirectoryCache) Update(paths ...string) error {
	if len(paths) == 0 {
		// Update all files (original behavior)
		if err := dc.ScanDirectory(); err != nil {
			return fmt.Errorf("failed to scan directory: %w", err)
		}
	} else {
		// Update only specified paths
		if err := dc.UpdatePaths(paths); err != nil {
			return fmt.Errorf("failed to update paths: %w", err)
		}
	}

	if err := dc.WriteIndex(); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	return nil
}

// UpdatePaths updates only the specified paths in the index
func (dc *DirectoryCache) UpdatePaths(paths []string) error {
	// Load existing index to preserve other entries
	existingEntries := make(map[string]FileEntry)
	if err := dc.LoadIndex(); err == nil {
		for _, entry := range dc.entries {
			existingEntries[entry.RelativePath] = entry
		}
	}

	// Collect files to process from specified paths
	var fileJobs []fileJob
	jobIndex := 0
	pathsToRemove := make(map[string]bool)

	for _, inputPath := range paths {
		// Convert to absolute path
		absPath := inputPath
		if !filepath.IsAbs(inputPath) {
			absPath = filepath.Join(dc.RootDir, inputPath)
		}

		// Clean the path
		absPath = filepath.Clean(absPath)

		// Check if path exists
		info, err := os.Lstat(absPath)
		if err != nil {
			// Path doesn't exist - mark for removal from index
			relPath, relErr := filepath.Rel(dc.RootDir, absPath)
			if relErr == nil {
				pathsToRemove[relPath] = true
				// Also mark any files under this path for removal
				for existingPath := range existingEntries {
					if strings.HasPrefix(existingPath, relPath+"/") || existingPath == relPath {
						pathsToRemove[existingPath] = true
					}
				}
			}
			continue
		}

		// Get relative path from root directory
		relPath, err := filepath.Rel(dc.RootDir, absPath)
		if err != nil {
			return fmt.Errorf("path %s is not under root directory %s", absPath, dc.RootDir)
		}

		// If it's a directory, scan it recursively
		if info.IsDir() {
			if err := dc.scanPathRecursively(absPath, &fileJobs, &jobIndex, pathsToRemove); err != nil {
				return fmt.Errorf("failed to scan directory %s: %w", absPath, err)
			}
		} else if info.Mode().IsRegular() {
			// Skip the index file itself
			if absPath == dc.IndexFile {
				continue
			}

			// Add single file
			fileJobs = append(fileJobs, fileJob{
				path:    absPath,
				info:    info,
				relPath: relPath,
				index:   jobIndex,
			})
			jobIndex++

			// Mark this path as being updated (remove from pathsToRemove if it was there)
			delete(pathsToRemove, relPath)
		}
	}

	// Process files in parallel
	var newEntries []FileEntry
	if len(fileJobs) > 0 {
		results, err := dc.processFilesParallel(fileJobs)
		if err != nil {
			return err
		}

		// Sort results by original index to maintain discovery order
		sort.Slice(results, func(i, j int) bool {
			return results[i].index < results[j].index
		})

		// Extract entries from results
		for _, result := range results {
			if result.err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to process file: %v\n", result.err)
				continue
			}
			newEntries = append(newEntries, *result.entry)
		}
	}

	// Merge with existing entries
	updatedPaths := make(map[string]bool)
	for _, entry := range newEntries {
		updatedPaths[entry.RelativePath] = true
	}

	// Start with new entries
	dc.entries = make([]FileEntry, 0, len(newEntries)+len(existingEntries))
	dc.entries = append(dc.entries, newEntries...)

	// Add existing entries that weren't updated or removed
	for path, entry := range existingEntries {
		if !updatedPaths[path] && !pathsToRemove[path] {
			dc.entries = append(dc.entries, entry)
		}
	}

	// Sort entries by filename for byte comparison order
	sort.Slice(dc.entries, func(i, j int) bool {
		return dc.entries[i].RelativePath < dc.entries[j].RelativePath
	})

	return nil
}

// scanPathRecursively scans a directory path recursively and adds files to the job list
func (dc *DirectoryCache) scanPathRecursively(rootPath string, fileJobs *[]fileJob, jobIndex *int, pathsToRemove map[string]bool) error {
	// FIFO slice for directory traversal
	pathQueue := []string{rootPath}

	for len(pathQueue) > 0 {
		// Pop the first entry from the FIFO slice
		currentPath := pathQueue[0]
		pathQueue = pathQueue[1:]

		// Get file info
		info, err := os.Lstat(currentPath)
		if err != nil {
			continue // Skip files we can't access
		}

		// Get relative path
		relPath, err := filepath.Rel(dc.RootDir, currentPath)
		if err != nil {
			continue
		}

		if info.IsDir() {
			// Skip the index directory
			indexDir := filepath.Dir(dc.IndexFile)
			if currentPath == indexDir {
				continue
			}

			// Remove this path from removal list since it exists
			delete(pathsToRemove, relPath)

			entries, err := os.ReadDir(currentPath)
			if err != nil {
				continue // Skip directories we can't read
			}

			// Sort entries by name using bytewise comparison
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() < entries[j].Name()
			})

			// Add all entries to the FIFO queue
			for _, entry := range entries {
				fullPath := filepath.Join(currentPath, entry.Name())
				pathQueue = append(pathQueue, fullPath)
			}
		} else if info.Mode().IsRegular() {
			// Skip the index file itself
			if currentPath == dc.IndexFile {
				continue
			}

			// Add to jobs list
			*fileJobs = append(*fileJobs, fileJob{
				path:    currentPath,
				info:    info,
				relPath: relPath,
				index:   *jobIndex,
			})
			*jobIndex++

			// Remove this path from removal list since it exists
			delete(pathsToRemove, relPath)
		}
	}

	return nil
}

// ScanDirectory scans the directory and creates file entries with hashes using parallel processing
func (dc *DirectoryCache) ScanDirectory() error {
	dc.entries = make([]FileEntry, 0)

	// Collect all regular files first
	var fileJobs []fileJob
	jobIndex := 0

	// FIFO slice for file paths - push to end, pop from front
	pathQueue := []string{dc.RootDir}

	// Process paths until queue is empty
	for len(pathQueue) > 0 {
		// Pop the first entry from the FIFO slice
		currentPath := pathQueue[0]
		pathQueue = pathQueue[1:]

		// Get file info
		info, err := os.Lstat(currentPath) // Use Lstat to handle symlinks properly
		if err != nil {
			// Skip files we can't access
			continue
		}

		// If it's a directory, read its contents and add to queue
		if info.IsDir() {
			// Skip the index directory if it's inside the scan directory
			indexDir := filepath.Dir(dc.IndexFile)
			if currentPath == indexDir {
				continue
			}

			entries, err := os.ReadDir(currentPath)
			if err != nil {
				// Skip directories we can't read
				continue
			}

			// Sort entries by name using bytewise comparison
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() < entries[j].Name()
			})

			// Add all entries to the FIFO queue
			for _, entry := range entries {
				fullPath := filepath.Join(currentPath, entry.Name())
				pathQueue = append(pathQueue, fullPath)
			}
		} else if info.Mode().IsRegular() {
			// Skip the index file itself
			if currentPath == dc.IndexFile {
				continue
			}

			// Calculate relative path from root directory
			relPath, err := filepath.Rel(dc.RootDir, currentPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get relative path for %s: %v\n", currentPath, err)
				continue
			}

			// Add to jobs list
			fileJobs = append(fileJobs, fileJob{
				path:    currentPath,
				info:    info,
				relPath: relPath,
				index:   jobIndex,
			})
			jobIndex++
		}
		// For other file types (symlinks, device files, etc.), we just skip them
		// since we only want to index regular files with content hashes
	}

	// Process files in parallel
	if len(fileJobs) > 0 {
		results, err := dc.processFilesParallel(fileJobs)
		if err != nil {
			return err
		}

		// Sort results by original index to maintain discovery order
		sort.Slice(results, func(i, j int) bool {
			return results[i].index < results[j].index
		})

		// Extract entries from results
		for _, result := range results {
			if result.err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to process file: %v\n", result.err)
				continue
			}
			dc.entries = append(dc.entries, *result.entry)
		}
	}

	// Sort entries by filename (RelativePath) for byte comparison order
	sort.Slice(dc.entries, func(i, j int) bool {
		return dc.entries[i].RelativePath < dc.entries[j].RelativePath
	})

	return nil
}

// processFilesParallel processes files using a pool of goroutines
func (dc *DirectoryCache) processFilesParallel(jobs []fileJob) ([]fileResult, error) {
	// Determine number of workers (use number of CPU cores)
	numWorkers := runtime.NumCPU()
	if numWorkers > len(jobs) {
		numWorkers = len(jobs)
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	// Create channels
	jobChan := make(chan fileJob, len(jobs))
	resultChan := make(chan fileResult, len(jobs))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go dc.fileHashWorker(jobChan, resultChan, &wg)
	}

	// Send jobs
	for _, job := range jobs {
		jobChan <- job
	}
	close(jobChan)

	// Wait for workers to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var results []fileResult
	for result := range resultChan {
		results = append(results, result)
	}

	return results, nil
}

// fileHashWorker is a worker goroutine that processes file hashing jobs
func (dc *DirectoryCache) fileHashWorker(jobs <-chan fileJob, results chan<- fileResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		entry, err := dc.processFileJob(job)
		results <- fileResult{
			entry: entry,
			err:   err,
			index: job.index,
		}
	}
}
