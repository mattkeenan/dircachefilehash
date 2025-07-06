package dircachefilehash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SnapshotMetadata represents metadata for a snapshot
type SnapshotMetadata struct {
	ID         string            `json:"id"`   // ISO 8601 datetime string
	Time       time.Time         `json:"time"` // Parsed time for convenience
	Hostname   string            `json:"hostname,omitempty"`
	Username   string            `json:"username,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Tree       string            `json:"tree"`  // Hash of the tree structure
	Files      map[string]string `json:"files"` // filename -> hash mapping
	Summary    SnapshotSummary   `json:"summary"`
	Repository string            `json:"repository"` // Repository root path
}

// SnapshotSummary provides summary statistics for a snapshot
type SnapshotSummary struct {
	FilesCount   int   `json:"files_count"`
	TotalSize    int64 `json:"total_size"`
	MainEntries  int   `json:"main_entries,omitempty"`
	CacheEntries int   `json:"cache_entries,omitempty"`
	ScanFiles    int   `json:"scan_files,omitempty"`
}

// SnapshotRepository manages snapshot storage and operations
type SnapshotRepository struct {
	BasePath     string // .dcfh directory path
	SnapshotsDir string // .dcfh/snapshots directory (contains snapshot subdirectories directly)
}

// RetentionPolicy defines snapshot retention rules (restic-style)
type RetentionPolicy struct {
	Hourly  int `json:"hourly,omitempty"`
	Daily   int `json:"daily,omitempty"`
	Weekly  int `json:"weekly,omitempty"`
	Monthly int `json:"monthly,omitempty"`
	Yearly  int `json:"yearly,omitempty"`
}

// ComparisonDirection defines direction for snapshot comparison
type ComparisonDirection string

const (
	ComparisonForward  ComparisonDirection = "forward"  // from past to now
	ComparisonBackward ComparisonDirection = "backward" // from now to past
)

// SnapshotTarget represents a comparison target
type SnapshotTarget struct {
	Type string // "snapshot", "current", "main", "index"
	ID   string // snapshot ID (if Type == "snapshot")
}

// NewSnapshotRepository creates a new snapshot repository
func NewSnapshotRepository(dcfhDir string) *SnapshotRepository {
	snapshotsDir := filepath.Join(dcfhDir, "snapshots")
	VerboseLog(3, "NewSnapshotRepository: dcfhDir=%s, basePath=%s", dcfhDir, dcfhDir)
	return &SnapshotRepository{
		BasePath:     dcfhDir,
		SnapshotsDir: snapshotsDir,
	}
}

// Initialise creates the snapshot repository structure
func (sr *SnapshotRepository) Initialise() error {
	// Create snapshots directory
	if err := os.MkdirAll(sr.SnapshotsDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", sr.SnapshotsDir, err)
	}

	// Create config file if it doesn't exist
	configPath := filepath.Join(sr.SnapshotsDir, "config")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config := map[string]interface{}{
			"version": "1",
			"id":      generateRepositoryID(),
			"created": time.Now().UTC(),
		}

		configData, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}

		if err := os.WriteFile(configPath, configData, 0644); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
	}

	return nil
}

// CreateSnapshot creates a new snapshot by copying all .idx files
func (sr *SnapshotRepository) CreateSnapshot(repositoryRoot string, tags []string) (*SnapshotMetadata, error) {
	if err := sr.Initialise(); err != nil {
		return nil, fmt.Errorf("failed to initialise snapshot repository: %w", err)
	}

	// Generate snapshot ID using ISO 8601 datetime
	now := time.Now().UTC()
	snapshotID := generateSnapshotID(now)
	snapshotDir := filepath.Join(sr.SnapshotsDir, snapshotID)

	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	// Find all files to snapshot (.idx files and ignore file)
	filesToSnapshot, err := sr.findFilesToSnapshot()
	if err != nil {
		return nil, fmt.Errorf("failed to find files to snapshot: %w", err)
	}

	// Copy files and collect metadata
	files := make(map[string]string)
	var totalSize int64

	for _, file := range filesToSnapshot {
		// Copy file preserving metadata
		srcPath := filepath.Join(sr.BasePath, file)
		dstPath := filepath.Join(snapshotDir, file)

		VerboseLog(2, "Copying %s (%s)", file, formatSize(getFileSize(srcPath)))

		fileHash, size, err := sr.copyFileWithHash(srcPath, dstPath)
		if err != nil {
			return nil, fmt.Errorf("failed to copy %s: %w", file, err)
		}

		VerboseLog(3, "File %s: hash=%s, size=%d", file, fileHash, size)

		files[file] = fileHash
		totalSize += size
	}

	// Get hostname and username
	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}

	// Calculate tree hash
	treeHash := sr.calculateTreeHash(files)

	// Create metadata
	metadata := &SnapshotMetadata{
		ID:         snapshotID,
		Time:       now,
		Hostname:   hostname,
		Username:   username,
		Tags:       tags,
		Tree:       treeHash,
		Files:      files,
		Repository: repositoryRoot,
		Summary: SnapshotSummary{
			FilesCount: len(files),
			TotalSize:  totalSize,
		},
	}

	// Save tags to separate file if any tags provided
	if len(tags) > 0 {
		tagsPath := filepath.Join(snapshotDir, "tags")
		tagsContent := strings.Join(tags, "\n") + "\n"
		if err := os.WriteFile(tagsPath, []byte(tagsContent), 0644); err != nil {
			return nil, fmt.Errorf("failed to write tags file: %w", err)
		}
	}

	// Analyze snapshot content for detailed summary
	if err := sr.analyzeSnapshotContent(snapshotDir, &metadata.Summary); err != nil {
		// Non-fatal - just log and continue
		VerboseLog(3, "Warning: failed to analyze snapshot content: %v", err)
	}

	// Log level 2: Show total entries in all indices
	if metadata.Summary.MainEntries > 0 || metadata.Summary.CacheEntries > 0 {
		VerboseLog(2, "Index analysis: main.idx=%d entries, cache.idx=%d entries",
			metadata.Summary.MainEntries, metadata.Summary.CacheEntries)
	}

	// Save metadata
	metadataPath := filepath.Join(snapshotDir, "metadata.json")
	metadataData, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, metadataData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	return metadata, nil
}

// ListSnapshots returns all snapshots sorted by time (newest first)
func (sr *SnapshotRepository) ListSnapshots() ([]*SnapshotMetadata, error) {
	if _, err := os.Stat(sr.SnapshotsDir); os.IsNotExist(err) {
		return []*SnapshotMetadata{}, nil
	}

	entries, err := os.ReadDir(sr.SnapshotsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshots directory: %w", err)
	}

	var snapshots []*SnapshotMetadata
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "config" {
			continue
		}

		metadataPath := filepath.Join(sr.SnapshotsDir, entry.Name(), "metadata.json")
		metadata, err := sr.loadSnapshotMetadata(metadataPath)
		if err != nil {
			VerboseLog(1, "Warning: failed to load metadata for snapshot %s: %v", entry.Name(), err)
			continue
		}

		snapshots = append(snapshots, metadata)
	}

	// Sort by time (newest first)
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Time.After(snapshots[j].Time)
	})

	return snapshots, nil
}

// ForgetSnapshots removes old snapshots based on retention policy (restic-style)
func (sr *SnapshotRepository) ForgetSnapshots(policy RetentionPolicy, dryRun bool) ([]string, error) {
	snapshots, err := sr.ListSnapshots()
	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots: %w", err)
	}

	if len(snapshots) == 0 {
		return []string{}, nil
	}

	// Group snapshots by time period
	toKeep := sr.selectSnapshotsToKeep(snapshots, policy)
	toRemove := sr.findSnapshotsToRemove(snapshots, toKeep)

	var removed []string
	for _, snapshot := range toRemove {
		if dryRun {
			VerboseLog(1, "Would remove snapshot: %s", snapshot.ID)
		} else {
			VerboseLog(1, "Removing snapshot: %s", snapshot.ID)
			if err := sr.RemoveSnapshot(snapshot.ID); err != nil {
				VerboseLog(1, "Warning: failed to remove snapshot %s: %v", snapshot.ID, err)
				continue
			}
		}
		removed = append(removed, snapshot.ID)
	}

	return removed, nil
}

// selectSnapshotsToKeep determines which snapshots to keep based on retention policy
func (sr *SnapshotRepository) selectSnapshotsToKeep(snapshots []*SnapshotMetadata, policy RetentionPolicy) map[string]*SnapshotMetadata {
	toKeep := make(map[string]*SnapshotMetadata)

	// Group snapshots by time periods
	hourlyGroups := sr.groupSnapshotsByHour(snapshots)
	dailyGroups := sr.groupSnapshotsByDay(snapshots)
	weeklyGroups := sr.groupSnapshotsByWeek(snapshots)
	monthlyGroups := sr.groupSnapshotsByMonth(snapshots)
	yearlyGroups := sr.groupSnapshotsByYear(snapshots)

	// Keep the latest snapshot from each group, up to the policy limits
	sr.selectFromGroups(hourlyGroups, policy.Hourly, toKeep)
	sr.selectFromGroups(dailyGroups, policy.Daily, toKeep)
	sr.selectFromGroups(weeklyGroups, policy.Weekly, toKeep)
	sr.selectFromGroups(monthlyGroups, policy.Monthly, toKeep)
	sr.selectFromGroups(yearlyGroups, policy.Yearly, toKeep)

	return toKeep
}

// findSnapshotsToRemove finds snapshots that are not in the keep list
func (sr *SnapshotRepository) findSnapshotsToRemove(all []*SnapshotMetadata, toKeep map[string]*SnapshotMetadata) []*SnapshotMetadata {
	var toRemove []*SnapshotMetadata
	for _, snapshot := range all {
		if _, keep := toKeep[snapshot.ID]; !keep {
			toRemove = append(toRemove, snapshot)
		}
	}
	return toRemove
}

// removeSnapshot removes a snapshot directory and all its contents
func (sr *SnapshotRepository) RemoveSnapshot(snapshotID string) error {
	snapshotDir := filepath.Join(sr.SnapshotsDir, snapshotID)
	return os.RemoveAll(snapshotDir)
}

// Time grouping functions for retention policy

// groupSnapshotsByHour groups snapshots by hour (YYYY-MM-DD HH)
func (sr *SnapshotRepository) groupSnapshotsByHour(snapshots []*SnapshotMetadata) map[string][]*SnapshotMetadata {
	groups := make(map[string][]*SnapshotMetadata)
	for _, snapshot := range snapshots {
		key := snapshot.Time.Format("2006-01-02 15")
		groups[key] = append(groups[key], snapshot)
	}
	return groups
}

// groupSnapshotsByDay groups snapshots by day (YYYY-MM-DD)
func (sr *SnapshotRepository) groupSnapshotsByDay(snapshots []*SnapshotMetadata) map[string][]*SnapshotMetadata {
	groups := make(map[string][]*SnapshotMetadata)
	for _, snapshot := range snapshots {
		key := snapshot.Time.Format("2006-01-02")
		groups[key] = append(groups[key], snapshot)
	}
	return groups
}

// groupSnapshotsByWeek groups snapshots by week (YYYY-WW)
func (sr *SnapshotRepository) groupSnapshotsByWeek(snapshots []*SnapshotMetadata) map[string][]*SnapshotMetadata {
	groups := make(map[string][]*SnapshotMetadata)
	for _, snapshot := range snapshots {
		year, week := snapshot.Time.ISOWeek()
		key := fmt.Sprintf("%d-W%02d", year, week)
		groups[key] = append(groups[key], snapshot)
	}
	return groups
}

// groupSnapshotsByMonth groups snapshots by month (YYYY-MM)
func (sr *SnapshotRepository) groupSnapshotsByMonth(snapshots []*SnapshotMetadata) map[string][]*SnapshotMetadata {
	groups := make(map[string][]*SnapshotMetadata)
	for _, snapshot := range snapshots {
		key := snapshot.Time.Format("2006-01")
		groups[key] = append(groups[key], snapshot)
	}
	return groups
}

// groupSnapshotsByYear groups snapshots by year (YYYY)
func (sr *SnapshotRepository) groupSnapshotsByYear(snapshots []*SnapshotMetadata) map[string][]*SnapshotMetadata {
	groups := make(map[string][]*SnapshotMetadata)
	for _, snapshot := range snapshots {
		key := snapshot.Time.Format("2006")
		groups[key] = append(groups[key], snapshot)
	}
	return groups
}

// selectFromGroups selects the latest snapshot from each group, up to the limit
func (sr *SnapshotRepository) selectFromGroups(groups map[string][]*SnapshotMetadata, limit int, toKeep map[string]*SnapshotMetadata) {
	if limit <= 0 {
		return
	}

	// Get all group keys and sort them in reverse chronological order (newest first)
	var keys []string
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))

	// Select the latest snapshot from each group, up to the limit
	count := 0
	for _, key := range keys {
		if count >= limit {
			break
		}

		// Sort snapshots in this group by time (newest first)
		groupSnapshots := groups[key]
		sort.Slice(groupSnapshots, func(i, j int) bool {
			return groupSnapshots[i].Time.After(groupSnapshots[j].Time)
		})

		// Keep the latest snapshot from this group
		if len(groupSnapshots) > 0 {
			latest := groupSnapshots[0]
			toKeep[latest.ID] = latest
			count++
		}
	}
}

// Helper functions

func (sr *SnapshotRepository) findFilesToSnapshot() ([]string, error) {
	entries, err := os.ReadDir(sr.BasePath)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Include .idx files and ignore file
		if strings.HasSuffix(name, ".idx") || name == "ignore" {
			files = append(files, name)
		}
	}

	// Debug output
	VerboseLog(3, "Found %d files to snapshot in %s: %v", len(files), sr.BasePath, files)

	return files, nil
}

func (sr *SnapshotRepository) copyFileWithHash(src, dst string) (string, int64, error) {
	// Read source file
	data, err := os.ReadFile(src)
	if err != nil {
		return "", 0, err
	}

	// Calculate hash
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// Get source file info for metadata preservation
	srcInfo, err := os.Stat(src)
	if err != nil {
		return "", 0, err
	}

	// Write destination file
	if err := os.WriteFile(dst, data, srcInfo.Mode()); err != nil {
		return "", 0, err
	}

	// Preserve timestamps
	if err := os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		// Non-fatal
		VerboseLog(2, "Warning: failed to preserve timestamps for %s: %v", dst, err)
	}

	return hashStr, int64(len(data)), nil
}

func (sr *SnapshotRepository) calculateTreeHash(files map[string]string) string {
	// Create deterministic hash of file structure
	var items []string
	for filename, hash := range files {
		items = append(items, fmt.Sprintf("%s:%s", filename, hash))
	}
	sort.Strings(items)

	content := strings.Join(items, "\n")
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

func (sr *SnapshotRepository) analyzeSnapshotContent(snapshotDir string, summary *SnapshotSummary) error {
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".idx") {
			continue
		}

		indexPath := filepath.Join(snapshotDir, entry.Name())
		header, err := ValidateIndexHeader(indexPath, true, 0) // Validate version compatibility (current version is 0)
		if err != nil {
			VerboseLog(3, "Warning: failed to analyze %s: %v", entry.Name(), err)
			continue
		}

		entryCount := int(header.EntryCount)
		switch entry.Name() {
		case "main.idx":
			summary.MainEntries = entryCount
		case "cache.idx":
			summary.CacheEntries = entryCount
		default:
			if strings.HasPrefix(entry.Name(), "scan-") {
				summary.ScanFiles++
			}
		}

		VerboseLog(3, "Index %s: %d entries, version=%d, flags=0x%x",
			entry.Name(), entryCount, header.Version, header.Flags)
	}

	return nil
}

func (sr *SnapshotRepository) loadSnapshotMetadata(metadataPath string) (*SnapshotMetadata, error) {
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, err
	}

	var metadata SnapshotMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}

	// Load tags from tags file if it exists
	snapshotDir := filepath.Dir(metadataPath)
	tagsPath := filepath.Join(snapshotDir, "tags")
	if tagsData, err := os.ReadFile(tagsPath); err == nil {
		tagsContent := strings.TrimSpace(string(tagsData))
		if tagsContent != "" {
			metadata.Tags = strings.Split(tagsContent, "\n")
			// Clean up any empty lines
			var cleanTags []string
			for _, tag := range metadata.Tags {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					cleanTags = append(cleanTags, tag)
				}
			}
			metadata.Tags = cleanTags
		}
	}

	return &metadata, nil
}

func generateSnapshotID(t time.Time) string {
	// Generate snapshot ID using ISO 8601 format with nanoseconds
	// Format: 20060102T150405.000000000Z
	return t.Format("20060102T150405.000000000Z")
}

func generateRepositoryID() string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(hash[:16])
}

// getFileSize returns the size of a file
func getFileSize(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}

// formatSize formats a byte count as a human-readable string
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
