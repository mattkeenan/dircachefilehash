package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Dev       uint64
	MTimeWall uint64
	CTimeWall uint64
	HashStr   string
	HashType  uint16
}

// entryInfoAdapter wraps *EntryInfo to satisfy FilterEntry; the wrapper
// exists because EntryInfo's exported fields collide with the method
// names FilterEntry requires.
type entryInfoAdapter struct{ e *EntryInfo }

// AsFilterEntry returns a FilterEntry view of e for use with FilterExpr /
// FilterAction.
func (e *EntryInfo) AsFilterEntry() FilterEntry { return entryInfoAdapter{e} }

func (a entryInfoAdapter) RelativePath() (string, error) { return a.e.Path, nil }
func (a entryInfoAdapter) FileSize() (uint64, error)     { return a.e.FileSize, nil }
func (a entryInfoAdapter) Mode() (uint32, error)         { return a.e.Mode, nil }
func (a entryInfoAdapter) UID() (uint32, error)          { return a.e.UID, nil }
func (a entryInfoAdapter) GID() (uint32, error)          { return a.e.GID, nil }
func (a entryInfoAdapter) Dev() (uint64, error)          { return a.e.Dev, nil }
func (a entryInfoAdapter) MTimeWall() (uint64, error)    { return a.e.MTimeWall, nil }
func (a entryInfoAdapter) CTimeWall() (uint64, error)    { return a.e.CTimeWall, nil }
func (a entryInfoAdapter) HashType() (uint16, error)     { return a.e.HashType, nil }
func (a entryInfoAdapter) HashString() (string, error)   { return a.e.HashStr, nil }
func (a entryInfoAdapter) IsDeleted() (bool, error)      { return a.e.IsDeleted, nil }

// EntryCallback is called for each entry during index iteration
type EntryCallback func(entry *EntryInfo, indexType string) bool

// IterateIndexFile loads an index file and calls the callback for each entry
// This function is specifically provided for dcfhfind and similar tools.
// Unlike normal index loading, this bypasses NewMetaStore entirely to avoid
// .dcfh nesting checks and directory creation — it only needs read-only access.
// It also accepts any supported index version (v1 and v2) for cross-machine compatibility.
func IterateIndexFile(indexPath string, callback EntryCallback) error {
	skiplist, indexType, err := loadIndexFileForValidation(indexPath)
	if err != nil {
		return err
	}

	skiplist.ForEach(func(entry *binaryEntry, _ string) bool {
		return callback(entryToInfo(entry), indexType)
	})

	return nil
}

// FindEntries loads an index file and returns the entries matching paths,
// plus the input paths that weren't found (in their original form and order).
// Lookup is O(log n) per path via the skiplist, replacing the O(n)
// IterateIndexFile pattern when callers know up front which paths they want.
//
// Paths are normalised with filepath.Clean before lookup ("." → "" so callers
// can pass the repo root directly). Returned entries are in path-sorted order.
func FindEntries(indexPath string, paths []string) ([]*EntryInfo, []string, error) {
	skiplist, _, err := loadIndexFileForValidation(indexPath)
	if err != nil {
		return nil, nil, err
	}

	type hit struct {
		path  string
		entry *binaryEntry
	}
	hits := make([]hit, 0, len(paths))
	var notFound []string
	for _, p := range paths {
		key := filepath.Clean(p)
		if key == "." {
			key = ""
		}
		entry, _ := skiplist.Find(key)
		if entry == nil {
			notFound = append(notFound, p)
			continue
		}
		hits = append(hits, hit{path: key, entry: entry})
	}

	sort.Slice(hits, func(i, j int) bool { return hits[i].path < hits[j].path })
	found := make([]*EntryInfo, len(hits))
	for i, h := range hits {
		found[i] = entryToInfo(h.entry)
	}
	return found, notFound, nil
}

// loadIndexFileForValidation parses indexPath via the validation-friendly
// loader and returns a populated skiplist plus the index type. Shared by
// IterateIndexFile and FindEntries.
func loadIndexFileForValidation(indexPath string) (*skiplistWrapper, string, error) {
	ms := &MetaStore{
		signature: [4]byte{'d', 'c', 'f', 'h'},
		version:   0,
	}
	refs, err := ms.LoadIndexFromFileForValidation(indexPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load index: %w", err)
	}

	basename := filepath.Base(indexPath)
	ctx := ContextForIndexBasename(basename)
	skiplist := NewSkiplistWrapper(16, ctx)
	for _, ref := range refs {
		skiplist.Insert(ref, ctx)
	}
	return skiplist, IndexTypeForBasename(basename), nil
}

func entryToInfo(entry *binaryEntry) *EntryInfo {
	return &EntryInfo{
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
}

// FindRepositoryRootFrom discovers the repository root starting from a specific directory.
// If startDir is empty, uses current working directory.
// Handles both internal (.dcfh subdirectory) and external (*.dcfh) repos.
func FindRepositoryRootFrom(startDir string) (string, error) {
	rootDir, _, err := DiscoverRepository(startDir)
	return rootDir, err
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
	_, metaDir, err := DiscoverRepository("")
	if err != nil {
		return "", fmt.Errorf("not in a dcfh repository: %w", err)
	}

	switch indexSpec {
	case "main":
		return filepath.Join(metaDir, "main.idx"), nil
	case "cache":
		return filepath.Join(metaDir, "cache.idx"), nil
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
			scanPath := filepath.Join(metaDir, scanFile)
			if _, err := os.Stat(scanPath); err != nil {
				return "", fmt.Errorf("scan index file not found: %s", scanPath)
			}
			return scanPath, nil
		}

		// Try appending .idx if it doesn't have an extension
		if !strings.Contains(indexSpec, ".") {
			indexWithExt := indexSpec + ".idx"
			indexPath := filepath.Join(metaDir, indexWithExt)
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
		1: 40,  // SHA1 - 20 bytes * 2 hex chars
		2: 64,  // SHA256 - 32 bytes * 2 hex chars
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
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
				issues = append(issues, "hash contains non-hex characters")
				break
			}
		}
	}

	return len(issues) > 0, issues
}
