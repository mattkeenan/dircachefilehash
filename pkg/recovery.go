package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ValidationMode defines the strictness level for index validation
type ValidationMode int

const (
	ValidationStrict     ValidationMode = iota // idxck behaviour - fail on any error
	ValidationLenient                          // recovery behaviour - skip invalid entries
	ValidationDiagnostic                       // report all issues but continue
	ValidationRecovery                         // recovery with fixing - allow fixable issues
)

// FixMode defines how fixes should be applied
type FixMode int

const (
	FixModeNone   FixMode = iota // No fixes applied
	FixModeAuto                  // Apply all safe fixes automatically
	FixModeManual                // Prompt user for each fix
)

// FixableIssue represents a validation issue that can potentially be fixed
type FixableIssue struct {
	Type        string       // Type of issue (e.g., "hash_type", "mtime", "missing_file")
	Description string       // Human-readable description
	FixAction   string       // Description of proposed fix
	CurrentPath string       // File path for file-based issues
	EntryIndex  uint32       // Entry index in the scan
	FixFunc     func() error // Function to apply the fix
}

// ValidationConfig configures the unified validation system
type ValidationConfig struct {
	Mode               ValidationMode
	FixMode            FixMode // How to handle fixable issues
	StructuralChecks   bool    // Binary format validation (alignment, sizes, etc.)
	LogicalChecks      bool    // Data reasonableness (timestamps, file sizes, etc.)
	ChecksumValidation bool    // Full file checksum verification
	Verbosity          int
	ContinueOnError    bool
	MaxPathLength      int
	MaxFileSize        uint64
	MinYear            int
	MaxYearOffset      int    // Years from now
	RootDir            string // Root directory for file path resolution
}

// DefaultValidationConfig returns a standard validation configuration
func DefaultValidationConfig(mode ValidationMode, verbosity int) ValidationConfig {
	return ValidationConfigWithFixes(mode, FixModeNone, verbosity, "")
}

// ValidationConfigWithFixes returns a validation configuration with fix mode support
func ValidationConfigWithFixes(mode ValidationMode, fixMode FixMode, verbosity int, rootDir string) ValidationConfig {
	return ValidationConfig{
		Mode:               mode,
		FixMode:            fixMode,
		StructuralChecks:   true,
		LogicalChecks:      true,
		ChecksumValidation: mode == ValidationStrict,
		Verbosity:          verbosity,
		ContinueOnError:    mode != ValidationStrict,
		MaxPathLength:      4096,
		MaxFileSize:        1 << 62, // 4 exabytes
		MinYear:            1970,
		MaxYearOffset:      1, // 1 year in future
		RootDir:            rootDir,
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

		case ValidationRecovery:
			// Include entries for recovery, but in auto mode skip entries with unfixable time issues
			if hasErrors {
				var path string
				if entry != nil {
					path = entry.RelativePath()
				}
				if path == "" {
					path = fmt.Sprintf("<entry-%d>", entryIndex)
				}

				// In auto mode, skip entries with time validation errors (unfixable)
				if config.FixMode == FixModeAuto {
					for _, errMsg := range validationErrors {
						if strings.Contains(errMsg, "invalid ctime") || strings.Contains(errMsg, "invalid mtime") {
							if config.Verbosity >= 2 {
								VerboseLog(2, "Auto mode: skipping entry %d (%s) with unfixable time issue: %s", entryIndex, path, errMsg)
							}
							return false, nil // Skip entry with unfixable time issues
						}
					}
				}

				if config.Verbosity >= 2 {
					for _, errMsg := range validationErrors {
						VerboseLog(2, "Recovery: including entry %d (%s) despite issues: %s", entryIndex, path, errMsg)
					}
				}
			}
			return true, nil // Include entry for potential fixing

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
		// In recovery mode, allow fixable hash type issues (like HashType=0)
		if config.Mode == ValidationRecovery && (entry.HashType == 0 || !isValidHashType(entry.HashType)) {
			// Use a reasonable default for hash length validation
			hashLen = HashSizeSHA256 // Default hash size for validation purposes
		} else {
			return fmt.Errorf("invalid hash type %d", entry.HashType)
		}
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
// using validation filtering and the Hwang-Lin comparison workflow with clean entry copying
func (dc *DirectoryCache) RecoverFromIndex(indexPath string, verbosity int) error {
	return dc.RecoverFromIndexWithFixes(indexPath, FixModeNone, verbosity)
}

// RecoverFromIndexWithFixes recovers a clean cache index with optional interactive fixing
func (dc *DirectoryCache) RecoverFromIndexWithFixes(indexPath string, fixMode FixMode, verbosity int) error {
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

	// Create recovery index for clean entry copies
	recoveryIndexPath := dc.generateTempFileName("recovery")
	if err := dc.createEmptyScanIndex(recoveryIndexPath); err != nil {
		return fmt.Errorf("failed to create recovery index: %w", err)
	}
	defer os.Remove(recoveryIndexPath) // Always cleanup recovery index

	// Load corrupted index with clean entry copying and fixing
	recoverySkiplist, err := dc.loadIndexWithCleanCopyingAndFixes(indexPath, recoveryIndexPath, fixMode, verbosity)
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
	currentSkiplist, err := dc.performHwangLinScanToSkiplist(nil, []string{}, recoverySkiplist)
	if err != nil {
		return fmt.Errorf("failed to scan current state for recovery: %w", err)
	}

	if verbosity >= 1 {
		VerboseLog(1, "Merged with current disk state, result has %d entries", currentSkiplist.Length())
	}

	// Write to both main and cache indices for complete recovery

	// 1. Write main index using vectorio (exclude deleted entries for main)
	tempMainPath := dc.generateTempFileName("main")
	if err := dc.writeMainIndexWithVectorIO(currentSkiplist, tempMainPath, MainContext); err != nil {
		os.Remove(tempMainPath)
		return fmt.Errorf("failed to write recovery main index: %w", err)
	}

	// 2. Write cache index using vectorio (include deleted entries for cache)
	tempCachePath := dc.generateTempFileName("cache")
	if err := dc.writeSkiplistWithVectorIO(currentSkiplist, tempCachePath, CacheContext); err != nil {
		os.Remove(tempMainPath) // Cleanup main on failure
		os.Remove(tempCachePath)
		return fmt.Errorf("failed to write recovery cache index: %w", err)
	}

	// Cleanup scan index file now that temp indices are written
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, but warn
		if verbosity >= 2 {
			VerboseLog(2, "Warning: failed to cleanup scan file: %v", err)
		}
	}

	// 3. Atomic replace main index first
	if err := os.Rename(tempMainPath, dc.IndexFile); err != nil {
		os.Remove(tempMainPath) // Cleanup on failure
		os.Remove(tempCachePath)
		return fmt.Errorf("failed to replace main index: %w", err)
	}

	// 4. Atomic replace cache index
	if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
		os.Remove(tempCachePath) // Cleanup on failure
		return fmt.Errorf("failed to replace cache index: %w", err)
	}

	if verbosity >= 1 {
		VerboseLog(1, "Successfully recovered both main and cache indices from %s", indexPath)
	}

	return nil
}

// loadIndexWithCleanCopyingOriginal loads an index file and creates clean copies of entries that need fixing (original implementation)
func (dc *DirectoryCache) loadIndexWithCleanCopyingOriginal(indexPath, recoveryIndexPath string, verbosity int) (*skiplistWrapper, error) {
	// Open the corrupted index file
	file, err := os.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file %s: %w", indexPath, err)
	}
	defer file.Close()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < HeaderSize {
		return nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	// Memory map the file for reading
	data, err := unix.Mmap(int(file.Fd()), 0, int(stat.Size()), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("failed to mmap file: %w", err)
	}
	defer unix.Munmap(data)

	// Get direct pointer to header in mmap'd memory
	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Basic header validation (signature, byte order, version)
	if err := header.ValidateSignature(dc.signature); err != nil {
		return nil, err
	}
	if err := header.ValidateByteOrder(); err != nil {
		return nil, err
	}
	if err := header.ValidateVersion(dc.version); err != nil {
		return nil, err
	}

	// Check Clean flag - we've already handled header checksum validation in loadIndexFromFileWithProcessor
	isClean := (header.Flags & IndexFlagClean) != 0
	if verbosity >= 2 {
		if isClean {
			VerboseLog(2, "Processing clean index file: %s", indexPath)
		} else {
			VerboseLog(2, "Processing unclean index file (likely interrupted): %s", indexPath)
		}
	}

	// Create skiplist for recovery
	skiplist := NewSkiplistWrapper(int(header.EntryCount), CacheContext)

	// Parse entries and create clean copies when needed
	offset := 0
	entryData := data[HeaderSize:]
	validEntryCount := 0

	for i := uint32(0); i < header.EntryCount; i++ {
		if offset >= len(entryData) {
			if verbosity >= 2 {
				VerboseLog(2, "Unexpected end of data at entry %d", i)
			}
			break
		}

		// Get direct pointer to binaryEntry in mmap'd memory
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))

		// Validate this entry with recovery validation (allows fixable issues)
		config := DefaultValidationConfig(ValidationRecovery, verbosity)
		processor := UnifiedValidationProcessor(config)

		shouldInclude, err := processor(entry, i, indexPath)
		if err != nil {
			if verbosity >= 2 {
				VerboseLog(2, "Entry %d validation failed: %v", i, err)
			}
			// Try to skip to next entry - use entry size or estimate
			if entry.Size > 0 && entry.Size < 4096 {
				offset += int(entry.Size)
			} else {
				offset += 256 // Conservative skip
			}
			continue
		}

		if shouldInclude {
			// Check if entry needs fixing (clean copying)
			needsFixing := false
			hashTypeFixed := false

			// Entry needs fixing if:
			// 1. File is unclean (header suggests corruption possible)
			// 2. Entry has structural issues but recoverable data
			// 3. Entry has invalid hash type that can be corrected
			if !isClean {
				needsFixing = true
			}

			// Check for hash type issues and fix if needed
			if entry.HashType == 0 || !isValidHashType(entry.HashType) {
				if verbosity >= 2 {
					VerboseLog(2, "Entry %d has invalid hash type %d", i, entry.HashType)
				}
				needsFixing = true
				hashTypeFixed = true
			}

			if needsFixing {
				// Create clean copy in recovery index (with hash type fixing if needed)
				cleanEntryRef, err := dc.createCleanEntryCopyWithFixes(entry, recoveryIndexPath, hashTypeFixed, verbosity)
				if err != nil {
					if verbosity >= 2 {
						VerboseLog(2, "Failed to create clean copy of entry %d: %v", i, err)
					}
					offset += int(entry.Size)
					continue
				}

				// Add clean copy to skiplist
				skiplist.Insert(cleanEntryRef, CacheContext)
				validEntryCount++

				if verbosity >= 3 {
					VerboseLog(3, "Created clean copy for entry %d: %s", i, entry.RelativePath())
				}
			} else {
				// In recovery mode, we need clean copies of all entries from unclean files
				// to ensure memory safety - force clean copying
				needsFixing = true
			}
		}

		// Move to next entry
		offset += int(entry.Size)
	}

	if verbosity >= 1 {
		VerboseLog(1, "Recovered %d valid entries from %d total entries", validEntryCount, header.EntryCount)
	}

	return skiplist, nil
}

// loadIndexWithCleanCopyingAndFixes loads an index file and creates clean copies with interactive fixing support
func (dc *DirectoryCache) loadIndexWithCleanCopyingAndFixes(indexPath, recoveryIndexPath string, fixMode FixMode, verbosity int) (*skiplistWrapper, error) {
	// Create enhanced validation config with fix mode support
	config := ValidationConfigWithFixes(ValidationRecovery, fixMode, verbosity, dc.RootDir)

	// Use the existing function as a base, but with enhanced validation
	return dc.loadIndexWithCleanCopyingEnhanced(indexPath, recoveryIndexPath, config)
}

// loadIndexWithCleanCopying loads an index file and creates clean copies of entries that need fixing (legacy compatibility)
func (dc *DirectoryCache) loadIndexWithCleanCopying(indexPath, recoveryIndexPath string, verbosity int) (*skiplistWrapper, error) {
	config := ValidationConfigWithFixes(ValidationRecovery, FixModeNone, verbosity, dc.RootDir)
	return dc.loadIndexWithCleanCopyingEnhanced(indexPath, recoveryIndexPath, config)
}

// loadIndexWithCleanCopyingEnhanced is the core implementation with full fix support
func (dc *DirectoryCache) loadIndexWithCleanCopyingEnhanced(indexPath, recoveryIndexPath string, config ValidationConfig) (*skiplistWrapper, error) {
	// Open the corrupted index file
	file, err := os.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file %s: %w", indexPath, err)
	}
	defer file.Close()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < HeaderSize {
		return nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	// Memory map the file for reading
	data, err := unix.Mmap(int(file.Fd()), 0, int(stat.Size()), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("failed to mmap file: %w", err)
	}
	defer unix.Munmap(data)

	// Get direct pointer to header in mmap'd memory
	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Basic header validation (signature, byte order, version)
	if err := header.ValidateSignature(dc.signature); err != nil {
		return nil, err
	}
	if err := header.ValidateByteOrder(); err != nil {
		return nil, err
	}
	if err := header.ValidateVersion(dc.version); err != nil {
		return nil, err
	}

	// Check Clean flag
	isClean := (header.Flags & IndexFlagClean) != 0
	if config.Verbosity >= 2 {
		if isClean {
			VerboseLog(2, "Processing clean index file: %s", indexPath)
		} else {
			VerboseLog(2, "Processing unclean index file (likely interrupted): %s", indexPath)
		}
	}

	// Create skiplist for recovery
	skiplist := NewSkiplistWrapper(int(header.EntryCount), CacheContext)

	// Parse entries and apply fixes
	offset := 0
	entryData := data[HeaderSize:]
	validEntryCount := 0
	fixesApplied := 0

	for i := uint32(0); i < header.EntryCount; i++ {
		if offset >= len(entryData) {
			if config.Verbosity >= 2 {
				VerboseLog(2, "Unexpected end of data at entry %d", i)
			}
			break
		}

		// Get direct pointer to binaryEntry in mmap'd memory
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))

		// Create a copy of the entry for potential fixing
		entrySize := int(entry.Size)
		if entrySize <= 0 || entrySize > 4096 {
			if config.Verbosity >= 2 {
				VerboseLog(2, "Invalid entry size %d at entry %d, skipping", entrySize, i)
			}
			offset += 256 // Conservative skip
			continue
		}

		entryCopy := make([]byte, entrySize)
		sourceBytes := (*[4096]byte)(unsafe.Pointer(entry))[:entrySize:entrySize]
		copy(entryCopy, sourceBytes)

		// Get pointer to our copy for fixing
		workingEntry := (*binaryEntry)(unsafe.Pointer(&entryCopy[0]))

		// Apply fixes to the working copy
		hadFixes, err := dc.applyFixesToEntry(workingEntry, i, config)
		if err != nil {
			if config.Verbosity >= 2 {
				VerboseLog(2, "Failed to apply fixes to entry %d: %v", i, err)
			}
			offset += int(entry.Size)
			continue
		}

		if hadFixes {
			fixesApplied++
		}

		// Validate the (potentially fixed) entry
		processor := UnifiedValidationProcessor(config)
		shouldInclude, err := processor(workingEntry, i, indexPath)
		if err != nil {
			if config.Verbosity >= 2 {
				VerboseLog(2, "Entry %d validation failed even after fixes: %v", i, err)
			}
			offset += int(entry.Size)
			continue
		}

		if shouldInclude {
			// Create clean copy in recovery index
			_, cleanOffset, err := dc.appendRawEntryToScanIndex(recoveryIndexPath, entryCopy)
			if err != nil {
				if config.Verbosity >= 2 {
					VerboseLog(2, "Failed to create clean copy of entry %d: %v", i, err)
				}
				offset += int(entry.Size)
				continue
			}

			// Create skiplist reference to the clean copy
			recoveryIndexFile := &mmapIndexFile{
				File:     nil,
				Data:     nil,
				Size:     0,
				Offset:   int(cleanOffset),
				Type:     "recovery",
				FilePath: recoveryIndexPath,
			}

			cleanEntryRef := binaryEntryRef{
				Offset:    int(cleanOffset),
				IndexFile: recoveryIndexFile,
			}

			skiplist.Insert(cleanEntryRef, CacheContext)
			validEntryCount++

			if config.Verbosity >= 3 {
				VerboseLog(3, "Successfully processed entry %d: %s", i, workingEntry.RelativePath())
			}
		}

		// Move to next entry
		offset += int(entry.Size)
	}

	if config.Verbosity >= 1 {
		VerboseLog(1, "Enhanced recovery: processed %d valid entries from %d total, applied %d fixes",
			validEntryCount, header.EntryCount, fixesApplied)
	}

	return skiplist, nil
}

// isValidHashType checks if a hash type is valid
func isValidHashType(hashType uint16) bool {
	switch hashType {
	case HashTypeSHA1, HashTypeSHA256, HashTypeSHA512:
		return true
	default:
		return false
	}
}

// createCleanEntryCopyWithFixes creates a clean copy of a binaryEntry with optional fixes applied
func (dc *DirectoryCache) createCleanEntryCopyWithFixes(sourceEntry *binaryEntry, recoveryIndexPath string, fixHashType bool, verbosity int) (binaryEntryRef, error) {
	// Copy the entry data to clean memory
	entrySize := int(sourceEntry.Size)
	entryData := make([]byte, entrySize)

	// Copy from source entry
	sourceBytes := (*[4096]byte)(unsafe.Pointer(sourceEntry))[:entrySize:entrySize]
	copy(entryData, sourceBytes)

	// Apply fixes if needed
	if fixHashType {
		// Get the clean entry pointer to modify
		cleanEntry := (*binaryEntry)(unsafe.Pointer(&entryData[0]))

		// Fix hash type - use current configured hash type
		newHashType := dc.GetCurrentHashType()
		if verbosity >= 2 {
			VerboseLog(2, "Fixing hash type from %d to %d for entry: %s",
				cleanEntry.HashType, newHashType, cleanEntry.RelativePath())
		}
		cleanEntry.HashType = newHashType
	}

	// Append the clean copy to recovery index using raw data append
	_, cleanOffset, err := dc.appendRawEntryToScanIndex(recoveryIndexPath, entryData)
	if err != nil {
		return binaryEntryRef{}, fmt.Errorf("failed to append clean entry to recovery index: %w", err)
	}

	// For recovery operations, we create a temporary mmapIndexFile reference
	// This is safe because the recovery index will be cleaned up after use
	recoveryIndexFile := &mmapIndexFile{
		File:     nil, // File will be closed by caller
		Data:     nil, // Will be set when needed
		Size:     0,   // Will be updated when accessed
		Offset:   int(cleanOffset),
		Type:     "recovery",
		FilePath: recoveryIndexPath,
	}

	return binaryEntryRef{
		Offset:    int(cleanOffset),
		IndexFile: recoveryIndexFile,
	}, nil
}

// analyzeEntryForFixes analyzes a binary entry and returns a list of fixable issues
func (dc *DirectoryCache) analyzeEntryForFixes(entry *binaryEntry, entryIndex uint32, config ValidationConfig) ([]FixableIssue, error) {
	var issues []FixableIssue

	// Get entry path for file-based checks
	entryPath := entry.RelativePath()
	fullPath := filepath.Join(config.RootDir, entryPath)

	// Check 1: Hash Type Issues
	if entry.HashType == 0 || !isValidHashType(entry.HashType) {
		currentHashType := dc.GetCurrentHashType()
		issues = append(issues, FixableIssue{
			Type:        "hash_type",
			Description: fmt.Sprintf("Invalid hash type %d", entry.HashType),
			FixAction:   fmt.Sprintf("Update to current configured hash type (%d)", currentHashType),
			CurrentPath: entryPath,
			EntryIndex:  entryIndex,
			FixFunc: func() error {
				entry.HashType = currentHashType
				return nil
			},
		})
	}

	// Check 2: Missing Files (offer to delete entry)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		issues = append(issues, FixableIssue{
			Type:        "missing_file",
			Description: fmt.Sprintf("File no longer exists: %s", entryPath),
			FixAction:   "Mark entry as deleted (will be excluded from output)",
			CurrentPath: entryPath,
			EntryIndex:  entryIndex,
			FixFunc: func() error {
				entry.SetDeleted()
				return nil
			},
		})
	} else if err == nil {
		// File exists - check for other issues

		// Get current file info
		stat, err := os.Stat(fullPath)
		if err != nil {
			return issues, fmt.Errorf("failed to stat file %s: %w", fullPath, err)
		}

		sysStat, ok := stat.Sys().(*syscall.Stat_t)
		if !ok {
			// Can't get detailed file info, skip file-based fixes
			return issues, nil
		}

		// Check 3: File Size Mismatch
		if entry.FileSize != uint64(stat.Size()) {
			issues = append(issues, FixableIssue{
				Type:        "file_size",
				Description: fmt.Sprintf("File size mismatch: entry has %d bytes, file has %d bytes", entry.FileSize, stat.Size()),
				FixAction:   fmt.Sprintf("Update entry to match current file size (%d bytes)", stat.Size()),
				CurrentPath: entryPath,
				EntryIndex:  entryIndex,
				FixFunc: func() error {
					entry.FileSize = uint64(stat.Size())
					return nil
				},
			})
		}

		// Check 4: Mode Mismatch
		if entry.Mode != uint32(stat.Mode()) {
			issues = append(issues, FixableIssue{
				Type:        "file_mode",
				Description: fmt.Sprintf("File mode mismatch: entry has %o, file has %o", entry.Mode, stat.Mode()),
				FixAction:   fmt.Sprintf("Update entry to match current file mode (%o)", stat.Mode()),
				CurrentPath: entryPath,
				EntryIndex:  entryIndex,
				FixFunc: func() error {
					entry.Mode = uint32(stat.Mode())
					return nil
				},
			})
		}

		// Check 5: Time Mismatch (be more lenient with time - only fix if very different)
		currentMTime := encodeWallTime(sysStat.Mtim.Sec, sysStat.Mtim.Nsec)
		if entry.MTimeWall != currentMTime {
			// Only offer to fix if the difference is significant (more than 1 second)
			entryTime := timeFromWall(entry.MTimeWall)
			currentTime := timeFromWall(currentMTime)
			if entryTime.Sub(currentTime).Abs() > time.Second {
				issues = append(issues, FixableIssue{
					Type:        "mtime",
					Description: fmt.Sprintf("Modification time mismatch: entry has %v, file has %v", entryTime, currentTime),
					FixAction:   fmt.Sprintf("Update entry to match current file time (%v)", currentTime),
					CurrentPath: entryPath,
					EntryIndex:  entryIndex,
					FixFunc: func() error {
						entry.MTimeWall = currentMTime
						entry.CTimeWall = encodeWallTime(sysStat.Ctim.Sec, sysStat.Ctim.Nsec)
						return nil
					},
				})
			}
		}

		// Check 6: UID/GID Mismatch
		if entry.UID != sysStat.Uid || entry.GID != sysStat.Gid {
			issues = append(issues, FixableIssue{
				Type:        "ownership",
				Description: fmt.Sprintf("Ownership mismatch: entry has %d:%d, file has %d:%d", entry.UID, entry.GID, sysStat.Uid, sysStat.Gid),
				FixAction:   fmt.Sprintf("Update entry to match current ownership (%d:%d)", sysStat.Uid, sysStat.Gid),
				CurrentPath: entryPath,
				EntryIndex:  entryIndex,
				FixFunc: func() error {
					entry.UID = sysStat.Uid
					entry.GID = sysStat.Gid
					return nil
				},
			})
		}
	}

	return issues, nil
}

// promptUserForFix prompts the user whether to apply a specific fix
func promptUserForFix(issue FixableIssue) bool {
	fmt.Printf("\nIssue found in entry %d (%s):\n", issue.EntryIndex, issue.CurrentPath)
	fmt.Printf("  Problem: %s\n", issue.Description)
	fmt.Printf("  Proposed fix: %s\n", issue.FixAction)
	fmt.Printf("Apply this fix? [y/N]: ")

	var response string
	fmt.Scanln(&response)

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// applyFixesToEntry analyzes and optionally applies fixes to a binary entry
func (dc *DirectoryCache) applyFixesToEntry(entry *binaryEntry, entryIndex uint32, config ValidationConfig) (bool, error) {
	// Analyze entry for fixable issues
	issues, err := dc.analyzeEntryForFixes(entry, entryIndex, config)
	if err != nil {
		return false, fmt.Errorf("failed to analyze entry for fixes: %w", err)
	}

	if len(issues) == 0 {
		return false, nil // No fixes needed
	}

	appliedFixes := false

	for _, issue := range issues {
		shouldApply := false

		switch config.FixMode {
		case FixModeAuto:
			// Apply all safe fixes automatically
			shouldApply = true
			if config.Verbosity >= 1 {
				VerboseLog(1, "Auto-applying fix for %s: %s", issue.Type, issue.Description)
			}

		case FixModeManual:
			// Prompt user for each fix
			shouldApply = promptUserForFix(issue)

		case FixModeNone:
			// No fixes applied
			if config.Verbosity >= 2 {
				VerboseLog(2, "Found fixable issue (not applying): %s - %s", issue.Type, issue.Description)
			}
			continue
		}

		if shouldApply {
			if err := issue.FixFunc(); err != nil {
				return false, fmt.Errorf("failed to apply fix for %s: %w", issue.Type, err)
			}
			appliedFixes = true

			if config.Verbosity >= 2 {
				VerboseLog(2, "Applied fix for %s: %s", issue.Type, issue.FixAction)
			}
		}
	}

	return appliedFixes, nil
}

// createCleanEntryCopy creates a clean copy of a binaryEntry in the recovery index file (legacy compatibility)
func (dc *DirectoryCache) createCleanEntryCopy(sourceEntry *binaryEntry, recoveryIndexPath string) (binaryEntryRef, error) {
	return dc.createCleanEntryCopyWithFixes(sourceEntry, recoveryIndexPath, false, 0)
}

// appendRawEntryToScanIndex appends raw binaryEntry data to a scan index file
func (dc *DirectoryCache) appendRawEntryToScanIndex(scanIndexPath string, entryData []byte) (*binaryEntry, uint32, error) {
	// Open scan index file for append
	file, err := os.OpenFile(scanIndexPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open scan index for append: %w", err)
	}
	defer file.Close()

	// Get current file size to determine append offset
	stat, err := file.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to stat scan index: %w", err)
	}

	currentSize := stat.Size()
	appendOffset := uint32(currentSize)

	// Calculate new file size
	newSize := currentSize + int64(len(entryData))

	// Extend the file
	if err := file.Truncate(newSize); err != nil {
		return nil, 0, fmt.Errorf("failed to extend scan index: %w", err)
	}

	// Memory map the extended file
	data, err := unix.Mmap(int(file.Fd()), 0, int(newSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to mmap extended scan index: %w", err)
	}
	defer unix.Munmap(data)

	// Copy entry data to the append position
	copy(data[currentSize:], entryData)

	// Get pointer to the appended entry
	cleanEntry := (*binaryEntry)(unsafe.Pointer(&data[currentSize]))

	// Update header entry count
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	header.EntryCount++

	return cleanEntry, appendOffset, nil
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
	finalSkiplist, err := dc.performHwangLinScanToSkiplist(nil, []string{}, mergedSkiplist)
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
