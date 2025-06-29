package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"
)

// ValidationMode defines the strictness level for index validation
type ValidationMode int

const (
	ValidationStrict  ValidationMode = iota // idxck behavior - fail on any error
	ValidationLenient                       // recovery behavior - skip invalid entries
	ValidationDiagnostic                    // report all issues but continue
)

// ValidationConfig configures the unified validation system
type ValidationConfig struct {
	Mode             ValidationMode
	StructuralChecks bool // Binary format validation (alignment, sizes, etc.)
	LogicalChecks    bool // Data reasonableness (timestamps, file sizes, etc.)
	ChecksumValidation bool // Full file checksum verification
	Verbosity        int
	ContinueOnError  bool
	MaxPathLength    int
	MaxFileSize      uint64
	MinYear          int
	MaxYearOffset    int // Years from now
}

// DefaultValidationConfig returns a standard validation configuration
func DefaultValidationConfig(mode ValidationMode, verbosity int) ValidationConfig {
	return ValidationConfig{
		Mode:             mode,
		StructuralChecks: true,
		LogicalChecks:    true,
		ChecksumValidation: mode == ValidationStrict,
		Verbosity:        verbosity,
		ContinueOnError:  mode != ValidationStrict,
		MaxPathLength:    4096,
		MaxFileSize:      1 << 62, // 4 exabytes
		MinYear:          1970,
		MaxYearOffset:    1, // 1 year in future
	}
}

// UnifiedValidationProcessor creates a configurable validation processor
func UnifiedValidationProcessor(config ValidationConfig) EntryProcessor {
	return func(entry *binaryEntry, entryIndex uint32, filePath string) (bool, error) {
		var validationErrors []string
		
		// Structural validation
		if config.StructuralChecks {
			if err := validateEntryStructure(entry, entryIndex); err != nil {
				validationErrors = append(validationErrors, fmt.Sprintf("structural: %v", err))
				if config.Mode == ValidationStrict {
					return false, err
				}
			}
		}
		
		// Logical validation
		if config.LogicalChecks {
			if err := validateEntryLogical(entry, config); err != nil {
				validationErrors = append(validationErrors, fmt.Sprintf("logical: %v", err))
				if config.Mode == ValidationStrict {
					return false, err
				}
			}
		}
		
		// Handle validation results based on mode
		hasErrors := len(validationErrors) > 0
		
		switch config.Mode {
		case ValidationStrict:
			// Already handled above - any error causes immediate failure
			return !hasErrors, nil
			
		case ValidationLenient:
			// Skip entries with errors, log if verbose
			if hasErrors && config.Verbosity >= 2 {
				var path string
				if entry != nil {
					path = entry.RelativePath()
				}
				if path == "" {
					path = fmt.Sprintf("<entry-%d>", entryIndex)
				}
				for _, errMsg := range validationErrors {
					VerboseLog(2, "Validation: skipping entry %d (%s): %s", entryIndex, path, errMsg)
				}
			}
			return !hasErrors, nil
			
		case ValidationDiagnostic:
			// Include all entries but report issues
			if hasErrors && config.Verbosity >= 1 {
				var path string
				if entry != nil {
					path = entry.RelativePath()
				}
				if path == "" {
					path = fmt.Sprintf("<entry-%d>", entryIndex)
				}
				for _, errMsg := range validationErrors {
					VerboseLog(1, "Diagnostic: entry %d (%s): %s", entryIndex, path, errMsg)
				}
			}
			return true, nil // Include entry regardless of validation results
			
		default:
			return !hasErrors, nil
		}
	}
}

// validateEntryStructure performs binary format validation (idxck-style)
func validateEntryStructure(entry *binaryEntry, entryIndex uint32) error {
	// Basic nil check
	if entry == nil {
		return fmt.Errorf("nil entry at index %d", entryIndex)
	}
	
	// Size validation
	minSize := uint32(unsafe.Sizeof(binaryEntry{}))
	if entry.Size < minSize {
		return fmt.Errorf("entry size %d too small (minimum %d) at index %d", 
			entry.Size, minSize, entryIndex)
	}
	
	maxReasonableSize := uint32(4096) // Reasonable maximum for path + padding
	if entry.Size > maxReasonableSize {
		return fmt.Errorf("entry size %d unreasonably large (maximum %d) at index %d", 
			entry.Size, maxReasonableSize, entryIndex)
	}
	
	// 8-byte alignment validation
	if entry.Size%8 != 0 {
		return fmt.Errorf("entry size %d not 8-byte aligned at index %d", entry.Size, entryIndex)
	}
	
	// Validate that the entry pointer is 8-byte aligned
	entryPtr := uintptr(unsafe.Pointer(entry))
	if entryPtr%8 != 0 {
		return fmt.Errorf("entry pointer 0x%x not 8-byte aligned at index %d", entryPtr, entryIndex)
	}
	
	return nil
}

// validateEntryLogical performs data reasonableness validation (recovery-style)
func validateEntryLogical(entry *binaryEntry, config ValidationConfig) error {
	// Basic nil check
	if entry == nil {
		return fmt.Errorf("nil entry")
	}
	
	// Path validation
	path := entry.RelativePath()
	if path == "" {
		return fmt.Errorf("empty path")
	}
	
	if len(path) > config.MaxPathLength {
		return fmt.Errorf("path length %d exceeds maximum %d", len(path), config.MaxPathLength)
	}
	
	// File size validation
	if entry.FileSize > config.MaxFileSize {
		return fmt.Errorf("file size %d exceeds maximum %d", entry.FileSize, config.MaxFileSize)
	}
	
	// Hash validation
	hash := entry.HashString()
	if len(hash) == 0 {
		return fmt.Errorf("empty hash")
	}
	
	// Check for all-zero hash
	var hashLen int
	switch entry.HashType {
	case HashTypeSHA1:
		hashLen = HashSizeSHA1
	case HashTypeSHA256:
		hashLen = HashSizeSHA256
	case HashTypeSHA512:
		hashLen = HashSizeSHA512
	default:
		return fmt.Errorf("invalid hash type %d", entry.HashType)
	}
	
	allZero := true
	for i := 0; i < hashLen; i++ {
		if entry.Hash[i] != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return fmt.Errorf("all-zero hash")
	}
	
	// Timestamp validation
	ctime := timeFromWall(entry.CTimeWall)
	mtime := timeFromWall(entry.MTimeWall)
	minTime := time.Date(config.MinYear, 1, 1, 0, 0, 0, 0, time.UTC)
	maxTime := time.Now().Add(time.Duration(config.MaxYearOffset) * 365 * 24 * time.Hour)
	
	if ctime.Before(minTime) || ctime.After(maxTime) {
		return fmt.Errorf("invalid ctime %v (range: %v to %v)", ctime, minTime, maxTime)
	}
	if mtime.Before(minTime) || mtime.After(maxTime) {
		return fmt.Errorf("invalid mtime %v (range: %v to %v)", mtime, minTime, maxTime)
	}
	
	return nil
}

// RecoveryValidationProcessor validates binary entries for recovery operations
// Filters out corrupted or invalid entries while preserving valid ones
// DEPRECATED: Use UnifiedValidationProcessor with ValidationLenient mode instead
func RecoveryValidationProcessor(verbosity int) EntryProcessor {
	config := DefaultValidationConfig(ValidationLenient, verbosity)
	return UnifiedValidationProcessor(config)
}

// IdxckValidationProcessor creates a strict validation processor for index checking
// Equivalent to the validation logic used by the idxck command
func IdxckValidationProcessor(verbosity int) EntryProcessor {
	config := DefaultValidationConfig(ValidationStrict, verbosity)
	return UnifiedValidationProcessor(config)
}

// DiagnosticValidationProcessor creates a validation processor that reports all issues
// but includes all entries for diagnostic purposes
func DiagnosticValidationProcessor(verbosity int) EntryProcessor {
	config := DefaultValidationConfig(ValidationDiagnostic, verbosity)
	return UnifiedValidationProcessor(config)
}

// createPreRecoverySnapshot creates a complete backup of all index files before recovery
func (dc *DirectoryCache) createPreRecoverySnapshot(verbosity int) error {
	dcfhDir := filepath.Dir(dc.IndexFile)
	recoveryDir := filepath.Join(dcfhDir, "recovery")
	
	// Create recovery directory if it doesn't exist
	if err := os.MkdirAll(recoveryDir, 0755); err != nil {
		return fmt.Errorf("failed to create recovery directory: %w", err)
	}
	
	if verbosity >= 2 {
		VerboseLog(2, "Created recovery snapshot directory: %s", recoveryDir)
	}
	
	// List all .idx files in the .dcfh directory
	entries, err := os.ReadDir(dcfhDir)
	if err != nil {
		return fmt.Errorf("failed to read .dcfh directory: %w", err)
	}
	
	copiedCount := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".idx") {
			continue
		}
		
		sourcePath := filepath.Join(dcfhDir, entry.Name())
		destPath := filepath.Join(recoveryDir, entry.Name())
		
		// Copy file preserving metadata
		if err := dc.copyFileWithMetadata(sourcePath, destPath, verbosity); err != nil {
			if verbosity >= 1 {
				VerboseLog(1, "Warning: failed to backup %s: %v", entry.Name(), err)
			}
			continue // Non-fatal, continue with other files
		}
		
		copiedCount++
		if verbosity >= 2 {
			VerboseLog(2, "Backed up %s to recovery directory", entry.Name())
		}
	}
	
	if verbosity >= 1 {
		VerboseLog(1, "Pre-recovery snapshot created: %d index files backed up to %s", copiedCount, recoveryDir)
	}
	
	return nil
}

// copyFileWithMetadata copies a file while preserving its mtime and ctime
func (dc *DirectoryCache) copyFileWithMetadata(src, dst string, verbosity int) error {
	// Get source file info
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}
	
	// Read source file
	sourceData, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}
	
	// Write destination file
	if err := os.WriteFile(dst, sourceData, srcInfo.Mode()); err != nil {
		return fmt.Errorf("failed to write destination file: %w", err)
	}
	
	// Preserve modification time (note: ctime is set automatically by the filesystem)
	if err := os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		if verbosity >= 2 {
			VerboseLog(2, "Warning: failed to preserve mtime for %s: %v", dst, err)
		}
		// Non-fatal - file was copied successfully
	}
	
	return nil
}

// determineRecoveryType determines the type of recovery and target file based on source path
func (dc *DirectoryCache) determineRecoveryType(indexPath string) (string, string) {
	if indexPath == dc.IndexFile {
		return "main", dc.IndexFile
	} else if indexPath == dc.CacheFile {
		return "cache", dc.CacheFile
	} else if filepath.Base(indexPath) == "main.idx" {
		return "main", dc.IndexFile
	} else if filepath.Base(indexPath) == "cache.idx" {
		return "cache", dc.CacheFile
	} else if strings.Contains(filepath.Base(indexPath), "scan-") {
		return "scan", dc.CacheFile // Scan files recover to cache
	} else {
		return "unknown", dc.CacheFile // Default to cache
	}
}

// generateRecoveryBackupName creates a backup filename for recovery operations
func (dc *DirectoryCache) generateRecoveryBackupName(recoveryType string) string {
	dcfhDir := filepath.Dir(dc.IndexFile)
	return filepath.Join(dcfhDir, fmt.Sprintf("recover-%s-%d-%d.idx", recoveryType, os.Getpid(), getGoroutineID()))
}

// createRecoveryBackup creates a backup copy of a broken index file
func (dc *DirectoryCache) createRecoveryBackup(sourcePath, backupPath string, verbosity int) error {
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}
	
	if err := os.WriteFile(backupPath, sourceData, 0644); err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}
	
	if verbosity >= 2 {
		VerboseLog(2, "Created recovery backup: %s (%d bytes)", backupPath, len(sourceData))
	}
	
	return nil
}

// CreateEmptyMainIndex creates a new empty main index file
// This is useful for recovery when the main index is completely corrupted
func (dc *DirectoryCache) CreateEmptyMainIndex() error {
	defer VerboseEnter()()
	
	// CRITICAL: Create pre-recovery snapshot before replacing index files
	if err := dc.createPreRecoverySnapshot(1); err != nil {
		// Non-fatal for CreateEmptyMainIndex - warn but continue
		VerboseLog(1, "Warning: failed to create pre-recovery snapshot: %v", err)
	}
	
	// Create an empty skiplist
	emptySkiplist := NewSkiplistWrapper(16, MainContext)
	
	// Write empty index to a temp file first
	tempIndexPath := dc.generateTempFileName("index")
	if err := dc.writeMainIndexWithVectorIO(emptySkiplist, tempIndexPath, MainContext); err != nil {
		os.Remove(tempIndexPath)
		return fmt.Errorf("failed to write empty index: %w", err)
	}
	
	// Atomic replace main index
	if err := os.Rename(tempIndexPath, dc.IndexFile); err != nil {
		os.Remove(tempIndexPath) // Cleanup on failure
		return fmt.Errorf("failed to replace main index: %w", err)
	}
	
	// Remove cache file since we're starting fresh
	os.Remove(dc.CacheFile) // Non-fatal if it fails
	
	return nil
}

// CreatePreRecoverySnapshotForIdxck creates a pre-recovery snapshot specifically for idxck operations
// This is a public wrapper that can be called from CLI code
func (dc *DirectoryCache) CreatePreRecoverySnapshotForIdxck(verbosity int) error {
	return dc.createPreRecoverySnapshot(verbosity)
}

// RecoverFromIndex recovers a clean cache index from a potentially corrupted index file
// using validation filtering and the Hwang-Lin comparison workflow
func (dc *DirectoryCache) RecoverFromIndex(indexPath string, verbosity int) error {
	defer VerboseEnter()()
	
	if verbosity >= 1 {
		VerboseLog(1, "Starting index recovery from: %s", indexPath)
	}
	
	// CRITICAL: Create pre-recovery snapshot before any recovery operations
	if err := dc.createPreRecoverySnapshot(verbosity); err != nil {
		return fmt.Errorf("failed to create pre-recovery snapshot: %w", err)
	}
	
	// Check if source index exists
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		return fmt.Errorf("source index file does not exist: %s", indexPath)
	}
	
	// Load faulty index with validation filtering
	recoverySkiplist, err := dc.loadIndexWithProcessor(indexPath, RecoveryValidationProcessor(verbosity))
	if err != nil {
		return fmt.Errorf("failed to load source index for recovery: %w", err)
	}
	
	originalLength := recoverySkiplist.Length()
	if verbosity >= 1 {
		VerboseLog(1, "Loaded %d valid entries from source index", originalLength)
	}
	
	if originalLength == 0 {
		return fmt.Errorf("no valid entries found in source index")
	}
	
	// Now use Hwang-Lin workflow to merge with current disk state
	// This ensures we have the most up-to-date information
	currentSkiplist, err := dc.performHwangLinScanToSkiplist([]string{}, recoverySkiplist)
	if err != nil {
		return fmt.Errorf("failed to scan current state for recovery: %w", err)
	}
	
	if verbosity >= 1 {
		VerboseLog(1, "Merged with current disk state, result has %d entries", currentSkiplist.Length())
	}
	
	// Write to cache index using vectorio (include deleted entries for cache)
	tempCachePath := dc.generateTempFileName("cache")
	if err := dc.writeSkiplistWithVectorIO(currentSkiplist, tempCachePath, CacheContext); err != nil {
		os.Remove(tempCachePath)
		return fmt.Errorf("failed to write recovery cache index: %w", err)
	}
	
	// Cleanup scan index file now that temp index is written
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, but warn
		if verbosity >= 2 {
			VerboseLog(2, "Warning: failed to cleanup scan file: %v", err)
		}
	}
	
	// Atomic replace cache index
	if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
		os.Remove(tempCachePath) // Cleanup on failure
		return fmt.Errorf("failed to replace cache index: %w", err)
	}
	
	if verbosity >= 1 {
		VerboseLog(1, "Successfully recovered cache index from %s", indexPath)
	}
	
	return nil
}

// RecoverFromScanFiles attempts to recover from scan index files in the .dcfh directory
// This is useful when a previous operation was interrupted
func (dc *DirectoryCache) RecoverFromScanFiles(verbosity int) error {
	defer VerboseEnter()()
	
	// Find all scan index files
	scanFiles, err := dc.findScanIndexFiles()
	if err != nil {
		return fmt.Errorf("failed to find scan files: %w", err)
	}
	
	if len(scanFiles) == 0 {
		return fmt.Errorf("no scan index files found for recovery")
	}
	
	if verbosity >= 1 {
		VerboseLog(1, "Found %d scan index files for recovery", len(scanFiles))
	}
	
	// Use the most recent scan file (they're sorted by modification time)
	latestScanFile := scanFiles[0].Path
	
	if verbosity >= 1 {
		VerboseLog(1, "Using most recent scan file: %s", latestScanFile)
	}
	
	// Recover using the scan file
	return dc.RecoverFromIndex(latestScanFile, verbosity)
}

// AutoRecover attempts automatic recovery by trying multiple sources in order of preference
func (dc *DirectoryCache) AutoRecover(verbosity int) error {
	defer VerboseEnter()()
	
	if verbosity >= 1 {
		VerboseLog(1, "Starting automatic index recovery")
	}
	
	// CRITICAL: Create pre-recovery snapshot before any recovery operations
	if err := dc.createPreRecoverySnapshot(verbosity); err != nil {
		return fmt.Errorf("failed to create pre-recovery snapshot: %w", err)
	}
	
	// First, try comprehensive state preservation recovery if any index files exist
	hasAnyIndex := false
	if _, err := os.Stat(dc.IndexFile); err == nil {
		hasAnyIndex = true
	}
	if _, err := os.Stat(dc.CacheFile); err == nil {
		hasAnyIndex = true
	}
	if scanFiles, err := dc.findScanIndexFiles(); err == nil && len(scanFiles) > 0 {
		hasAnyIndex = true
	}
	
	if hasAnyIndex {
		if verbosity >= 1 {
			VerboseLog(1, "Attempting comprehensive recovery with state preservation")
		}
		if err := dc.RecoverWithStatePreservation(verbosity); err == nil {
			if verbosity >= 1 {
				VerboseLog(1, "Successfully recovered with state preservation")
			}
			return nil
		} else if verbosity >= 2 {
			VerboseLog(2, "Comprehensive recovery failed: %v", err)
		}
	}
	
	// Fallback strategies for partial recovery
	
	// Strategy 1: Try to recover from existing cache index (if it exists and partially readable)
	if _, err := os.Stat(dc.CacheFile); err == nil {
		if verbosity >= 1 {
			VerboseLog(1, "Attempting recovery from cache index only")
		}
		if err := dc.RecoverFromIndex(dc.CacheFile, verbosity); err == nil {
			if verbosity >= 1 {
				VerboseLog(1, "Successfully recovered from cache index")
			}
			return nil
		}
		if verbosity >= 2 {
			VerboseLog(2, "Cache index recovery failed: %v", err)
		}
	}
	
	// Strategy 2: Try to recover from scan files
	if verbosity >= 1 {
		VerboseLog(1, "Attempting recovery from scan files")
	}
	if err := dc.RecoverFromScanFiles(verbosity); err == nil {
		if verbosity >= 1 {
			VerboseLog(1, "Successfully recovered from scan files")
		}
		return nil
	} else if verbosity >= 2 {
		VerboseLog(2, "Scan file recovery failed: %v", err)
	}
	
	// Strategy 3: Try to recover from main index (if it exists)
	if _, err := os.Stat(dc.IndexFile); err == nil {
		if verbosity >= 1 {
			VerboseLog(1, "Attempting recovery from main index")
		}
		if err := dc.RecoverFromIndex(dc.IndexFile, verbosity); err == nil {
			if verbosity >= 1 {
				VerboseLog(1, "Successfully recovered from main index")
			}
			return nil
		} else if verbosity >= 2 {
			VerboseLog(2, "Main index recovery failed: %v", err)
		}
	}
	
	return fmt.Errorf("all recovery strategies failed")
}

// RecoverWithStatePreservation performs comprehensive recovery while preserving as much state as possible
func (dc *DirectoryCache) RecoverWithStatePreservation(verbosity int) error {
	defer VerboseEnter()()
	
	if verbosity >= 1 {
		VerboseLog(1, "Starting comprehensive recovery with state preservation")
	}
	
	// CRITICAL: Create pre-recovery snapshot before any recovery operations
	if err := dc.createPreRecoverySnapshot(verbosity); err != nil {
		return fmt.Errorf("failed to create pre-recovery snapshot: %w", err)
	}
	
	var recoveredSkiplists []*skiplistWrapper
	var backupPaths []string
	
	// Step 1: Try to recover from main index
	if _, err := os.Stat(dc.IndexFile); err == nil {
		mainBackup := dc.generateRecoveryBackupName("main")
		if err := dc.createRecoveryBackup(dc.IndexFile, mainBackup, verbosity); err == nil {
			backupPaths = append(backupPaths, mainBackup)
			
			if mainSkiplist, err := dc.loadIndexWithProcessor(dc.IndexFile, RecoveryValidationProcessor(verbosity)); err == nil && mainSkiplist.Length() > 0 {
				recoveredSkiplists = append(recoveredSkiplists, mainSkiplist)
				if verbosity >= 1 {
					VerboseLog(1, "Recovered %d entries from main index", mainSkiplist.Length())
				}
			}
		}
	}
	
	// Step 2: Try to recover from cache index
	if _, err := os.Stat(dc.CacheFile); err == nil {
		cacheBackup := dc.generateRecoveryBackupName("cache")
		if err := dc.createRecoveryBackup(dc.CacheFile, cacheBackup, verbosity); err == nil {
			backupPaths = append(backupPaths, cacheBackup)
			
			if cacheSkiplist, err := dc.loadIndexWithProcessor(dc.CacheFile, RecoveryValidationProcessor(verbosity)); err == nil && cacheSkiplist.Length() > 0 {
				recoveredSkiplists = append(recoveredSkiplists, cacheSkiplist)
				if verbosity >= 1 {
					VerboseLog(1, "Recovered %d entries from cache index", cacheSkiplist.Length())
				}
			}
		}
	}
	
	// Step 3: Try to recover from scan files
	if scanFiles, err := dc.findScanIndexFiles(); err == nil && len(scanFiles) > 0 {
		for _, scanFile := range scanFiles {
			scanBackup := dc.generateRecoveryBackupName("scan")
			if err := dc.createRecoveryBackup(scanFile.Path, scanBackup, verbosity); err == nil {
				backupPaths = append(backupPaths, scanBackup)
				
				if scanSkiplist, err := dc.loadIndexWithProcessor(scanFile.Path, RecoveryValidationProcessor(verbosity)); err == nil && scanSkiplist.Length() > 0 {
					recoveredSkiplists = append(recoveredSkiplists, scanSkiplist)
					if verbosity >= 1 {
						VerboseLog(1, "Recovered %d entries from scan file %s", scanSkiplist.Length(), filepath.Base(scanFile.Path))
					}
				}
			}
		}
	}
	
	if len(recoveredSkiplists) == 0 {
		return fmt.Errorf("no valid data could be recovered from any index files")
	}
	
	// Step 4: Merge all recovered data
	mergedSkiplist := recoveredSkiplists[0].Copy()
	for i := 1; i < len(recoveredSkiplists); i++ {
		if err := mergedSkiplist.Merge(recoveredSkiplists[i], MergeTheirs); err != nil {
			if verbosity >= 2 {
				VerboseLog(2, "Warning: failed to merge skiplist %d: %v", i, err)
			}
		}
	}
	
	if verbosity >= 1 {
		VerboseLog(1, "Merged recovery data: %d entries total", mergedSkiplist.Length())
	}
	
	// Step 5: Merge with current disk state via Hwang-Lin
	finalSkiplist, err := dc.performHwangLinScanToSkiplist([]string{}, mergedSkiplist)
	if err != nil {
		return fmt.Errorf("failed to merge recovered data with current state: %w", err)
	}
	
	// Step 6: Write recovered cache index
	tempCachePath := dc.generateTempFileName("cache")
	if err := dc.writeSkiplistWithVectorIO(finalSkiplist, tempCachePath, CacheContext); err != nil {
		os.Remove(tempCachePath)
		return fmt.Errorf("failed to write recovered cache index: %w", err)
	}
	
	// Step 7: Write recovered main index (excluding deleted)
	tempMainPath := dc.generateTempFileName("main")
	if err := dc.writeMainIndexWithVectorIO(finalSkiplist, tempMainPath, MainContext); err != nil {
		os.Remove(tempCachePath)
		os.Remove(tempMainPath)
		return fmt.Errorf("failed to write recovered main index: %w", err)
	}
	
	// Step 8: Atomic replacement
	if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
		os.Remove(tempCachePath)
		os.Remove(tempMainPath)
		return fmt.Errorf("failed to replace cache index: %w", err)
	}
	
	if err := os.Rename(tempMainPath, dc.IndexFile); err != nil {
		os.Remove(tempMainPath)
		return fmt.Errorf("failed to replace main index: %w", err)
	}
	
	// Cleanup scan files after successful recovery
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		if verbosity >= 2 {
			VerboseLog(2, "Warning: failed to cleanup scan file: %v", err)
		}
	}
	
	if verbosity >= 1 {
		VerboseLog(1, "Recovery completed successfully. Backups created:")
		for _, backup := range backupPaths {
			VerboseLog(1, "  %s", backup)
		}
		VerboseLog(1, "Final result: %d entries in both main and cache indices", finalSkiplist.Length())
	}
	
	return nil
}