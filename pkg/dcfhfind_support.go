package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EntryInfo provides read-only access to index entry information for external tools
type EntryInfo struct {
	Path      string
	IsDeleted bool
	FileSize  uint64
	Mode      uint32
	UID       uint32
	GID       uint32
	Dev       uint32
	MTimeWall uint64
	CTimeWall uint64
	HashStr   string
	HashType  uint16
}

// EntryCallback is called for each entry during index iteration
type EntryCallback func(entry *EntryInfo, indexType string) bool

// IterateIndexFile loads an index file and calls the callback for each entry
// This function is specifically provided for dcfhfind and similar tools
func IterateIndexFile(indexPath string, callback EntryCallback) error {
	// Create a temporary DirectoryCache to use for loading
	tempDir := filepath.Dir(indexPath)
	dc := NewDirectoryCache(tempDir, "")
	
	// Load the index file into a skiplist
	refs, err := dc.LoadIndexFromFileForValidation(indexPath)
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}
	
	// Create skiplist and insert all entries
	skiplist := NewSkiplistWrapper(16, MainContext)
	for _, ref := range refs {
		skiplist.Insert(ref, MainContext)
	}

	// Determine index type from path
	indexType := "file"
	if basename := filepath.Base(indexPath); basename != "" {
		switch {
		case basename == "main.idx":
			indexType = "main"
		case basename == "cache.idx":
			indexType = "cache"
		case strings.HasPrefix(basename, "scan-") && strings.HasSuffix(basename, ".idx"):
			indexType = "scan"
		}
	}

	// Use ForEach to iterate through entries
	skiplist.ForEach(func(entry *binaryEntry, entryContext string) bool {
		// Convert internal binaryEntry to exported EntryInfo
		info := &EntryInfo{
			Path:      entry.RelativePath(),
			IsDeleted: entry.IsDeleted(),
			FileSize:  entry.FileSize,
			Mode:      entry.Mode,
			UID:       entry.UID,
			GID:       entry.GID,
			Dev:       entry.Dev,
			MTimeWall: entry.MTimeWall,
			CTimeWall: entry.CTimeWall,
			HashStr:   entry.HashString(),
			HashType:  entry.HashType,
		}
		
		// Call the user-provided callback
		return callback(info, indexType)
	})

	return nil
}

// FindRepositoryRootFrom discovers the repository root starting from a specific directory
// If startDir is empty, uses current working directory
func FindRepositoryRootFrom(startDir string) (string, error) {
	if startDir == "" {
		return repoDir()
	}

	// If startDir IS a .dcfh directory, return its parent as repo root
	if filepath.Base(startDir) == ".dcfh" {
		repoRoot := filepath.Dir(startDir)
		realDir, err := filepath.EvalSymlinks(repoRoot)
		if err != nil {
			// If symlink resolution fails, fall back to original path
			realDir = repoRoot
		}
		return realDir, nil
	}

	// Validate the specified directory has a .dcfh subdirectory
	dcfhPath := filepath.Join(startDir, ".dcfh")
	if info, err := os.Stat(dcfhPath); err != nil || !info.IsDir() {
		return "", fmt.Errorf("no dcfh repository found at %s", startDir)
	}

	// Resolve symlinks to get the real path (like the core function does)
	realDir, err := filepath.EvalSymlinks(startDir)
	if err != nil {
		// If symlink resolution fails, fall back to original path
		realDir = startDir
	}
	return realDir, nil
}

// ResolveIndexFile resolves an index specifier to an actual file path
// Supports index types: "main", "cache", "scan-PID-TID", or direct file paths
func ResolveIndexFile(indexSpec string) (string, error) {
	// If it's an absolute path or contains path separators, treat as direct file path
	if filepath.IsAbs(indexSpec) || strings.Contains(indexSpec, "/") || strings.Contains(indexSpec, "\\") {
		// Validate that the file exists
		if _, err := os.Stat(indexSpec); err != nil {
			return "", fmt.Errorf("index file not found: %s", indexSpec)
		}
		return indexSpec, nil
	}
	
	// Otherwise, discover repository and resolve index type
	repoRoot, err := FindRepositoryRootFrom("")
	if err != nil {
		return "", fmt.Errorf("not in a dcfh repository: %v", err)
	}
	
	dcfhDir := filepath.Join(repoRoot, ".dcfh")
	
	switch indexSpec {
	case "main":
		return filepath.Join(dcfhDir, "main.idx"), nil
	case "cache":
		return filepath.Join(dcfhDir, "cache.idx"), nil
	case "scan":
		// For scan, we'd need to handle multiple files - not supported yet
		return "", fmt.Errorf("scan index type not yet supported (use scan-PID-TID instead)")
	default:
		// Check if it's a specific scan index (scan-PID-TID pattern)
		if strings.HasPrefix(indexSpec, "scan-") {
			scanFile := indexSpec
			if !strings.HasSuffix(scanFile, ".idx") {
				scanFile += ".idx"
			}
			scanPath := filepath.Join(dcfhDir, scanFile)
			if _, err := os.Stat(scanPath); err != nil {
				return "", fmt.Errorf("scan index file not found: %s", scanPath)
			}
			return scanPath, nil
		}
		
		// Try appending .idx if it doesn't have an extension
		if !strings.Contains(indexSpec, ".") {
			indexWithExt := indexSpec + ".idx"
			indexPath := filepath.Join(dcfhDir, indexWithExt)
			if _, err := os.Stat(indexPath); err == nil {
				return indexPath, nil
			}
		}
		
		return "", fmt.Errorf("unknown index type: %s (use 'main', 'cache', 'scan-PID-TID', or full path)", indexSpec)
	}
}

// TimeFromWall converts wall time format back to time.Time
// This is an exported wrapper around the internal timeFromWall() function
func TimeFromWall(wall uint64) time.Time {
	return timeFromWall(wall)
}

// TimeToWall converts time.Time to wall time format
// This is an exported wrapper around the internal timeWall() function
func TimeToWall(t time.Time) uint64 {
	return timeWall(t)
}

// ValidateEntryInfo performs comprehensive validation of an entry
// Returns true if the entry is valid, false if invalid, and error if validation fails
func ValidateEntryInfo(entry *EntryInfo, repoPath string) (bool, error) {
	// Basic structural validation
	if entry.Path == "" {
		return false, nil
	}
	
	if entry.HashStr == "" {
		return false, nil
	}
	
	// Validate hash type
	if entry.HashType == 0 || entry.HashType > 3 {
		return false, nil
	}
	
	// Check hash string length based on type
	expectedLength := map[uint16]int{
		1: 40, // SHA1 - 20 bytes * 2 hex chars
		2: 64, // SHA256 - 32 bytes * 2 hex chars  
		3: 128, // SHA512 - 64 bytes * 2 hex chars
	}
	
	if expected, ok := expectedLength[entry.HashType]; ok {
		if len(entry.HashStr) != expected {
			return false, nil
		}
	}
	
	// Validate file size is reasonable (less than 4 exabytes)
	if entry.FileSize > (1 << 62) {
		return false, nil
	}
	
	return true, nil
}

// VerifyEntryChecksum calculates and compares file hash against stored value
// Returns true if hashes match, false if they don't, and error if verification fails
func VerifyEntryChecksum(entry *EntryInfo, repoPath string) (bool, error) {
	filePath := filepath.Join(repoPath, entry.Path)
	
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return false, fmt.Errorf("file does not exist")
	} else if err != nil {
		return false, fmt.Errorf("stat error: %w", err)
	}
	
	// Get hash algorithm
	algorithm, err := GetHashAlgorithmByType(entry.HashType)
	if err != nil {
		return false, fmt.Errorf("invalid hash type %d: %w", entry.HashType, err)
	}
	
	// Calculate current file hash
	currentHash, err := HashFileToHexString(filePath, algorithm)
	if err != nil {
		return false, fmt.Errorf("failed to calculate hash: %w", err)
	}
	
	// Compare hashes (case-insensitive)
	return strings.EqualFold(currentHash, entry.HashStr), nil
}

// DetectEntryCorruption checks for corruption indicators in an entry
// Returns true if corruption is detected, and a list of corruption issues found
func DetectEntryCorruption(entry *EntryInfo) (bool, []string) {
	var issues []string
	
	// Check for all-zero hash (common corruption indicator)
	if entry.HashStr == strings.Repeat("0", len(entry.HashStr)) {
		issues = append(issues, "all-zero hash")
	}
	
	// Check for invalid hash type
	if entry.HashType == 0 || entry.HashType > 3 {
		issues = append(issues, fmt.Sprintf("invalid hash type: %d", entry.HashType))
	}
	
	// Check for unreasonable file size (>4 exabytes)
	if entry.FileSize > (1 << 62) {
		issues = append(issues, fmt.Sprintf("unreasonable file size: %d bytes", entry.FileSize))
	}
	
	// Check for empty path
	if entry.Path == "" {
		issues = append(issues, "empty file path")
	}
	
	// Check for empty hash
	if entry.HashStr == "" {
		issues = append(issues, "empty hash")
	}
	
	// Check hash string contains only hex characters
	if entry.HashStr != "" {
		for _, r := range entry.HashStr {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				issues = append(issues, "hash contains non-hex characters")
				break
			}
		}
	}
	
	return len(issues) > 0, issues
}