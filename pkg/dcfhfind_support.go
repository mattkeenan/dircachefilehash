package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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