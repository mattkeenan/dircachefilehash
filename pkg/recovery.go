package dircachefilehash

import (
	"context"
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

// ValidationProcessor creates a configurable validation processor
func ValidationProcessor(config ValidationConfig) EntryProcessor {
	return func(entry *binaryEntry, entryIndex uint32, _ string) (bool, error) {
		errs, strictErr := collectValidationErrors(entry, entryIndex, config)
		if strictErr != nil {
			return false, strictErr
		}
		return decideInclusion(config, entry, entryIndex, errs)
	}
}

// collectValidationErrors runs the structural and logical checks
// enabled in config and returns the aggregated error messages. If
// the mode is strict and any check fails, the original error is
// returned via strictErr so the caller can bail immediately.
func collectValidationErrors(entry *binaryEntry, entryIndex uint32, config ValidationConfig) (errs []string, strictErr error) {
	if config.StructuralChecks {
		if err := validateEntryStructure(entry, entryIndex); err != nil {
			errs = append(errs, fmt.Sprintf("structural: %v", err))
			if config.Mode == ValidationStrict {
				return errs, err
			}
		}
	}
	if config.LogicalChecks {
		if err := validateEntryLogical(entry, config); err != nil {
			errs = append(errs, fmt.Sprintf("logical: %v", err))
			if config.Mode == ValidationStrict {
				return errs, err
			}
		}
	}
	return errs, nil
}

// decideInclusion applies the mode-specific policy: strict/lenient
// drop on error, diagnostic always includes, recovery includes but
// drops auto-fix candidates with unfixable time issues.
func decideInclusion(config ValidationConfig, entry *binaryEntry, entryIndex uint32, errs []string) (bool, error) {
	hasErrors := len(errs) > 0
	switch config.Mode {
	case ValidationStrict, ValidationLenient:
		if hasErrors && config.Mode == ValidationLenient && config.Verbosity >= 2 {
			logValidationErrors(entry, entryIndex, errs, 2, "Validation: skipping entry %d (%s): %s")
		}
		return !hasErrors, nil
	case ValidationDiagnostic:
		if hasErrors && config.Verbosity >= 1 {
			logValidationErrors(entry, entryIndex, errs, 1, "Diagnostic: entry %d (%s): %s")
		}
		return true, nil
	case ValidationRecovery:
		if hasErrors {
			if config.FixMode == FixModeAuto && hasUnfixableTimeError(errs) {
				if config.Verbosity >= 2 {
					logValidationErrors(entry, entryIndex, errs, 2, "Auto mode: skipping entry %d (%s) with unfixable time issue: %s")
				}
				return false, nil
			}
			if config.Verbosity >= 2 {
				logValidationErrors(entry, entryIndex, errs, 2, "Recovery: including entry %d (%s) despite issues: %s")
			}
		}
		return true, nil
	default:
		return !hasErrors, nil
	}
}

// resolveValidationPath produces a stable identifier for logging when
// an entry may be nil or have an empty path (corrupt/indexOnly).
func resolveValidationPath(entry *binaryEntry, entryIndex uint32) string {
	if entry != nil {
		if p := entry.RelativePath(); p != "" {
			return p
		}
	}
	return fmt.Sprintf("<entry-%d>", entryIndex)
}

// logValidationErrors emits one VerboseLog line per error message
// at the given level using a printf-style format expecting
// (index, path, errMsg) placeholders.
func logValidationErrors(entry *binaryEntry, entryIndex uint32, errs []string, level int, format string) {
	path := resolveValidationPath(entry, entryIndex)
	for _, errMsg := range errs {
		VerboseLog(level, format, entryIndex, path, errMsg)
	}
}

// hasUnfixableTimeError reports whether any error in errs describes
// an invalid ctime/mtime — these are unfixable in auto mode.
func hasUnfixableTimeError(errs []string) bool {
	for _, errMsg := range errs {
		if strings.Contains(errMsg, "invalid ctime") || strings.Contains(errMsg, "invalid mtime") {
			return true
		}
	}
	return false
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

// RecoveryValidationProcessor validates binary entries for recovery operations.
// Filters out corrupted or invalid entries while preserving valid ones.
//
// Deprecated: Use ValidationProcessor with ValidationLenient mode instead.
func RecoveryValidationProcessor(verbosity int) EntryProcessor {
	config := DefaultValidationConfig(ValidationLenient, verbosity)
	return ValidationProcessor(config)
}

// IdxckValidationProcessor creates a strict validation processor for index checking
// Equivalent to the validation logic used by the idxck command
func IdxckValidationProcessor(verbosity int) EntryProcessor {
	config := DefaultValidationConfig(ValidationStrict, verbosity)
	return ValidationProcessor(config)
}

// DiagnosticValidationProcessor creates a validation processor that reports all issues
// but includes all entries for diagnostic purposes
func DiagnosticValidationProcessor(verbosity int) EntryProcessor {
	config := DefaultValidationConfig(ValidationDiagnostic, verbosity)
	return ValidationProcessor(config)
}

// createPreRecoverySnapshot creates a complete backup of all index files before recovery
func (dc *DirectoryCache) createPreRecoverySnapshot(verbosity int) error {
	metaDir := dc.MetaDir
	recoveryDir := filepath.Join(metaDir, "recovery")

	// Create recovery directory if it doesn't exist
	if err := os.MkdirAll(recoveryDir, 0755); err != nil {
		return fmt.Errorf("failed to create recovery directory: %w", err)
	}

	if verbosity >= 2 {
		VerboseLog(2, "Created recovery snapshot directory: %s", recoveryDir)
	}

	// List all .idx files in the .dcfh directory
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		return fmt.Errorf("failed to read .dcfh directory: %w", err)
	}

	copiedCount := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".idx") {
			continue
		}

		sourcePath := filepath.Join(metaDir, entry.Name())
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

// generateRecoveryBackupName creates a backup filename for recovery operations
func (dc *DirectoryCache) generateRecoveryBackupName(recoveryType string) string {
	return filepath.Join(dc.MetaDir, fmt.Sprintf("recover-%s-%d-%d.idx", recoveryType, os.Getpid(), getGoroutineID()))
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
	_ = NewSkiplistWrapper(16, MainContext) // emptySkiplist - unused until RecoveryCallback implemented

	// Write empty index to a temp file first
	_ = dc.generateTempFileName("index") // tempIndexPath - unused until RecoveryCallback implemented
	// TODO: Replace with callback-based writing using RecoveryCallback pattern
	// This batch writing approach uses problematic writeMainIndexWithVectorIO function
	// if err := dc.writeMainIndexWithVectorIO(emptySkiplist, tempIndexPath, MainContext); err != nil {
	//	os.Remove(tempIndexPath)
	//	return fmt.Errorf("failed to write empty index: %w", err)
	// }
	fmt.Fprintf(os.Stderr, "FATAL: Recovery main index writing not yet implemented with v0.7 callback architecture\n")
	fmt.Fprintf(os.Stderr, "TODO: Implement RecoveryCallback pattern for main index writing\n")
	os.Exit(1) //nolint:gocritic // exitAfterDefer: intentional fatal exit for unimplemented recovery path

	// Unreachable code after os.Exit(1) - commented out until RecoveryCallback implemented
	// Atomic replace main index
	// if err := os.Rename(tempIndexPath, dc.IndexFile); err != nil {
	//	os.Remove(tempIndexPath) // Cleanup on failure
	//	return fmt.Errorf("failed to replace main index: %w", err)
	// }

	// Remove cache file since we're starting fresh
	// os.Remove(dc.CacheFile) // Non-fatal if it fails

	// return nil
	return nil // Unreachable but required for Go compiler
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
	defer func() { _ = os.Remove(recoveryIndexPath) }() // Always cleanup recovery index

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
	currentSkiplist, err := dc.performScanToSkiplist(context.Background(), []string{}, recoverySkiplist)
	if err != nil {
		return fmt.Errorf("failed to scan current state for recovery: %w", err)
	}

	if verbosity >= 1 {
		VerboseLog(1, "Merged with current disk state, result has %d entries", currentSkiplist.Length())
	}

	// Write to both main and cache indices for complete recovery

	// 1. Write main index using vectorio (exclude deleted entries for main)
	tempMainPath := dc.generateTempFileName("main") //nolint:staticcheck // SA4006: used after os.Exit is removed when RecoveryCallback is implemented
	// TODO: Replace with callback-based writing using RecoveryCallback pattern
	// This batch writing approach uses problematic writeMainIndexWithVectorIO function
	// if err := dc.writeMainIndexWithVectorIO(currentSkiplist, tempMainPath, MainContext); err != nil {
	//	os.Remove(tempMainPath)
	//	return fmt.Errorf("failed to write recovery main index: %w", err)
	// }
	fmt.Fprintf(os.Stderr, "FATAL: Recovery main index writing not yet implemented with v0.7 callback architecture\n")
	fmt.Fprintf(os.Stderr, "TODO: Implement RecoveryCallback pattern for main index writing\n")
	os.Exit(1) //nolint:gocritic // exitAfterDefer: intentional fatal exit for unimplemented recovery path

	// 2. Write cache index using vectorio (include deleted entries for cache)
	tempCachePath := dc.generateTempFileName("cache") //nolint:staticcheck // SA4006: used after os.Exit is removed
	// TODO: Replace with callback-based writing using RecoveryCallback pattern
	// This batch writing approach uses problematic writeSkiplistWithVectorIOFiltered with go func loops
	fmt.Fprintf(os.Stderr, "FATAL: Recovery batch writing not yet implemented with v0.7 callback architecture\n")
	fmt.Fprintf(os.Stderr, "TODO: Implement RecoveryCallback pattern for cache index writing\n")
	os.Exit(1) //nolint:gocritic // exitAfterDefer: intentional fatal exit for unimplemented recovery path

	// Cleanup scan index file now that temp indices are written
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, but warn
		if verbosity >= 2 {
			VerboseLog(2, "Warning: failed to cleanup scan file: %v", err)
		}
	}

	// 3. Atomic replace main index first
	if err := os.Rename(tempMainPath, dc.IndexFile); err != nil {
		_ = os.Remove(tempMainPath) // Cleanup on failure
		_ = os.Remove(tempCachePath)
		return fmt.Errorf("failed to replace main index: %w", err)
	}

	// 4. Atomic replace cache index
	if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
		_ = os.Remove(tempCachePath) // Cleanup on failure
		return fmt.Errorf("failed to replace cache index: %w", err)
	}

	if verbosity >= 1 {
		VerboseLog(1, "Successfully recovered both main and cache indices from %s", indexPath)
	}

	return nil
}

// loadIndexWithCleanCopyingAndFixes loads an index file and creates clean copies with interactive fixing support
func (dc *DirectoryCache) loadIndexWithCleanCopyingAndFixes(indexPath, recoveryIndexPath string, fixMode FixMode, verbosity int) (*skiplistWrapper, error) {
	// Create enhanced validation config with fix mode support
	config := ValidationConfigWithFixes(ValidationRecovery, fixMode, verbosity, dc.RootDir)

	// Use the existing function as a base, but with enhanced validation
	return dc.loadIndexWithCleanCopyingEnhanced(indexPath, recoveryIndexPath, config)
}

// loadIndexWithCleanCopyingEnhanced is the core implementation with full fix support
func (dc *DirectoryCache) loadIndexWithCleanCopyingEnhanced(indexPath, recoveryIndexPath string, config ValidationConfig) (*skiplistWrapper, error) {
	// Open the corrupted index file
	file, err := os.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file %s: %w", indexPath, err)
	}
	defer func() { _ = file.Close() }()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < int64(V2HeaderSize) {
		return nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	// Memory map the file for reading
	data, err := unix.Mmap(int(file.Fd()), 0, int(stat.Size()), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("failed to mmap file: %w", err)
	}
	defer func() { _ = unix.Munmap(data) }()

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

	offset := 0
	entryData := data[headerSizeForVersion(header.Version):]
	hdrSize := headerSizeForVersion(header.Version)
	validEntryCount := 0
	fixesApplied := 0
	processor := ValidationProcessor(config)

	for i := uint32(0); i < header.EntryCount; i++ {
		if offset >= len(entryData) {
			if config.Verbosity >= 2 {
				VerboseLog(2, "Unexpected end of data at entry %d", i)
			}
			break
		}
		advance, fixed, inserted := dc.processRecoveryEntry(entryData, offset, i, config, processor, recoveryIndexPath, indexPath, hdrSize, skiplist)
		if fixed {
			fixesApplied++
		}
		if inserted {
			validEntryCount++
		}
		offset += advance
	}

	if config.Verbosity >= 1 {
		VerboseLog(1, "Enhanced recovery: processed %d valid entries from %d total, applied %d fixes",
			validEntryCount, header.EntryCount, fixesApplied)
	}

	return skiplist, nil
}

// processRecoveryEntry evaluates one entry during enhanced recovery.
// It returns how far to advance the offset in entryData, whether a
// fix was applied, and whether the (possibly-fixed) entry was
// inserted into the recovery skiplist.
func (dc *DirectoryCache) processRecoveryEntry(entryData []byte, offset int, i uint32, config ValidationConfig, processor EntryProcessor, recoveryIndexPath, indexPath string, hdrSize int, skiplist *skiplistWrapper) (advance int, fixed, inserted bool) {
	entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))
	entrySize := int(entry.Size)
	if entrySize <= 0 || entrySize > 4096 {
		if config.Verbosity >= 2 {
			VerboseLog(2, "Invalid entry size %d at entry %d, skipping", entrySize, i)
		}
		return 256, false, false // Conservative skip
	}

	entryCopy := make([]byte, entrySize)
	sourceBytes := (*[4096]byte)(unsafe.Pointer(entry))[:entrySize:entrySize]
	copy(entryCopy, sourceBytes)
	workingEntry := (*binaryEntry)(unsafe.Pointer(&entryCopy[0]))

	hadFixes, err := dc.applyFixesToEntry(workingEntry, i, config)
	if err != nil {
		if config.Verbosity >= 2 {
			VerboseLog(2, "Failed to apply fixes to entry %d: %v", i, err)
		}
		return entrySize, false, false
	}

	shouldInclude, err := processor(workingEntry, i, indexPath)
	if err != nil {
		if config.Verbosity >= 2 {
			VerboseLog(2, "Entry %d validation failed even after fixes: %v", i, err)
		}
		return entrySize, hadFixes, false
	}
	if !shouldInclude {
		return entrySize, hadFixes, false
	}

	_, cleanOffset, err := dc.appendRawEntryToScanIndex(recoveryIndexPath, entryCopy)
	if err != nil {
		if config.Verbosity >= 2 {
			VerboseLog(2, "Failed to create clean copy of entry %d: %v", i, err)
		}
		return entrySize, hadFixes, false
	}

	skiplist.Insert(binaryEntryRef{
		Offset: int(cleanOffset),
		IndexFile: &mmapIndexFile{
			Offset:     int(cleanOffset),
			Type:       "recovery",
			FilePath:   recoveryIndexPath,
			headerSize: hdrSize,
		},
	}, CacheContext)

	if config.Verbosity >= 3 {
		VerboseLog(3, "Successfully processed entry %d: %s", i, workingEntry.RelativePath())
	}
	return entrySize, hadFixes, true
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
	_, _ = fmt.Scanln(&response)

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

// appendRawEntryToScanIndex appends raw binaryEntry data to a scan index file
func (dc *DirectoryCache) appendRawEntryToScanIndex(scanIndexPath string, entryData []byte) (*binaryEntry, uint32, error) {
	// Open scan index file for append
	file, err := os.OpenFile(scanIndexPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open scan index for append: %w", err)
	}
	defer func() { _ = file.Close() }()

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
	defer func() { _ = unix.Munmap(data) }()

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
	if err := dc.createPreRecoverySnapshot(verbosity); err != nil {
		return fmt.Errorf("failed to create pre-recovery snapshot: %w", err)
	}

	if dc.anyIndexExists() {
		if dc.tryRecoveryStrategy(verbosity, "comprehensive recovery with state preservation",
			func() error { return dc.RecoverWithStatePreservation(verbosity) }) {
			return nil
		}
	}

	cacheExists := fileExists(dc.CacheFile)
	mainExists := fileExists(dc.IndexFile)

	if cacheExists && dc.tryRecoveryStrategy(verbosity, "recovery from cache index only",
		func() error { return dc.RecoverFromIndex(dc.CacheFile, verbosity) }) {
		return nil
	}
	if dc.tryRecoveryStrategy(verbosity, "recovery from scan files",
		func() error { return dc.RecoverFromScanFiles(verbosity) }) {
		return nil
	}
	if mainExists && dc.tryRecoveryStrategy(verbosity, "recovery from main index",
		func() error { return dc.RecoverFromIndex(dc.IndexFile, verbosity) }) {
		return nil
	}
	return fmt.Errorf("all recovery strategies failed")
}

// anyIndexExists reports whether at least one of the main/cache/scan
// index files is present on disk — the precondition for trying
// comprehensive state-preserving recovery.
func (dc *DirectoryCache) anyIndexExists() bool {
	if fileExists(dc.IndexFile) || fileExists(dc.CacheFile) {
		return true
	}
	scanFiles, err := dc.findScanIndexFiles()
	return err == nil && len(scanFiles) > 0
}

// tryRecoveryStrategy runs attempt, logging success or failure at the
// configured verbosity. Returns true iff attempt returned nil, so the
// caller can short-circuit the strategy chain.
func (dc *DirectoryCache) tryRecoveryStrategy(verbosity int, label string, attempt func() error) bool {
	if verbosity >= 1 {
		VerboseLog(1, "Attempting %s", label)
	}
	err := attempt()
	if err == nil {
		if verbosity >= 1 {
			VerboseLog(1, "Successfully completed %s", label)
		}
		return true
	}
	if verbosity >= 2 {
		VerboseLog(2, "%s failed: %v", label, err)
	}
	return false
}

// fileExists is a zero-fuss os.Stat wrapper for strategy gating.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// tryRecoverSource backs up a single source (main / cache / one scan
// file) and, if the load produces any entries, appends its skiplist
// to recovered. Returns the updated recovered/backups slices.
// backupKind drives the backup filename template; label is the
// human-readable source name used in verbose log output.
func (dc *DirectoryCache) tryRecoverSource(path, backupKind, label string, verbosity int, recovered []*skiplistWrapper, backups []string) ([]*skiplistWrapper, []string) {
	backupPath := dc.generateRecoveryBackupName(backupKind)
	if err := dc.createRecoveryBackup(path, backupPath, verbosity); err != nil {
		return recovered, backups
	}
	backups = append(backups, backupPath)

	skiplist, err := dc.loadIndexWithProcessor(path, RecoveryValidationProcessor(verbosity))
	if err != nil || skiplist.Length() == 0 {
		return recovered, backups
	}
	if verbosity >= 1 {
		VerboseLog(1, "Recovered %d entries from %s", skiplist.Length(), label)
	}
	return append(recovered, skiplist), backups
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

	if fileExists(dc.IndexFile) {
		recoveredSkiplists, backupPaths = dc.tryRecoverSource(dc.IndexFile, "main", "main index", verbosity, recoveredSkiplists, backupPaths)
	}
	if fileExists(dc.CacheFile) {
		recoveredSkiplists, backupPaths = dc.tryRecoverSource(dc.CacheFile, "cache", "cache index", verbosity, recoveredSkiplists, backupPaths)
	}
	if scanFiles, err := dc.findScanIndexFiles(); err == nil {
		for _, scanFile := range scanFiles {
			label := fmt.Sprintf("scan file %s", filepath.Base(scanFile.Path))
			recoveredSkiplists, backupPaths = dc.tryRecoverSource(scanFile.Path, "scan", label, verbosity, recoveredSkiplists, backupPaths)
		}
	}
	_ = backupPaths //nolint:staticcheck // SA4010: backupPaths accumulated for future recovery reporting

	if len(recoveredSkiplists) == 0 {
		return fmt.Errorf("no valid data could be recovered from any index files")
	}

	mergedSkiplist := recoveredSkiplists[0].Copy()
	for i := 1; i < len(recoveredSkiplists); i++ {
		if err := mergedSkiplist.Merge(recoveredSkiplists[i], MergeTheirs); err != nil && verbosity >= 2 {
			VerboseLog(2, "Warning: failed to merge skiplist %d: %v", i, err)
		}
	}

	if verbosity >= 1 {
		VerboseLog(1, "Merged recovery data: %d entries total", mergedSkiplist.Length())
	}

	// Step 5: Merge with current disk state via Hwang-Lin
	finalSkiplist, err := dc.performScanToSkiplist(context.Background(), []string{}, mergedSkiplist) //nolint:staticcheck // SA4006: used after os.Exit is removed
	if err != nil {
		return fmt.Errorf("failed to merge recovered data with current state: %w", err)
	}

	// Step 6: Write recovered cache index
	tempCachePath := dc.generateTempFileName("cache") //nolint:staticcheck // SA4006: used after os.Exit is removed
	// TODO: Replace with callback-based writing using RecoveryCallback pattern
	// This batch writing approach uses problematic writeSkiplistWithVectorIOFiltered with go func loops
	fmt.Fprintf(os.Stderr, "FATAL: Recovery batch writing not yet implemented with v0.7 callback architecture\n")
	fmt.Fprintf(os.Stderr, "TODO: Implement RecoveryCallback pattern for cache index writing\n")
	os.Exit(1) //nolint:gocritic // exitAfterDefer: intentional fatal exit for unimplemented recovery path

	// Step 7: Write recovered main index (excluding deleted)
	tempMainPath := dc.generateTempFileName("main") //nolint:staticcheck // SA4006: used after os.Exit is removed
	// TODO: Replace with callback-based writing using RecoveryCallback pattern
	// This batch writing approach uses problematic writeMainIndexWithVectorIO function
	// if err := dc.writeMainIndexWithVectorIO(finalSkiplist, tempMainPath, MainContext); err != nil {
	//	os.Remove(tempCachePath)
	//	os.Remove(tempMainPath)
	//	return fmt.Errorf("failed to write recovered main index: %w", err)
	// }
	_ = finalSkiplist //nolint:staticcheck // used after os.Exit is removed when RecoveryCallback is implemented
	_ = tempCachePath //nolint:staticcheck // used after os.Exit is removed when RecoveryCallback is implemented
	_ = tempMainPath  //nolint:staticcheck // used after os.Exit is removed when RecoveryCallback is implemented
	fmt.Fprintf(os.Stderr, "FATAL: Recovery main index writing not yet implemented with v0.7 callback architecture\n")
	fmt.Fprintf(os.Stderr, "TODO: Implement RecoveryCallback pattern for main index writing\n")
	os.Exit(1)

	// Unreachable code after os.Exit(1) - commented out until RecoveryCallback implemented
	// Step 8: Atomic replacement
	// Unreachable code after os.Exit(1) - commented out until RecoveryCallback implemented
	// if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
	//	os.Remove(tempCachePath)
	//	os.Remove(tempMainPath)
	//	return fmt.Errorf("failed to replace cache index: %w", err)
	// }

	// if err := os.Rename(tempMainPath, dc.IndexFile); err != nil {
	//	os.Remove(tempMainPath)
	//	return fmt.Errorf("failed to replace main index: %w", err)
	// }

	// Cleanup scan files after successful recovery
	// if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
	//	if verbosity >= 2 {
	//		VerboseLog(2, "Warning: failed to cleanup scan file: %v", err)
	//	}
	// }

	// if verbosity >= 1 {
	//	VerboseLog(1, "Recovery completed successfully. Backups created:")
	//	for _, backup := range backupPaths {
	//		VerboseLog(1, "  %s", backup)
	//	}
	//	VerboseLog(1, "Final result: %d entries in both main and cache indices", finalSkiplist.Length())
	// }

	// return nil
	return nil // Unreachable but required for Go compiler
}
