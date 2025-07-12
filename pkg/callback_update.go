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

			// Files are different if hwangLinUnified detected they don't match
			// This means file was modified - create scan entry and submit for hashing
			return true, uc.createScanEntryAndHash(rightEntry)
		}
		
	case ComparisonRightFirst:
		// File only in scan (right side) - new file (matches hwangLinCompareToSkiplist cmp < 0 case)
		if rightEntry != nil {
			// Check if this file should be indexed
			if !uc.dc.shouldIndex(rightPath) {
				// File should not be indexed - skip without creating entry
				return true, nil
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
	// For the unified architecture, the UnifiedFilesystemScanIterator handles hashing internally
	// We just need to add the already-processed entry to our result skiplist
	
	ref, ok := scanEntry.GetBinaryEntryRef()
	if !ok {
		return fmt.Errorf("scan entry does not support binaryEntryRef for update")
	}
	
	// Insert into result skiplist - entry should already be hashed by the iterator
	uc.resultSkiplist.Insert(ref, ScanContext)
	
	if IsDebugEnabled("scanning") {
		relPath, _ := scanEntry.RelativePath()
		fmt.Fprintf(os.Stderr, "[UPDATE] Collected scan entry for file: %s\n", relPath)
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