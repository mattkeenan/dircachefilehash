//go:build exclude
package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ============================================================================
// DEPRECATED v0.6 SCAN FUNCTIONS - MOVED TO v0.6/
// ============================================================================
// 
// These functions are part of the old v0.6 architecture and have been
// replaced by the unified v0.7 architecture using hwangLinUnified() with
// callbacks. They are preserved here for reference and potential recovery
// scenarios but should not be used in new code.
//
// Replacement in v0.7:
// - hwangLinCompareToSkiplist() → hwangLinUnified() with CallbackScanCoordinator
// - performHwangLinScanToSkiplist() → runStatusWorkflowUnified()
// ============================================================================

// mockFileInfo implements os.FileInfo for deleted entries
type mockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return m.modTime }
func (m *mockFileInfo) IsDir() bool        { return m.mode.IsDir() }
func (m *mockFileInfo) Sys() interface{}   { return nil }

// scannedPath represents a file found during filesystem scanning
type scannedPath struct {
	AbsPath  string
	RelPath  string
	Info     os.FileInfo
	StatInfo *syscall.Stat_t
}

// hashJobStart represents a hash job being started
type hashJobStart struct {
	JobID       uint64
	Cookie      uint64         // External cookie for caller tracking
	FilePath    string
	IndexEntry  binaryEntryRef // Entry to update with hash (mremap-safe) - DEPRECATED for v0.7
	ScannedPath *scannedPath
	
	// v0.7 unified entry support - works for both mmap and heap entries
	Entry       BinaryEntryInterface // Unified interface for all entry types
}

// simpleHashManager - coordinates hash job completion
type simpleHashManager struct {
	hashJobChan    chan *hashJobStart
	callFinishChan chan uint64 // job completion notifications
	wg             sync.WaitGroup
	shutdownChan   <-chan struct{} // shutdown notification
	closed         bool            // track if channel is closed
	closeMutex     sync.Mutex      // protect closed flag
}

// hwangLinCompareToSkiplist performs Hwang-Lin comparison and builds scan index + skiplist
// DEPRECATED: This function is part of the v0.6 architecture. Use hwangLinUnified() with 
// CallbackScanCoordinator in v0.7 instead.
func (dc *DirectoryCache) hwangLinCompareToSkiplist(
	scanChan <-chan *scannedPath,
	compareSkiplist *skiplistWrapper,
	scanSkiplist *skiplistWrapper,
	scanFileName string,
	hashJobManager *simpleHashManager,
	callStartChan chan<- uint64,
	shutdownChan <-chan struct{},
) error {
	defer VerboseEnter()()
	
	// Track if we exit early to drain the channel
	earlyExit := false
	defer func() {
		if earlyExit {
			// Drain any remaining items from scanChan in a separate goroutine
			go func() {
				for {
					select {
					case <-scanChan:
						// Just consume to prevent blocking
					case <-shutdownChan:
						// Exit if shutdown signal received
						return
					default:
						// Channel is likely closed, exit
						return
					}
				}
			}()
		}
	}()
	
	var currentScanned *scannedPath
	var scanChanOpen bool = true
	currentIndex := compareSkiplist.skiplist.First()
	jobIDCounter := uint64(1)

	if IsDebugEnabled("scan") {
		VerboseLog(3, "hwangLinCompareToSkiplist: starting comparison, compareSkiplist length = %d", compareSkiplist.Length())
	}

	// Read first scanned path
	if scanChanOpen {
		currentScanned, scanChanOpen = <-scanChan
		if IsDebugEnabled("scan") && currentScanned != nil {
			VerboseLog(3, "hwangLinCompareToSkiplist: first scanned file = %s", currentScanned.RelPath)
		}
	}

	for scanChanOpen || currentIndex != nil {
		// Check for shutdown signal at the beginning of each iteration
		select {
		case <-shutdownChan:
			if IsDebugEnabled("scan") {
				VerboseLog(3, "hwangLinCompareToSkiplist: shutdown signal received, exiting")
			}
			earlyExit = true
			return fmt.Errorf("operation interrupted by shutdown signal")
		default:
			// Continue processing
		}

		var cmp int
		if !scanChanOpen {
			cmp = 1 // No more scanned files, only index entries remain (deletions)
		} else if currentIndex == nil {
			cmp = -1 // No more index entries, only scanned files remain (new files)
		} else {
			// Compare paths
			indexRef := currentIndex.Item()
			indexEntry := indexRef.GetBinaryEntry()
			if indexEntry == nil {
				return fmt.Errorf("GetBinaryEntry returned nil for index entry - this should never happen")
			}
			// Create string copy to avoid use-after-free when scan memory is unmapped
			indexPath := string([]byte(indexEntry.RelativePath()))
			cmp = strings.Compare(currentScanned.RelPath, indexPath)

		}

		if cmp == 0 {
			// File exists in both - check if changed
			indexRef := currentIndex.Item()
			indexEntry := indexRef.GetBinaryEntry()
			if indexEntry == nil {
				return fmt.Errorf("GetBinaryEntry returned nil for index entry - this should never happen")
			}

			// Skip deleted entries in the index
			if indexEntry.IsDeleted() {
				currentIndex = currentIndex.Next()
				continue
			}
			
			// Check if this file should still be indexed
			if !dc.shouldIndex(currentScanned.RelPath) {
				// File exists but should no longer be indexed (ignored or under unfollowed symlink)
				// Create a deleted entry
				deletedEntry, err := dc.appendEntryToScanIndex(scanFileName, currentScanned)
				if err != nil {
					return fmt.Errorf("failed to create deleted scan index entry: %w", err)
				}
				
				// Mark as deleted and preserve existing hash
				deletedEntry.SetDeleted()
				copy(deletedEntry.Hash[:], indexEntry.Hash[:])
				deletedEntry.HashType = indexEntry.HashType
				
				// Insert into scan skiplist
				deletedRef := createBinaryEntryRef(deletedEntry, dc.currentScan)
				scanSkiplist.Insert(deletedRef, ScanContext)
				
				// Advance both
				if scanChanOpen {
					currentScanned, scanChanOpen = <-scanChan
				}
				currentIndex = currentIndex.Next()
				continue
			}

			if dc.isFileChangedFromScanned(indexEntry, currentScanned) {
				// File modified - create scan index entry and submit for hashing
				scanEntry, err := dc.appendEntryToScanIndex(scanFileName, currentScanned)
				if err != nil {
					return fmt.Errorf("failed to create scan index entry: %w", err)
				}

				// Insert into scan skiplist using binaryEntryRef
				scanRef := createBinaryEntryRef(scanEntry, dc.currentScan)
				scanSkiplist.Insert(scanRef, ScanContext)

				// Submit for async hashing
				jobID := jobIDCounter
				jobIDCounter++

				hashJob := &hashJobStart{
					JobID:       jobID,
					FilePath:    currentScanned.AbsPath,
					IndexEntry:  createBinaryEntryRef(scanEntry, dc.currentScan), // Hash worker will update this safely
					ScannedPath: currentScanned,
				}

				// Check for shutdown before submitting new job
				if hashJobManager.IsShuttingDown() {
					if IsDebugEnabled("scanning") {
						fmt.Fprintf(os.Stderr, "[SCAN] Skipping hash job submission during shutdown for file: %s\n", currentScanned.RelPath)
					}
					// Don't return error - just stop submitting new jobs and continue with what we have
					// The scan skiplist already has the entry, we just won't hash it
					earlyExit = true
					break
				}

				hashJobManager.SubmitHashJob(hashJob, callStartChan)

			} else {
				// File unchanged - DO NOT create scan entry
				// The existing entry in main/cache index is already correct
				// Just continue to next file
				
				if IsDebugEnabled("scan") {
					VerboseLog(3, "hwangLinCompareToSkiplist: file unchanged, skipping: %s", currentScanned.RelPath)
				}
			}

			// Advance both
			if scanChanOpen {
				currentScanned, scanChanOpen = <-scanChan
			}
			currentIndex = currentIndex.Next()

		} else if cmp < 0 {
			// File only in scan - new file
			// Check if this file should be indexed
			if !dc.shouldIndex(currentScanned.RelPath) {
				// File should not be indexed (ignored or under unfollowed symlink)
				// Skip without creating entry
				if scanChanOpen {
					currentScanned, scanChanOpen = <-scanChan
				}
				continue
			}
			
			// Create scan index entry and submit for hashing
			scanEntry, err := dc.appendEntryToScanIndex(scanFileName, currentScanned)
			if err != nil {
				return fmt.Errorf("failed to create scan index entry: %w", err)
			}

			// Insert into scan skiplist using binaryEntryRef
			scanRef := createBinaryEntryRef(scanEntry, dc.currentScan)
			scanSkiplist.Insert(scanRef, ScanContext)

			// Submit for async hashing
			jobID := jobIDCounter
			jobIDCounter++

			hashJob := &hashJobStart{
				JobID:       jobID,
				FilePath:    currentScanned.AbsPath,
				IndexEntry:  createBinaryEntryRef(scanEntry, dc.currentScan), // Hash worker will update this safely
				ScannedPath: currentScanned,
			}

			// Check for shutdown before submitting new job
			if hashJobManager.IsShuttingDown() {
				if IsDebugEnabled("scanning") {
					fmt.Fprintf(os.Stderr, "[SCAN] Skipping hash job submission during shutdown for file: %s\n", currentScanned.RelPath)
				}
				// Don't return error - just stop submitting new jobs and continue with what we have
				// The scan skiplist already has the entry, we just won't hash it
				earlyExit = true
				break
			}

			if IsDebugEnabled("scanning") {
				fmt.Fprintf(os.Stderr, "[SCAN] Submitting hash job %d for file: %s\n", jobID, currentScanned.RelPath)
			}
			hashJobManager.SubmitHashJob(hashJob, callStartChan)

			// Advance scan
			if scanChanOpen {
				currentScanned, scanChanOpen = <-scanChan
			}

		} else {
			// File only in index - deleted file, mark as deleted in scan skiplist
			indexRef := currentIndex.Item()
			indexEntry := indexRef.GetBinaryEntry()
			if indexEntry == nil {
				return fmt.Errorf("GetBinaryEntry returned nil for index entry - this should never happen")
			}
			
			// Get the relative path for checking
			relPath := string([]byte(indexEntry.RelativePath()))
			
			// Check if this file should still be indexed based on symlink and ignore rules
			if dc.shouldIndex(relPath) {
				// File should be indexed but isn't present - it's been deleted from disk
				// Fall through to create deleted entry
			} else {
				// File should not be indexed (due to symlink or ignore rules)
				// Skip without creating deleted entry
				currentIndex = currentIndex.Next()
				continue
			}

			// Skip already deleted entries
			if !indexEntry.IsDeleted() {
				// Create a deleted entry in scan index using metadata from existing entry
				// We need to reconstruct os.FileInfo and syscall.Stat_t from the index entry
				mockInfo := &mockFileInfo{
					name:    filepath.Base(indexEntry.RelativePath()),
					size:    int64(indexEntry.FileSize),
					mode:    os.FileMode(indexEntry.Mode),
					modTime: timeFromWall(indexEntry.MTimeWall),
				}
				mockStat := &syscall.Stat_t{
					Dev:  uint64(indexEntry.Dev),
					Ino:  uint64(indexEntry.Ino),
					Mode: indexEntry.Mode,
					Uid:  indexEntry.UID,
					Gid:  indexEntry.GID,
					Ctim: syscall.Timespec{Sec: timeFromWall(indexEntry.CTimeWall).Unix(), Nsec: 0},
					Mtim: syscall.Timespec{Sec: timeFromWall(indexEntry.MTimeWall).Unix(), Nsec: 0},
				}

				// Create string copy to avoid use-after-free when scan memory is unmapped
				deletedEntry, err := dc.appendEntryToScanIndex(scanFileName, &scannedPath{
					RelPath:  string([]byte(indexEntry.RelativePath())),
					Info:     mockInfo,
					StatInfo: mockStat,
				})
				if err != nil {
					return fmt.Errorf("failed to create deleted scan index entry: %w", err)
				}

				// Mark as deleted and copy hash
				deletedEntry.SetDeleted()
				copy(deletedEntry.Hash[:], indexEntry.Hash[:])
				deletedEntry.HashType = indexEntry.HashType

				// Insert into scan skiplist using binaryEntryRef
				deletedRef := createBinaryEntryRef(deletedEntry, dc.currentScan)
				scanSkiplist.Insert(deletedRef, ScanContext)
			}

			// Advance index
			currentIndex = currentIndex.Next()
		}
	}

	return nil
}

// performHwangLinScanToSkiplist performs Hwang-Lin scan and builds a skiplist directly with scan index files
// DEPRECATED: This function is part of the v0.6 architecture. Use runStatusWorkflowUnified() 
// in v0.7 instead.
func (dc *DirectoryCache) performHwangLinScanToSkiplist(shutdownChan <-chan struct{}, paths []string, compareSkiplist *skiplistWrapper) (*skiplistWrapper, error) {
	defer VerboseEnter()()
	// Synchronise concurrent scans - only one scan per DirectoryCache at a time
	dc.scanMutex.Lock()
	defer dc.scanMutex.Unlock()

	// If a scan is already in progress, wait for it and return the same results
	if dc.scanInProgress {
		// TODO: Handle race condition where files change between when the first scan
		// started and when this concurrent caller started. Currently we return the
		// results from the first scan, but ideally we should detect if files changed
		// and re-run the scan if necessary.
		if dc.lastScanError != nil {
			return nil, dc.lastScanError
		}
		return dc.lastScanResult, nil
	}

	// Mark scan as in progress
	dc.scanInProgress = true
	defer func() {
		dc.scanInProgress = false
	}()

	// Create result skiplist for scan entries
	scanSkiplist := NewSkiplistWrapper(16, ScanContext)

	// Generate scan index filename for this operation
	scanFileName := dc.generateScanFileName()

	// Initialise scan index with mmap
	if err := dc.initialiseScanIndex(scanFileName); err != nil {
		return nil, fmt.Errorf("failed to initialise scan index: %w", err)
	}

	// Create channels for streaming data
	scanChan := make(chan *scannedPath, 50)
	callStartChan := make(chan uint64, 100)
	callFinishChan := make(chan uint64, 100)
	collectionStop := make(chan struct{})

	// Create hash job manager for concurrent hashing
	hashJobManager := dc.newSimpleHashManager(dc.hashWorkers, callFinishChan, shutdownChan)
	defer hashJobManager.Shutdown()

	// Start filesystem scan
	var scanWg sync.WaitGroup
	scanWg.Add(1)
	go func() {
		defer scanWg.Done()
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Starting filesystem scan\n")
		}
		if err := dc.scanPath(paths, scanChan, shutdownChan); err != nil {
			fmt.Fprintf(os.Stderr, "Scan error: %v\n", err)
		}
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Filesystem scan completed\n")
		}
	}()

	// Start modified Hwang-Lin comparison that builds scan index and skiplist
	var compareWg sync.WaitGroup
	compareWg.Add(1)
	go func() {
		defer compareWg.Done()
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Starting Hwang-Lin comparison\n")
		}
		if err := dc.hwangLinCompareToSkiplist(scanChan, compareSkiplist, scanSkiplist, scanFileName, hashJobManager, callStartChan, shutdownChan); err != nil {
			fmt.Fprintf(os.Stderr, "Compare error: %v\n", err)
		}
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Hwang-Lin comparison completed\n")
		}
	}()

	// Monitor hash jobs
	var monitorWg sync.WaitGroup
	monitorWg.Add(1)
	go func() {
		defer monitorWg.Done()
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Starting job monitor\n")
		}
		dc.monitorJobs(callStartChan, callFinishChan, collectionStop, shutdownChan)
		if IsDebugEnabled("scanning") {
			fmt.Fprintf(os.Stderr, "[SCAN] Job monitor completed\n")
		}
	}()

	// Wait for scan to complete
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Waiting for filesystem scan to complete\n")
	}
	scanWg.Wait()
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Filesystem scan wait completed\n")
	}

	// Check if shutdown occurred during scan
	select {
	case <-shutdownChan:
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[SCAN] Shutdown detected after filesystem scan, returning partial skiplist with %d entries\n", scanSkiplist.Length())
		}
		// Return partial skiplist with error to indicate incomplete scan
		return scanSkiplist, fmt.Errorf("operation interrupted by shutdown")
	default:
	}

	// Wait for comparison to complete
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Waiting for comparison to complete\n")
	}
	compareWg.Wait()
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Comparison wait completed\n")
	}

	// Check if shutdown occurred during comparison
	select {
	case <-shutdownChan:
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[SCAN] Shutdown detected after comparison, returning partial skiplist with %d entries\n", scanSkiplist.Length())
		}
		// Return partial skiplist with error to indicate incomplete scan
		return scanSkiplist, fmt.Errorf("operation interrupted by shutdown")
	default:
	}

	// Signal that no more hash jobs will be submitted
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Finishing hash job submission\n")
	}
	hashJobManager.FinishSubmitting()
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Hash job submission finished\n")
	}

	// Signal monitoring to stop and wait for all jobs to finish
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Stopping job monitor\n")
	}
	close(collectionStop)
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Waiting for job monitor to complete\n")
	}
	monitorWg.Wait()
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[SCAN] Job monitor wait completed\n")
	}

	if GetVerboseLevel() > 1 {
		fmt.Printf("Scan to skiplist completed\n")
	}

	// Store results for concurrent callers
	dc.lastScanResult = scanSkiplist
	dc.lastScanError = nil

	return scanSkiplist, nil
}