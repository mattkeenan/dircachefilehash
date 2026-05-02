package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"
)

// isValidHashType reports whether hashType matches one of the supported
// hash algorithms. Used by ValidationProcessor when checking entries
// recovered from potentially corrupted indices.
func isValidHashType(hashType uint16) bool {
	switch hashType {
	case HashTypeSHA1, HashTypeSHA256, HashTypeSHA512:
		return true
	default:
		return false
	}
}

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

// CreatePreRecoverySnapshotForIdxck creates a pre-recovery snapshot specifically for idxck operations
// This is a public wrapper that can be called from CLI code
func (dc *DirectoryCache) CreatePreRecoverySnapshotForIdxck(verbosity int) error {
	return dc.createPreRecoverySnapshot(verbosity)
}
