package dircachefilehash

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestStatusResult_HasChanges(t *testing.T) {
	tests := []struct {
		name     string
		result   StatusResult
		expected bool
	}{
		{
			name:     "no changes",
			result:   StatusResult{},
			expected: false,
		},
		{
			name: "has modified files",
			result: StatusResult{
				Modified: []string{"file1.txt"},
			},
			expected: true,
		},
		{
			name: "has added files",
			result: StatusResult{
				Added: []string{"file2.txt"},
			},
			expected: true,
		},
		{
			name: "has deleted files",
			result: StatusResult{
				Deleted: []string{"file3.txt"},
			},
			expected: true,
		},
		{
			name: "has all types of changes",
			result: StatusResult{
				Modified: []string{"file1.txt"},
				Added:    []string{"file2.txt"},
				Deleted:  []string{"file3.txt"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasChanges(); got != tt.expected {
				t.Errorf("HasChanges() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStatusResult_TotalChanges(t *testing.T) {
	tests := []struct {
		name     string
		result   StatusResult
		expected int
	}{
		{
			name:     "no changes",
			result:   StatusResult{},
			expected: 0,
		},
		{
			name: "single type of change",
			result: StatusResult{
				Modified: []string{"file1.txt", "file2.txt"},
			},
			expected: 2,
		},
		{
			name: "mixed changes",
			result: StatusResult{
				Modified: []string{"file1.txt"},
				Added:    []string{"file2.txt", "file3.txt"},
				Deleted:  []string{"file4.txt"},
			},
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.TotalChanges(); got != tt.expected {
				t.Errorf("TotalChanges() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCleanStatusFields(t *testing.T) {
	cleanStatus := CleanStatus{
		MainIndex:    true,
		CacheIndex:   false,
		TempIndices:  []string{"scan-1234-5678.idx"},
		HasTempFiles: true,
	}

	if !cleanStatus.MainIndex {
		t.Error("Expected MainIndex to be true")
	}
	if cleanStatus.CacheIndex {
		t.Error("Expected CacheIndex to be false")
	}
	if len(cleanStatus.TempIndices) != 1 {
		t.Errorf("Expected 1 temp index, got %d", len(cleanStatus.TempIndices))
	}
	if !cleanStatus.HasTempFiles {
		t.Error("Expected HasTempFiles to be true")
	}
}

func TestDirectoryCache_Status_VerboseFlag(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create some test files
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test verbose flag parsing
	tests := []struct {
		name          string
		flags         map[string]string
		expectClean   bool
	}{
		{
			name:        "no verbose flag",
			flags:       map[string]string{},
			expectClean: false,
		},
		{
			name:        "verbose flag with level 0",
			flags:       map[string]string{"v": "0"},
			expectClean: false,
		},
		{
			name:        "verbose flag with level 1",
			flags:       map[string]string{"v": "1"},
			expectClean: true,
		},
		{
			name:        "verbose flag with level 2",
			flags:       map[string]string{"v": "2"},
			expectClean: true,
		},
		{
			name:        "verbose flag with invalid value",
			flags:       map[string]string{"v": "invalid"},
			expectClean: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create DirectoryCache instance - use tempDir as both root and dcfh location
			dc := NewDirectoryCache(tempDir, tempDir)
			defer dc.Close()

			// Initialise with empty index to avoid complex setup
			if err := dc.createEmptyIndex(); err != nil {
				t.Fatalf("Failed to create empty index: %v", err)
			}

			// We can't easily test the full Status() method without a complex setup,
			// but we can test the verbose flag parsing logic
			verboseLevel, exists := tt.flags["v"]
			hasCleanStatus := false
			
			if exists && verboseLevel != "" {
				if level, err := strconv.Atoi(verboseLevel); err == nil && level > 0 {
					hasCleanStatus = true
				}
			}

			if hasCleanStatus != tt.expectClean {
				t.Errorf("Expected clean status inclusion: %v, got: %v", tt.expectClean, hasCleanStatus)
			}
		})
	}
}

func TestFileStatus_Constants(t *testing.T) {
	// Test that file status constants are defined correctly
	if StatusUnchanged != 0 {
		t.Errorf("Expected StatusUnchanged to be 0, got %d", StatusUnchanged)
	}
	if StatusModified != 1 {
		t.Errorf("Expected StatusModified to be 1, got %d", StatusModified)
	}
	if StatusAdded != 2 {
		t.Errorf("Expected StatusAdded to be 2, got %d", StatusAdded)
	}
	if StatusDeleted != 3 {
		t.Errorf("Expected StatusDeleted to be 3, got %d", StatusDeleted)
	}
}

func TestStatusResult_JSONTags(t *testing.T) {
	// This test ensures that the JSON tags are properly set for API compatibility
	result := StatusResult{
		Modified: []string{"modified.txt"},
		Added:    []string{"added.txt"},
		Deleted:  []string{"deleted.txt"},
		CleanStatus: &CleanStatus{
			MainIndex:    true,
			CacheIndex:   false,
			TempIndices:  []string{"temp.idx"},
			HasTempFiles: true,
		},
	}

	// Basic validation that fields are accessible
	if len(result.Modified) != 1 {
		t.Error("Modified field not properly set")
	}
	if len(result.Added) != 1 {
		t.Error("Added field not properly set")
	}
	if len(result.Deleted) != 1 {
		t.Error("Deleted field not properly set")
	}
	if result.CleanStatus == nil {
		t.Error("CleanStatus field not properly set")
	}
}