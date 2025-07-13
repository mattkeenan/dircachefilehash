package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// UpdateCallback implements HwangLinCallback to perform index update operations using the same logic as hwangLinCompareToSkiplist
// This replicates the exact update behavior but uses the unified algorithm infrastructure
type UpdateCallback struct {
	resultSkiplist  *skiplistWrapper
	scanFileName    string
	dc              *DirectoryCache
}

// NewUpdateCallback creates a new UpdateCallback that matches the existing update logic
func NewUpdateCallback(dc *DirectoryCache, scanFileName string) *UpdateCallback {
	return &UpdateCallback{
		resultSkiplist: NewSkiplistWrapper(16, ScanContext),
		scanFileName:   scanFileName,
		dc:             dc,
	}
}

// OnComparison processes each comparison result following hwangLinCompareToSkiplist logic
func (uc *UpdateCallback) OnComparison(
	result ComparisonResult,
	leftEntry, rightEntry BinaryEntryInterface,
	leftPath, rightPath string,
) (bool, error) {
	if IsDebugEnabled("verbose-3") {
		fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback.OnComparison: result=%s, leftPath=%s, rightPath=%s, leftEntry!=nil=%t, rightEntry!=nil=%t\n", 
			result, leftPath, rightPath, leftEntry != nil, rightEntry != nil)
	}
	
	switch result {
	case ComparisonMatch:
		// Files exist in both - check if they differ (matches hwangLinCompareToSkiplist cmp == 0 case)
		if leftEntry != nil && rightEntry != nil {
			// Skip deleted entries in the index (left side)
			if isDeleted, err := leftEntry.IsDeleted(); err == nil && isDeleted {
				return true, nil // Continue processing, skip this entry
			}
			
			// Check if this file should still be indexed
			if !uc.dc.shouldIndex(rightPath) {
				// File exists but should no longer be indexed - create deleted entry
				return true, uc.createDeletedEntry(leftEntry)
			}

			// Check if the file needs hashing (has changed)
			if needsHash(leftEntry, rightEntry) {
				// Request hashing for the changed file (rightEntry is the current filesystem state)
				if err := rightEntry.RequestHash(); err != nil {
					return false, err
				}
				// File was modified - create scan entry
				return true, uc.createScanEntryAndHash(rightEntry)
			}
			// File unchanged - no need to create scan entry
		}
		
	case ComparisonRightFirst:
		// File only in scan (right side) - new file (matches hwangLinCompareToSkiplist cmp < 0 case)
		if rightEntry != nil {
			if IsDebugEnabled("verbose-3") {
				fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback: ComparisonRightFirst for %s - processing new file\n", rightPath)
			}
			// Check if this file should be indexed
			if !uc.dc.shouldIndex(rightPath) {
				if IsDebugEnabled("verbose-3") {
					fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback: Skipping %s - shouldIndex returned false\n", rightPath)
				}
				// File should not be indexed - skip without creating entry
				return true, nil
			}
			
			if IsDebugEnabled("verbose-3") {
				fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback: Creating scan entry for new file %s\n", rightPath)
			}
			// Request hashing for new file (always needs hashing)
			if err := rightEntry.RequestHash(); err != nil {
				return false, err
			}
			// Create scan entry and submit for hashing
			return true, uc.createScanEntryAndHash(rightEntry)
		}
		
	case ComparisonLeftFirst:
		// File only in index (left side) - deleted file (matches hwangLinCompareToSkiplist cmp > 0 case)
		if leftEntry != nil {
			// Check if this file should still be indexed based on symlink and ignore rules
			if !uc.dc.shouldIndex(leftPath) {
				// File should not be indexed - skip without creating deleted entry
				return true, nil
			}

			// Skip already deleted entries
			if isDeleted, err := leftEntry.IsDeleted(); err == nil && isDeleted {
				return true, nil // Continue processing
			}

			// Create deleted entry
			return true, uc.createDeletedEntry(leftEntry)
		}
	}
	
	return true, nil // Continue processing
}

// createScanEntryAndHash collects a scan entry that has been processed by the iterator (matches hwangLinCompareToSkiplist logic)
func (uc *UpdateCallback) createScanEntryAndHash(scanEntry BinaryEntryInterface) error {
	relPath, _ := scanEntry.RelativePath()
	
	if IsDebugEnabled("verbose-3") {
		fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback.createScanEntryAndHash: processing file %s\n", relPath)
	}
	
	// For the unified architecture, the UnifiedFilesystemScanIterator handles hashing internally
	// We just need to add the already-processed entry to our result skiplist
	
	ref, ok := scanEntry.GetBinaryEntryRef()
	if !ok {
		if IsDebugEnabled("verbose-3") {
			fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback.createScanEntryAndHash: ERROR - scan entry does not support binaryEntryRef for %s\n", relPath)
		}
		return fmt.Errorf("scan entry does not support binaryEntryRef for update")
	}
	
	if IsDebugEnabled("verbose-3") {
		fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback.createScanEntryAndHash: inserting into skiplist for %s\n", relPath)
	}
	
	// Insert into result skiplist - entry should already be hashed by the iterator
	uc.resultSkiplist.Insert(ref, ScanContext)
	
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[UPDATE] Collected scan entry for file: %s\n", relPath)
	}
	
	if IsDebugEnabled("verbose-3") {
		fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback.createScanEntryAndHash: completed for %s\n", relPath)
	}
	
	return nil
}

// createDeletedEntry creates a deleted entry for a file that was in index but not found on disk
func (uc *UpdateCallback) createDeletedEntry(indexEntry BinaryEntryInterface) error {
	// This matches the deleted entry creation logic from hwangLinCompareToSkiplist
	
	relPath, err := indexEntry.RelativePath()
	if err != nil {
		return fmt.Errorf("failed to get relative path for deleted entry: %w", err)
	}
	
	// Create mock file info for the deleted entry (matches hwangLinCompareToSkiplist logic)
	size, _ := indexEntry.Size()
	mode, _ := indexEntry.Mode()
	mtimeWall, _ := indexEntry.MTimeWall()
	mtime := timeFromWall(mtimeWall)
	dev, _ := indexEntry.Dev()
	ino, _ := indexEntry.Ino()
	uid, _ := indexEntry.UID()
	gid, _ := indexEntry.GID()
	ctimeWall, _ := indexEntry.CTimeWall()
	ctime := timeFromWall(ctimeWall)
	
	mockInfo := &mockFileInfo{
		name:    filepath.Base(relPath),
		size:    int64(size),
		mode:    os.FileMode(mode),
		modTime: mtime,
	}
	mockStat := &syscall.Stat_t{
		Dev:  uint64(dev),
		Ino:  uint64(ino),
		Mode: mode,
		Uid:  uid,
		Gid:  gid,
		Ctim: syscall.Timespec{Sec: ctime.Unix(), Nsec: 0},
		Mtim: syscall.Timespec{Sec: mtime.Unix(), Nsec: 0},
	}

	// Create scanned path for deleted entry
	scannedPath := &scannedPath{
		RelPath:  relPath,
		AbsPath:  filepath.Join(uc.dc.RootDir, relPath),
		Info:     mockInfo,
		StatInfo: mockStat,
	}
	
	// Create deleted entry using existing appendEntryToScanIndex
	deletedEntry, err := uc.dc.appendEntryToScanIndex(uc.scanFileName, scannedPath)
	if err != nil {
		return fmt.Errorf("failed to create deleted scan index entry: %w", err)
	}
	
	// Mark as deleted and copy hash from original entry
	deletedEntry.SetDeleted()
	hash, err := indexEntry.Hash()
	if err == nil {
		copy(deletedEntry.Hash[:], hash[:])
	}
	hashType, err := indexEntry.HashType()
	if err == nil {
		deletedEntry.HashType = hashType
	}
	
	// Insert into result skiplist using binaryEntryRef (matches existing logic)
	deletedRef := createBinaryEntryRef(deletedEntry, uc.dc.currentScan)
	uc.resultSkiplist.Insert(deletedRef, ScanContext)
	
	return nil
}

// GetResultSkiplist returns the accumulated result skiplist
func (uc *UpdateCallback) GetResultSkiplist() *skiplistWrapper {
	return uc.resultSkiplist
}

// GetScanFileName returns the scan file name for this update operation
func (uc *UpdateCallback) GetScanFileName() string {
	return uc.scanFileName
}

// OnLeftOnly handles remaining entries from left iterator (when right is exhausted)
func (uc *UpdateCallback) OnLeftOnly(entry BinaryEntryInterface, path string) (bool, error) {
	// Left entry exists but no right entry - this is a deleted file
	return true, uc.createDeletedEntry(entry)
}

// OnRightOnly handles remaining entries from right iterator (when left is exhausted)  
func (uc *UpdateCallback) OnRightOnly(entry BinaryEntryInterface, path string) (bool, error) {
	// Right entry exists but no left entry - this is a new file
	// Check if this file should be indexed
	if !uc.dc.shouldIndex(path) {
		// File should not be indexed - skip without creating entry
		return true, nil
	}
	
	// Request hashing for new file (always needs hashing)
	if err := entry.RequestHash(); err != nil {
		return false, err
	}
	return true, uc.createScanEntryAndHash(entry)
}

// OnStart is called before the algorithm begins processing
func (uc *UpdateCallback) OnStart(leftName, rightName string) error {
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[UPDATE] Starting unified update: left=%s, right=%s\n", leftName, rightName)
	}
	return nil
}

// OnComplete is called after the algorithm finishes processing
func (uc *UpdateCallback) OnComplete(err error) error {
	if IsDebugEnabled("scanning") {
		if err != nil {
			fmt.Fprintf(os.Stderr, "[UPDATE] Update completed with error: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[UPDATE] Update completed successfully, result entries: %d\n", uc.resultSkiplist.Length())
		}
	}
	return nil
}

// Name returns the name of this callback for debugging
func (uc *UpdateCallback) Name() string {
	return "update"
}