package dircachefilehash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotRepository_Initialise(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "dcfh-snapshot-init-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create snapshot repository
	sr := NewSnapshotRepository(tempDir)

	// Test initialization
	err = sr.Initialise()
	if err != nil {
		t.Fatalf("Failed to initialise snapshot repository: %v", err)
	}

	// Verify snapshots directory was created
	snapshotsDir := filepath.Join(tempDir, "snapshots")
	if _, err := os.Stat(snapshotsDir); os.IsNotExist(err) {
		t.Error("Snapshots directory was not created")
	}

	// Verify config file was created
	configPath := filepath.Join(snapshotsDir, "config")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file was not created")
	}

	// Verify config file content
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(configData, &config); err != nil {
		t.Fatalf("Failed to parse config JSON: %v", err)
	}

	if config["version"] != "1" {
		t.Errorf("Expected version '1', got '%v'", config["version"])
	}

	if config["id"] == nil || config["id"] == "" {
		t.Error("Config should have a repository ID")
	}
}

func TestSnapshotRepository_CreateSnapshot(t *testing.T) {
	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "dcfh-snapshot-create-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test index files
	testFiles := map[string]string{
		"main.idx":  "test main index content",
		"cache.idx": "test cache index content",
		"ignore":    "*.tmp\n",
	}

	for filename, content := range testFiles {
		filePath := filepath.Join(tempDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	// Create snapshot repository
	sr := NewSnapshotRepository(tempDir)

	// Create snapshot with tags
	tags := []string{"test", "backup"}
	metadata, err := sr.CreateSnapshot("/test/repo", tags)
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Verify metadata
	if metadata.ID == "" {
		t.Error("Snapshot ID should not be empty")
	}

	if !strings.HasSuffix(metadata.ID, "Z") {
		t.Error("Snapshot ID should be in ISO 8601 format ending with Z")
	}

	if len(metadata.Tags) != 2 || metadata.Tags[0] != "test" || metadata.Tags[1] != "backup" {
		t.Errorf("Expected tags ['test', 'backup'], got %v", metadata.Tags)
	}

	if metadata.Repository != "/test/repo" {
		t.Errorf("Expected repository '/test/repo', got '%s'", metadata.Repository)
	}

	if len(metadata.Files) != len(testFiles) {
		t.Errorf("Expected %d files in snapshot, got %d", len(testFiles), len(metadata.Files))
	}

	// Verify snapshot files were created
	snapshotDir := filepath.Join(tempDir, "snapshots", metadata.ID)
	for filename := range testFiles {
		snapshotFilePath := filepath.Join(snapshotDir, filename)
		if _, err := os.Stat(snapshotFilePath); os.IsNotExist(err) {
			t.Errorf("Snapshot file %s was not created", filename)
		}
	}

	// Verify metadata file
	metadataPath := filepath.Join(snapshotDir, "metadata.json")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Error("Metadata file was not created")
	}

	// Verify tags file
	tagsPath := filepath.Join(snapshotDir, "tags")
	if _, err := os.Stat(tagsPath); os.IsNotExist(err) {
		t.Error("Tags file was not created")
	}

	tagsContent, err := os.ReadFile(tagsPath)
	if err != nil {
		t.Fatalf("Failed to read tags file: %v", err)
	}

	expectedTags := "test\nbackup\n"
	if string(tagsContent) != expectedTags {
		t.Errorf("Expected tags content '%s', got '%s'", expectedTags, string(tagsContent))
	}
}

func TestSnapshotRepository_ListSnapshots(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "dcfh-snapshot-list-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test index file
	testFile := filepath.Join(tempDir, "main.idx")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	sr := NewSnapshotRepository(tempDir)

	// Test empty repository
	snapshots, err := sr.ListSnapshots()
	if err != nil {
		t.Fatalf("Failed to list snapshots from empty repository: %v", err)
	}
	if len(snapshots) != 0 {
		t.Errorf("Expected 0 snapshots in empty repository, got %d", len(snapshots))
	}

	// Create multiple snapshots
	snapshot1, err := sr.CreateSnapshot("/test/repo", []string{"tag1"})
	if err != nil {
		t.Fatalf("Failed to create first snapshot: %v", err)
	}

	// Sleep to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	snapshot2, err := sr.CreateSnapshot("/test/repo", []string{"tag2"})
	if err != nil {
		t.Fatalf("Failed to create second snapshot: %v", err)
	}

	// List snapshots
	snapshots, err = sr.ListSnapshots()
	if err != nil {
		t.Fatalf("Failed to list snapshots: %v", err)
	}

	if len(snapshots) != 2 {
		t.Errorf("Expected 2 snapshots, got %d", len(snapshots))
	}

	// Verify ordering (newest first)
	if snapshots[0].ID != snapshot2.ID {
		t.Error("Snapshots should be ordered newest first")
	}
	if snapshots[1].ID != snapshot1.ID {
		t.Error("Snapshots should be ordered newest first")
	}

	// Verify snapshot content
	if len(snapshots[0].Tags) != 1 || snapshots[0].Tags[0] != "tag2" {
		t.Errorf("Expected first snapshot to have tag 'tag2', got %v", snapshots[0].Tags)
	}
}

func TestRetentionPolicy_SelectSnapshotsToKeep(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "dcfh-retention-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test index file
	testFile := filepath.Join(tempDir, "main.idx")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	sr := NewSnapshotRepository(tempDir)

	// Create snapshots with different timestamps
	baseTime := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)
	var snapshots []*SnapshotMetadata

	// Create multiple snapshots per day for 3 days
	for day := 0; day < 3; day++ {
		for hour := 0; hour < 3; hour++ {
			snapshot := &SnapshotMetadata{
				ID:   baseTime.AddDate(0, 0, day).Add(time.Duration(hour) * time.Hour).Format("20060102T150405.000000000Z"),
				Time: baseTime.AddDate(0, 0, day).Add(time.Duration(hour) * time.Hour),
			}
			snapshots = append(snapshots, snapshot)
		}
	}

	// Test daily retention policy (keep 2 daily snapshots)
	policy := RetentionPolicy{
		Daily: 2,
	}

	toKeep := sr.selectSnapshotsToKeep(snapshots, policy)

	// Should keep latest snapshot from each of the 2 most recent days
	if len(toKeep) != 2 {
		t.Errorf("Expected to keep 2 snapshots with daily=2 policy, got %d", len(toKeep))
	}

	// Verify we kept the latest from each day
	expectedDays := []string{
		baseTime.AddDate(0, 0, 2).Format("2006-01-02"), // Most recent day
		baseTime.AddDate(0, 0, 1).Format("2006-01-02"), // Second most recent day
	}

	for _, expectedDay := range expectedDays {
		found := false
		for _, snapshot := range toKeep {
			if snapshot.Time.Format("2006-01-02") == expectedDay {
				// Should be the latest snapshot from that day (hour 14, which is 12+2)
				if snapshot.Time.Hour() != 14 {
					t.Errorf("Expected to keep latest snapshot from day %s (hour 14), got hour %d", expectedDay, snapshot.Time.Hour())
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to keep a snapshot from day %s", expectedDay)
		}
	}
}

func TestRetentionPolicy_ForgetSnapshots(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "dcfh-forget-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test index file
	testFile := filepath.Join(tempDir, "main.idx")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	sr := NewSnapshotRepository(tempDir)

	// Create multiple snapshots on different days by manipulating metadata
	var snapshotIDs []string
	baseTime := time.Now().UTC()
	for i := 0; i < 5; i++ {
		// Create snapshot
		snapshot, err := sr.CreateSnapshot("/test/repo", nil)
		if err != nil {
			t.Fatalf("Failed to create snapshot %d: %v", i, err)
		}
		snapshotIDs = append(snapshotIDs, snapshot.ID)

		// Modify the snapshot time to be on different days
		// Read and update metadata to simulate snapshots from different days
		snapshotDir := filepath.Join(tempDir, "snapshots", snapshot.ID)
		metadataPath := filepath.Join(snapshotDir, "metadata.json")

		// Read existing metadata
		metadataBytes, err := os.ReadFile(metadataPath)
		if err != nil {
			t.Fatalf("Failed to read metadata: %v", err)
		}

		var metadata SnapshotMetadata
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			t.Fatalf("Failed to parse metadata: %v", err)
		}

		// Update time to be on different days (going backwards in time)
		newTime := baseTime.AddDate(0, 0, -i)
		metadata.Time = newTime
		newID := generateSnapshotID(newTime)
		metadata.ID = newID

		// Write updated metadata
		updatedBytes, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			t.Fatalf("Failed to marshal updated metadata: %v", err)
		}

		if err := os.WriteFile(metadataPath, updatedBytes, 0644); err != nil {
			t.Fatalf("Failed to write updated metadata: %v", err)
		}

		// Rename snapshot directory to match new ID
		newSnapshotDir := filepath.Join(tempDir, "snapshots", newID)
		if err := os.Rename(snapshotDir, newSnapshotDir); err != nil {
			t.Fatalf("Failed to rename snapshot directory: %v", err)
		}

		// Update our tracking
		snapshotIDs[i] = newID
	}

	// Test dry-run mode (keep only 2 daily snapshots)
	policy := RetentionPolicy{Daily: 2}
	removed, err := sr.ForgetSnapshots(policy, true)
	if err != nil {
		t.Fatalf("Failed to run forget in dry-run mode: %v", err)
	}

	// Should report what would be removed but not actually remove
	if len(removed) == 0 {
		t.Error("Dry-run should report snapshots that would be removed")
	}

	// Verify snapshots still exist
	snapshots, err := sr.ListSnapshots()
	if err != nil {
		t.Fatalf("Failed to list snapshots after dry-run: %v", err)
	}
	if len(snapshots) != 5 {
		t.Errorf("Dry-run should not remove snapshots, expected 5, got %d", len(snapshots))
	}

	// Test actual removal
	removed, err = sr.ForgetSnapshots(policy, false)
	if err != nil {
		t.Fatalf("Failed to run forget: %v", err)
	}

	// Verify snapshots were actually removed
	snapshots, err = sr.ListSnapshots()
	if err != nil {
		t.Fatalf("Failed to list snapshots after forget: %v", err)
	}

	expectedRemaining := 2 // Should keep 2 daily snapshots (latest from 2 most recent days)
	if len(snapshots) != expectedRemaining {
		t.Errorf("Expected %d snapshots remaining, got %d", expectedRemaining, len(snapshots))
	}

	// Verify the correct snapshots were removed
	for _, removedID := range removed {
		for _, snapshot := range snapshots {
			if snapshot.ID == removedID {
				t.Errorf("Snapshot %s was reported as removed but still exists", removedID)
			}
		}
	}
}

func TestSnapshotID_Generation(t *testing.T) {
	testTime := time.Date(2023, 6, 15, 14, 30, 45, 123456789, time.UTC)
	id := generateSnapshotID(testTime)

	expected := "20230615T143045.123456789Z"
	if id != expected {
		t.Errorf("Expected snapshot ID '%s', got '%s'", expected, id)
	}

	// Verify it's a valid ISO 8601 format
	parsed, err := time.Parse("20060102T150405.000000000Z", id)
	if err != nil {
		t.Errorf("Generated snapshot ID is not valid ISO 8601 format: %v", err)
	}

	if !parsed.Equal(testTime) {
		t.Errorf("Parsed time doesn't match original: expected %v, got %v", testTime, parsed)
	}
}

func TestSnapshotGrouping_ByDay(t *testing.T) {
	sr := &SnapshotRepository{}

	// Create test snapshots from different days and times
	snapshots := []*SnapshotMetadata{
		{Time: time.Date(2023, 6, 15, 10, 0, 0, 0, time.UTC)},
		{Time: time.Date(2023, 6, 15, 14, 0, 0, 0, time.UTC)}, // Same day as above
		{Time: time.Date(2023, 6, 16, 9, 0, 0, 0, time.UTC)},  // Different day
		{Time: time.Date(2023, 6, 16, 18, 0, 0, 0, time.UTC)}, // Same day as above
	}

	groups := sr.groupSnapshotsByDay(snapshots)

	// Should have 2 groups (2 different days)
	if len(groups) != 2 {
		t.Errorf("Expected 2 day groups, got %d", len(groups))
	}

	// Check first day group
	day1 := "2023-06-15"
	if group, exists := groups[day1]; !exists {
		t.Errorf("Expected group for day %s", day1)
	} else if len(group) != 2 {
		t.Errorf("Expected 2 snapshots in day %s group, got %d", day1, len(group))
	}

	// Check second day group
	day2 := "2023-06-16"
	if group, exists := groups[day2]; !exists {
		t.Errorf("Expected group for day %s", day2)
	} else if len(group) != 2 {
		t.Errorf("Expected 2 snapshots in day %s group, got %d", day2, len(group))
	}
}

func TestSnapshotGrouping_ByWeek(t *testing.T) {
	sr := &SnapshotRepository{}

	// Create test snapshots from different weeks
	snapshots := []*SnapshotMetadata{
		{Time: time.Date(2023, 6, 12, 10, 0, 0, 0, time.UTC)}, // Week 24
		{Time: time.Date(2023, 6, 15, 14, 0, 0, 0, time.UTC)}, // Week 24
		{Time: time.Date(2023, 6, 19, 9, 0, 0, 0, time.UTC)},  // Week 25
		{Time: time.Date(2023, 6, 22, 18, 0, 0, 0, time.UTC)}, // Week 25
	}

	groups := sr.groupSnapshotsByWeek(snapshots)

	// Should have 2 groups (2 different weeks)
	if len(groups) != 2 {
		t.Errorf("Expected 2 week groups, got %d", len(groups))
	}

	// Verify week grouping logic
	for _, snapshot := range snapshots {
		year, week := snapshot.Time.ISOWeek()
		expectedKey := fmt.Sprintf("%d-W%02d", year, week)
		if group, exists := groups[expectedKey]; !exists {
			t.Errorf("Expected group for week %s", expectedKey)
		} else {
			found := false
			for _, s := range group {
				if s.Time.Equal(snapshot.Time) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Snapshot not found in expected week group %s", expectedKey)
			}
		}
	}
}

func TestSelectFromGroups_Ordering(t *testing.T) {
	sr := &SnapshotRepository{}

	// Create groups with multiple snapshots each
	groups := map[string][]*SnapshotMetadata{
		"2023-06-15": {
			{ID: "snapshot1", Time: time.Date(2023, 6, 15, 10, 0, 0, 0, time.UTC)},
			{ID: "snapshot2", Time: time.Date(2023, 6, 15, 14, 0, 0, 0, time.UTC)}, // Latest in group
		},
		"2023-06-16": {
			{ID: "snapshot3", Time: time.Date(2023, 6, 16, 9, 0, 0, 0, time.UTC)},
			{ID: "snapshot4", Time: time.Date(2023, 6, 16, 18, 0, 0, 0, time.UTC)}, // Latest in group
		},
		"2023-06-14": {
			{ID: "snapshot5", Time: time.Date(2023, 6, 14, 12, 0, 0, 0, time.UTC)},
		},
	}

	toKeep := make(map[string]*SnapshotMetadata)
	sr.selectFromGroups(groups, 2, toKeep)

	// Should keep 2 snapshots (limit=2)
	if len(toKeep) != 2 {
		t.Errorf("Expected to keep 2 snapshots, got %d", len(toKeep))
	}

	// Should keep latest from most recent groups (2023-06-16 and 2023-06-15)
	expectedIDs := map[string]bool{
		"snapshot4": true, // Latest from 2023-06-16
		"snapshot2": true, // Latest from 2023-06-15
	}

	for id := range toKeep {
		if !expectedIDs[id] {
			t.Errorf("Unexpected snapshot kept: %s", id)
		}
	}

	for expectedID := range expectedIDs {
		if _, found := toKeep[expectedID]; !found {
			t.Errorf("Expected snapshot %s to be kept", expectedID)
		}
	}
}

func TestSnapshotRepository_RemoveSnapshot(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "dcfh-snapshot-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create snapshot repository
	repo := NewSnapshotRepository(tempDir)
	err = repo.Initialise()
	if err != nil {
		t.Fatalf("Failed to initialise repository: %v", err)
	}

	// Create a test snapshot directory manually
	testSnapshotID := "20231201T120000.000000000Z"
	snapshotDir := filepath.Join(repo.SnapshotsDir, testSnapshotID)
	err = os.MkdirAll(snapshotDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test snapshot directory: %v", err)
	}

	// Add some test files to the snapshot
	testFile := filepath.Join(snapshotDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Verify snapshot directory exists before removal
	if _, err := os.Stat(snapshotDir); os.IsNotExist(err) {
		t.Fatalf("Test snapshot directory should exist before removal")
	}

	// Remove the snapshot
	err = repo.RemoveSnapshot(testSnapshotID)
	if err != nil {
		t.Fatalf("Failed to remove snapshot: %v", err)
	}

	// Verify snapshot directory is gone after removal
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Errorf("Snapshot directory should be removed after RemoveSnapshot")
	}
}

func TestSnapshotRepository_RemoveNonexistentSnapshot(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "dcfh-snapshot-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create snapshot repository
	repo := NewSnapshotRepository(tempDir)
	err = repo.Initialise()
	if err != nil {
		t.Fatalf("Failed to initialise repository: %v", err)
	}

	// Try to remove a nonexistent snapshot
	nonexistentID := "nonexistent-snapshot-id"
	err = repo.RemoveSnapshot(nonexistentID)

	// Should not return an error (os.RemoveAll doesn't fail on nonexistent paths)
	if err != nil {
		t.Errorf("RemoveSnapshot should not fail on nonexistent snapshot, got: %v", err)
	}
}
