// Package dircachefilehash provides functionality to scan directories,
// hash file contents, and maintain a sorted index file for file integrity
// checking and change detection.
package dircachefilehash

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// FileEntry represents a file with its hash and metadata
// Fields are ordered to match git dircache index file format
type FileEntry struct {
	CTime        time.Time `json:"ctime"`         // Creation time (seconds since epoch)
	CTimeNano    int32     `json:"ctime_nano"`    // Creation time nanoseconds
	MTime        time.Time `json:"mtime"`         // Modification time (seconds since epoch)
	MTimeNano    int32     `json:"mtime_nano"`    // Modification time nanoseconds
	Dev          uint32    `json:"dev"`           // Device ID
	Ino          uint32    `json:"ino"`           // Inode number
	Mode         uint32    `json:"mode"`          // File mode
	UID          uint32    `json:"uid"`           // User ID
	GID          uint32    `json:"gid"`           // Group ID
	Size         uint32    `json:"size"`          // File size
	Hash         string    `json:"hash"`          // SHA-1 hash (40 hex chars)
	Flags        uint16    `json:"flags"`         // Index flags
	Path         string    `json:"path"`          // File path
	RelativePath string    `json:"relative_path"` // Relative path from root
}

// DirectoryCache manages the file cache for a directory
type DirectoryCache struct {
	RootDir   string
	IndexFile string
	entries   []FileEntry
}

// NewDirectoryCache creates a new directory cache instance
func NewDirectoryCache(rootDir, indexFile string) *DirectoryCache {
	return &DirectoryCache{
		RootDir:   rootDir,
		IndexFile: indexFile,
		entries:   make([]FileEntry, 0),
	}
}

// hashFile calculates SHA-1 hash of a file's contents (matching git)
func (dc *DirectoryCache) hashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to hash file %s: %w", filePath, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// ScanDirectory scans the directory and creates file entries with hashes
func (dc *DirectoryCache) ScanDirectory() error {
	dc.entries = make([]FileEntry, 0)

	err := filepath.Walk(dc.RootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and the index file itself
		if info.IsDir() || path == dc.IndexFile {
			return nil
		}

		// Calculate relative path from root directory
		relPath, err := filepath.Rel(dc.RootDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}

		// Hash the file contents
		hash, err := dc.hashFile(path)
		if err != nil {
			return fmt.Errorf("failed to hash file %s: %w", path, err)
		}

		// Get system-specific file information
		stat := info.Sys().(*syscall.Stat_t)

		entry := FileEntry{
			CTime:        time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec),
			CTimeNano:    int32(stat.Ctim.Nsec),
			MTime:        info.ModTime(),
			MTimeNano:    int32(info.ModTime().Nanosecond()),
			Dev:          uint32(stat.Dev),
			Ino:          uint32(stat.Ino),
			Mode:         uint32(info.Mode()),
			UID:          stat.Uid,
			GID:          stat.Gid,
			Size:         uint32(info.Size()),
			Hash:         hash,
			Flags:        uint16(len(relPath)), // Use path length as flags (git convention)
			Path:         path,
			RelativePath: relPath,
		}

		dc.entries = append(dc.entries, entry)
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to scan directory %s: %w", dc.RootDir, err)
	}

	// Sort entries by hash for byte comparison order
	sort.Slice(dc.entries, func(i, j int) bool {
		return dc.entries[i].Hash < dc.entries[j].Hash
	})

	return nil
}

// WriteIndex writes the sorted index to the specified file
func (dc *DirectoryCache) WriteIndex() error {
	file, err := os.Create(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to create index file %s: %w", dc.IndexFile, err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	// Write header
	fmt.Fprintf(writer, "# Directory Cache Index (Git dircache format)\n")
	fmt.Fprintf(writer, "# Generated: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(writer, "# Root Directory: %s\n", dc.RootDir)
	fmt.Fprintf(writer, "# Format: CTIME|CTIME_NANO|MTIME|MTIME_NANO|DEV|INO|MODE|UID|GID|SIZE|HASH|FLAGS|RELATIVE_PATH\n")
	fmt.Fprintf(writer, "#\n")

	// Write entries in sorted order
	for _, entry := range dc.entries {
		fmt.Fprintf(writer, "%d|%d|%d|%d|%d|%d|%o|%d|%d|%d|%s|%d|%s\n",
			entry.CTime.Unix(),
			entry.CTimeNano,
			entry.MTime.Unix(),
			entry.MTimeNano,
			entry.Dev,
			entry.Ino,
			entry.Mode,
			entry.UID,
			entry.GID,
			entry.Size,
			entry.Hash,
			entry.Flags,
			entry.RelativePath,
		)
	}

	return nil
}

// LoadIndex loads an existing index file
func (dc *DirectoryCache) LoadIndex() error {
	file, err := os.Open(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to open index file %s: %w", dc.IndexFile, err)
	}
	defer file.Close()

	dc.entries = make([]FileEntry, 0)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) != 13 {
			continue // Skip malformed lines
		}

		// Parse timestamps
		var ctimeUnix, mtimeUnix int64
		var ctimeNano, mtimeNano int32
		var dev, ino, mode, uid, gid, size uint32
		var flags uint16

		fmt.Sscanf(parts[0], "%d", &ctimeUnix)
		fmt.Sscanf(parts[1], "%d", &ctimeNano)
		fmt.Sscanf(parts[2], "%d", &mtimeUnix)
		fmt.Sscanf(parts[3], "%d", &mtimeNano)
		fmt.Sscanf(parts[4], "%d", &dev)
		fmt.Sscanf(parts[5], "%d", &ino)
		fmt.Sscanf(parts[6], "%o", &mode) // Octal for mode
		fmt.Sscanf(parts[7], "%d", &uid)
		fmt.Sscanf(parts[8], "%d", &gid)
		fmt.Sscanf(parts[9], "%d", &size)
		// parts[10] is hash
		fmt.Sscanf(parts[11], "%d", &flags)
		// parts[12] is relative path

		entry := FileEntry{
			CTime:        time.Unix(ctimeUnix, int64(ctimeNano)),
			CTimeNano:    ctimeNano,
			MTime:        time.Unix(mtimeUnix, int64(mtimeNano)),
			MTimeNano:    mtimeNano,
			Dev:          dev,
			Ino:          ino,
			Mode:         mode,
			UID:          uid,
			GID:          gid,
			Size:         size,
			Hash:         parts[10],
			Flags:        flags,
			Path:         filepath.Join(dc.RootDir, parts[12]),
			RelativePath: parts[12],
		}

		dc.entries = append(dc.entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading index file: %w", err)
	}

	return nil
}

// GetEntries returns a copy of the current entries
func (dc *DirectoryCache) GetEntries() []FileEntry {
	entries := make([]FileEntry, len(dc.entries))
	copy(entries, dc.entries)
	return entries
}

// FindByHash finds entries with the specified hash
func (dc *DirectoryCache) FindByHash(hash string) []FileEntry {
	var matches []FileEntry

	// Use binary search since entries are sorted by hash
	idx := sort.Search(len(dc.entries), func(i int) bool {
		return dc.entries[i].Hash >= hash
	})

	// Collect all entries with matching hash
	for i := idx; i < len(dc.entries) && dc.entries[i].Hash == hash; i++ {
		matches = append(matches, dc.entries[i])
	}

	return matches
}

// Update scans the directory and updates the index file
func (dc *DirectoryCache) Update() error {
	if err := dc.ScanDirectory(); err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
	}

	if err := dc.WriteIndex(); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	return nil
}

// Stats returns statistics about the cache
func (dc *DirectoryCache) Stats() (int, int64, error) {
	var totalSize int64
	for _, entry := range dc.entries {
		totalSize += int64(entry.Size)
	}
	return len(dc.entries), totalSize, nil
}

// FindDuplicates returns groups of files with identical hashes
func (dc *DirectoryCache) FindDuplicates() map[string][]FileEntry {
	duplicates := make(map[string][]FileEntry)

	for _, entry := range dc.entries {
		if _, exists := duplicates[entry.Hash]; !exists {
			duplicates[entry.Hash] = make([]FileEntry, 0)
		}
		duplicates[entry.Hash] = append(duplicates[entry.Hash], entry)
	}

	// Remove entries with only one file
	for hash, entries := range duplicates {
		if len(entries) <= 1 {
			delete(duplicates, hash)
		}
	}

	return duplicates
}
