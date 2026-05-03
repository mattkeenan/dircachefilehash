package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// GenerateTimestampedFileName generates a timestamped filename using ISO 8601 format
func (ms *MetaStore) GenerateTimestampedFileName(prefix string) string {
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	return filepath.Join(ms.MetaDir,
		fmt.Sprintf("%s-%s.idx", prefix, timestamp))
}

// ScanForTimestampedCacheFiles finds all cache-{timestamp}.idx files in chronological order
func (ms *MetaStore) ScanForTimestampedCacheFiles() ([]string, error) {
	metaDir := ms.MetaDir
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read .dcfh directory: %w", err)
	}

	// Pattern to match cache-{timestamp}.idx files
	cachePattern := regexp.MustCompile(`^cache-(\d{8}T\d{6}Z)\.idx$`)

	var timestampedCaches []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		matches := cachePattern.FindStringSubmatch(entry.Name())
		if matches != nil {
			fullPath := filepath.Join(metaDir, entry.Name())
			timestampedCaches = append(timestampedCaches, fullPath)
		}
	}

	// Sort by filename (which sorts chronologically due to ISO 8601 format)
	sort.Strings(timestampedCaches)

	return timestampedCaches, nil
}

// CleanupTimestampedCacheFiles removes all timestamped cache files after successful operation
func (ms *MetaStore) CleanupTimestampedCacheFiles() error {
	timestampedCaches, err := ms.ScanForTimestampedCacheFiles()
	if err != nil {
		return fmt.Errorf("failed to scan for timestamped cache files: %w", err)
	}

	for _, cacheFile := range timestampedCaches {
		if err := os.Remove(cacheFile); err != nil && !os.IsNotExist(err) {
			// Log warning but continue with other files
			if IsDebugEnabled("scan") {
				fmt.Fprintf(os.Stderr, "[CLEANUP] Warning: failed to remove %s: %v\n", cacheFile, err)
			}
		} else if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[CLEANUP] Removed timestamped cache file: %s\n", cacheFile)
		}
	}

	return nil
}

// PathToSlug converts an absolute path to a kebab-case slug suitable for
// naming external .dcfh directories. Non-alphanumeric characters (including
// path separators) are replaced with hyphens, consecutive hyphens are
// collapsed, and leading/trailing hyphens are trimmed.
// Unicode letters and digits are preserved.
//
// Example: "/home/matt/some/dir" → "home-matt-some-dir"
func PathToSlug(path string) string {
	path = strings.ToLower(path)

	var b strings.Builder
	b.Grow(len(path))
	prevDash := false
	for _, r := range path {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}

	return strings.Trim(b.String(), "-")
}
