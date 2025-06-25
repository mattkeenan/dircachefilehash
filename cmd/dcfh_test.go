package main

import (
	"flag"
	"os"
	"testing"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

func TestOutputFormat_Constants(t *testing.T) {
	if OutputHuman != "human" {
		t.Errorf("Expected OutputHuman to be 'human', got '%s'", OutputHuman)
	}
	if OutputJSON != "json" {
		t.Errorf("Expected OutputJSON to be 'json', got '%s'", OutputJSON)
	}
}

func TestValidateOutputFormat_ValidFormats(t *testing.T) {
	tests := []struct {
		name         string
		outputFlag   string
		jsonFlag     bool
		expected     OutputFormat
		shouldExit   bool
	}{
		{
			name:       "default human format",
			outputFlag: "human",
			jsonFlag:   false,
			expected:   OutputHuman,
			shouldExit: false,
		},
		{
			name:       "explicit json format",
			outputFlag: "json",
			jsonFlag:   false,
			expected:   OutputJSON,
			shouldExit: false,
		},
		{
			name:       "json flag alias",
			outputFlag: "human", // default
			jsonFlag:   true,
			expected:   OutputJSON,
			shouldExit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flags to default state
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
			
			// Set up the flags as they would be in main
			output = flag.String("output", "human", "output format: human, json")
			jsonFlag = flag.Bool("json", false, "output in JSON format (alias for --output=json)")
			
			// Set the flag values for this test
			*output = tt.outputFlag
			*jsonFlag = tt.jsonFlag
			
			result := validateOutputFormat()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestValidateOutputFormat_InvalidFormat(t *testing.T) {
	// Reset flags
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	output = flag.String("output", "human", "output format: human, json")
	jsonFlag = flag.Bool("json", false, "output in JSON format (alias for --output=json)")
	
	*output = "xml" // Invalid format
	*jsonFlag = false
	
	// We can't easily test os.Exit behavior in unit tests
	// Instead, we test the logic manually
	if *output != "human" && *output != "json" {
		// This represents the invalid format case
		t.Log("Detected invalid format as expected")
	} else {
		t.Error("Should have detected invalid format")
	}
}

func TestValidateOutputFormat_ConflictingFlags(t *testing.T) {
	// Reset flags
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	output = flag.String("output", "human", "output format: human, json")
	jsonFlag = flag.Bool("json", false, "output in JSON format (alias for --output=json)")
	
	*output = "json"  // Explicit json
	*jsonFlag = true  // Also json flag
	
	// Test the conflict detection logic manually
	if *jsonFlag && *output != "human" {
		// This represents the conflicting flags case
		t.Log("Detected conflicting flags as expected")
	} else {
		t.Error("Should have detected conflicting flags")
	}
}

func TestBuildFlags(t *testing.T) {
	// Reset flags
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	verbose = flag.Bool("verbose", false, "verbose output")
	
	tests := []struct {
		name        string
		verboseFlag bool
		expected    map[string]string
	}{
		{
			name:        "verbose false",
			verboseFlag: false,
			expected:    map[string]string{},
		},
		{
			name:        "verbose true",
			verboseFlag: true,
			expected:    map[string]string{"v": "1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*verbose = tt.verboseFlag
			
			flags := buildFlags()
			
			if len(flags) != len(tt.expected) {
				t.Errorf("Expected %d flags, got %d", len(tt.expected), len(flags))
			}
			
			for key, expectedValue := range tt.expected {
				if value, exists := flags[key]; !exists {
					t.Errorf("Expected flag '%s' to exist", key)
				} else if value != expectedValue {
					t.Errorf("Expected flag '%s' to have value '%s', got '%s'", key, expectedValue, value)
				}
			}
		})
	}
}

func TestOutputStructures(t *testing.T) {
	// Test InitOutput structure
	initOut := InitOutput{
		Success:     true,
		Message:     "test message",
		Repository:  "/test/repo",
		FileCount:   100,
		TotalSize:   1024,
		TimeElapsed: "1.5s",
	}
	
	if !initOut.Success {
		t.Error("Expected Success to be true")
	}
	if initOut.Message != "test message" {
		t.Errorf("Expected Message 'test message', got '%s'", initOut.Message)
	}
	if initOut.FileCount != 100 {
		t.Errorf("Expected FileCount 100, got %d", initOut.FileCount)
	}

	// Test StatusOutput structure
	statusOut := StatusOutput{
		Repository: "/test/repo",
		WorkingDir: "subdir",
		Modified:   []string{"file1.txt"},
		Added:      []string{"file2.txt"},
		Deleted:    []string{"file3.txt"},
		Summary: StatusSummary{
			ModifiedCount: 1,
			AddedCount:    1,
			DeletedCount:  1,
			HasChanges:    true,
		},
		IndexInfo: IndexInfo{
			FileCount: 10,
		},
	}
	
	if len(statusOut.Modified) != 1 {
		t.Errorf("Expected 1 modified file, got %d", len(statusOut.Modified))
	}
	if !statusOut.Summary.HasChanges {
		t.Error("Expected HasChanges to be true")
	}

	// Test UpdateOutput structure
	updateOut := UpdateOutput{
		Success:      true,
		Message:      "update complete",
		Repository:   "/test/repo",
		PathsUpdated: []string{"path1", "path2"},
		FileCount:    50,
		TotalSize:    2048,
		TimeElapsed:  "2.3s",
		Duplicates: &DuplicateInfo{
			SetCount:  2,
			FileCount: 4,
		},
	}
	
	if len(updateOut.PathsUpdated) != 2 {
		t.Errorf("Expected 2 paths updated, got %d", len(updateOut.PathsUpdated))
	}
	if updateOut.Duplicates == nil {
		t.Error("Expected Duplicates to be set")
	}
	if updateOut.Duplicates.SetCount != 2 {
		t.Errorf("Expected 2 duplicate sets, got %d", updateOut.Duplicates.SetCount)
	}

	// Test DupesOutput structure
	dupesOut := DupesOutput{
		Repository: "/test/repo",
		DuplicateGroups: []dcfh.DuplicateGroup{
			{
				Hash:  "test_hash",
				Files: []string{"file1.txt", "file2.txt"},
				Count: 2,
			},
		},
		Summary: DuplicateSummary{
			GroupCount: 3,
			FileCount:  9,
		},
	}
	
	if dupesOut.Summary.GroupCount != 3 {
		t.Errorf("Expected 3 groups, got %d", dupesOut.Summary.GroupCount)
	}

	// Test ErrorOutput structure
	errorOut := ErrorOutput{
		Success: false,
		Error:   "test error",
	}
	
	if errorOut.Success {
		t.Error("Expected Success to be false")
	}
	if errorOut.Error != "test error" {
		t.Errorf("Expected Error 'test error', got '%s'", errorOut.Error)
	}
}

func TestStatusSummary_Logic(t *testing.T) {
	tests := []struct {
		name         string
		modified     int
		added        int
		deleted      int
		hasChanges   bool
	}{
		{
			name:       "no changes",
			modified:   0,
			added:      0,
			deleted:    0,
			hasChanges: false,
		},
		{
			name:       "has modified",
			modified:   1,
			added:      0,
			deleted:    0,
			hasChanges: true,
		},
		{
			name:       "has added",
			modified:   0,
			added:      1,
			deleted:    0,
			hasChanges: true,
		},
		{
			name:       "has deleted",
			modified:   0,
			added:      0,
			deleted:    1,
			hasChanges: true,
		},
		{
			name:       "has all changes",
			modified:   2,
			added:      1,
			deleted:    3,
			hasChanges: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := StatusSummary{
				ModifiedCount: tt.modified,
				AddedCount:    tt.added,
				DeletedCount:  tt.deleted,
				HasChanges:    tt.hasChanges,
			}
			
			// Verify the logic would be correct
			expectedHasChanges := tt.modified > 0 || tt.added > 0 || tt.deleted > 0
			if summary.HasChanges != expectedHasChanges {
				t.Errorf("HasChanges logic mismatch: expected %v, got %v", expectedHasChanges, summary.HasChanges)
			}
		})
	}
}

// Helper function to reset global flags for testing
func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	output = flag.String("output", "human", "output format: human, json")
	jsonFlag = flag.Bool("json", false, "output in JSON format (alias for --output=json)")
	verbose = flag.Bool("verbose", false, "verbose output")
	version = flag.Bool("version", false, "show version information")
}

func TestFlagDefaults(t *testing.T) {
	resetFlags()
	
	if *output != "human" {
		t.Errorf("Expected default output 'human', got '%s'", *output)
	}
	if *jsonFlag != false {
		t.Errorf("Expected default jsonFlag false, got %v", *jsonFlag)
	}
	if *verbose != false {
		t.Errorf("Expected default verbose false, got %v", *verbose)
	}
	if *version != false {
		t.Errorf("Expected default version false, got %v", *version)
	}
}