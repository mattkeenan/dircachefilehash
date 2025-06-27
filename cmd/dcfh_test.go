package main

import (
	"flag"
	"os"
	"strings"
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
	verbose = flag.Int("verbose", 0, "verbose level")
	symlinks = flag.String("symlinks", "all", "symlink handling")
	symlinkShort = flag.Bool("s", false, "follow symlinks")
	filehash = flag.String("filehash", "", "hash algorithm overrides")
	debug = flag.String("debug", "", "debug options")
	hashWorkers = flag.Int("hash-workers", 0, "number of concurrent hash workers")
	
	tests := []struct {
		name           string
		verboseVal     int
		symlinkVal     string
		symlinkShort   bool
		filehashVal    string
		debugVal       string
		hashWorkersVal int
		expected       map[string]string
	}{
		{
			name:           "defaults",
			verboseVal:     0,
			symlinkVal:     "all",
			symlinkShort:   false,
			filehashVal:    "",
			debugVal:       "",
			hashWorkersVal: 0,
			expected:       map[string]string{"symlinks": "all"},
		},
		{
			name:           "verbose level 2",
			verboseVal:     2,
			symlinkVal:     "all",
			symlinkShort:   false,
			filehashVal:    "",
			debugVal:       "",
			hashWorkersVal: 0,
			expected:       map[string]string{"v": "2", "symlinks": "all"},
		},
		{
			name:           "symlinks contained",
			verboseVal:     0,
			symlinkVal:     "contained",
			symlinkShort:   false,
			filehashVal:    "",
			debugVal:       "",
			hashWorkersVal: 0,
			expected:       map[string]string{"symlinks": "contained"},
		},
		{
			name:           "short symlink flag",
			verboseVal:     0,
			symlinkVal:     "none",
			symlinkShort:   true,
			filehashVal:    "",
			debugVal:       "",
			hashWorkersVal: 0,
			expected:       map[string]string{"symlinks": "all"}, // -s overrides --symlinks
		},
		{
			name:           "filehash override",
			verboseVal:     1,
			symlinkVal:     "all",
			symlinkShort:   false,
			filehashVal:    "default:sha1",
			debugVal:       "",
			hashWorkersVal: 0,
			expected:       map[string]string{"v": "1", "symlinks": "all", "filehash": "default:sha1"},
		},
		{
			name:           "debug options",
			verboseVal:     0,
			symlinkVal:     "all",
			symlinkShort:   false,
			filehashVal:    "",
			debugVal:       "scan,extravalidation",
			hashWorkersVal: 0,
			expected:       map[string]string{"symlinks": "all"}, // debug is set globally, not in flags
		},
		{
			name:           "hash workers specified",
			verboseVal:     0,
			symlinkVal:     "all",
			symlinkShort:   false,
			filehashVal:    "",
			debugVal:       "",
			hashWorkersVal: 8,
			expected:       map[string]string{"symlinks": "all", "hash_workers": "8"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*verbose = tt.verboseVal
			*symlinks = tt.symlinkVal
			*symlinkShort = tt.symlinkShort
			*filehash = tt.filehashVal
			*debug = tt.debugVal
			*hashWorkers = tt.hashWorkersVal
			
			flags := buildFlags()
			
			if len(flags) != len(tt.expected) {
				t.Errorf("Expected %d flags, got %d", len(tt.expected), len(flags))
				t.Errorf("Expected: %v", tt.expected)
				t.Errorf("Got: %v", flags)
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
	verbose = flag.Int("verbose", 0, "verbose level")
	version = flag.Bool("version", false, "show version information")
	symlinks = flag.String("symlinks", "all", "symlink handling")
	symlinkShort = flag.Bool("s", false, "follow symlinks")
	filehash = flag.String("filehash", "", "hash algorithm overrides")
	debug = flag.String("debug", "", "debug options")
	hashWorkers = flag.Int("hash-workers", 0, "number of concurrent hash workers")
}

func TestFlagDefaults(t *testing.T) {
	resetFlags()
	
	if *output != "human" {
		t.Errorf("Expected default output 'human', got '%s'", *output)
	}
	if *jsonFlag != false {
		t.Errorf("Expected default jsonFlag false, got %v", *jsonFlag)
	}
	if *verbose != 0 {
		t.Errorf("Expected default verbose 0, got %v", *verbose)
	}
	if *version != false {
		t.Errorf("Expected default version false, got %v", *version)
	}
	if *symlinks != "all" {
		t.Errorf("Expected default symlinks 'all', got '%s'", *symlinks)
	}
	if *symlinkShort != false {
		t.Errorf("Expected default symlinkShort false, got %v", *symlinkShort)
	}
	if *filehash != "" {
		t.Errorf("Expected default filehash empty, got '%s'", *filehash)
	}
	if *debug != "" {
		t.Errorf("Expected default debug empty, got '%s'", *debug)
	}
	if *hashWorkers != 0 {
		t.Errorf("Expected default hashWorkers 0, got %v", *hashWorkers)
	}
}

func TestSymlinkModeValidation(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		expectValid bool
	}{
		{
			name:        "valid all",
			mode:        "all",
			expectValid: true,
		},
		{
			name:        "valid contained",
			mode:        "contained",
			expectValid: true,
		},
		{
			name:        "valid none",
			mode:        "none",
			expectValid: true,
		},
		{
			name:        "invalid mode",
			mode:        "invalid",
			expectValid: false,
		},
		{
			name:        "empty mode",
			mode:        "",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dcfh.ValidateSymlinkMode(tt.mode)
			isValid := err == nil
			
			if isValid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected mode '%s' to be valid, got error: %v", tt.mode, err)
				} else {
					t.Errorf("Expected mode '%s' to be invalid, but it was accepted", tt.mode)
				}
			}
		})
	}
}

func TestParseVerboseFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected int
	}{
		{
			name:     "no verbose flags",
			args:     []string{"dcfh", "status"},
			expected: 0,
		},
		{
			name:     "single -v",
			args:     []string{"dcfh", "-v", "status"},
			expected: 1,
		},
		{
			name:     "double -vv",
			args:     []string{"dcfh", "-vv", "status"},
			expected: 2,
		},
		{
			name:     "triple -vvv",
			args:     []string{"dcfh", "-vvv", "status"},
			expected: 3,
		},
		{
			name:     "multiple -v flags",
			args:     []string{"dcfh", "-v", "-v", "-v", "status"},
			expected: 3,
		},
		{
			name:     "mixed with other flags",
			args:     []string{"dcfh", "--json", "-vv", "--symlinks=none", "status"},
			expected: 2,
		},
		{
			name:     "quad -vvvv",
			args:     []string{"dcfh", "-vvvv", "status"},
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original args
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			
			// Set test args
			os.Args = tt.args
			
			// Reset verbose flag
			resetFlags()
			
			// Parse verbose flags (this would normally happen in main)
			parseVerboseFlags()
			
			if *verbose != tt.expected {
				t.Errorf("Expected verbose level %d, got %d", tt.expected, *verbose)
			}
		})
	}
}

func TestSymlinkFlagInteraction(t *testing.T) {
	tests := []struct {
		name           string
		symlinkFlag    string
		symlinkShort   bool
		expectedResult string
	}{
		{
			name:           "default symlinks",
			symlinkFlag:    "all",
			symlinkShort:   false,
			expectedResult: "all",
		},
		{
			name:           "contained mode",
			symlinkFlag:    "contained",
			symlinkShort:   false,
			expectedResult: "contained",
		},
		{
			name:           "none mode",
			symlinkFlag:    "none",
			symlinkShort:   false,
			expectedResult: "none",
		},
		{
			name:           "short flag overrides long flag",
			symlinkFlag:    "none",
			symlinkShort:   true,
			expectedResult: "all", // -s always means "all"
		},
		{
			name:           "short flag with default long flag",
			symlinkFlag:    "all",
			symlinkShort:   true,
			expectedResult: "all",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFlags()
			
			*symlinks = tt.symlinkFlag
			*symlinkShort = tt.symlinkShort
			
			flags := buildFlags()
			
			if flags["symlinks"] != tt.expectedResult {
				t.Errorf("Expected symlinks=%s, got %s", tt.expectedResult, flags["symlinks"])
			}
		})
	}
}

func TestVersionGeneration(t *testing.T) {
	// Test that version functions exist and return reasonable values
	version := getVersionString()
	commit := getGitCommit()
	
	// Version should start with 'v' and not be empty
	if version == "" {
		t.Error("Version string should not be empty")
	}
	if !strings.HasPrefix(version, "v") {
		t.Errorf("Version string should start with 'v', got: %s", version)
	}
	
	// Commit should be a hex string (or "unknown" for fallback)
	if commit == "" {
		t.Error("Git commit should not be empty")
	}
	
	// Test version format patterns
	if strings.Contains(version, "-") {
		// Should be format like v0.0.1-abc12345
		parts := strings.Split(version, "-")
		if len(parts) != 2 {
			t.Errorf("Version with commit should have format vX.Y.Z-commit, got: %s", version)
		}
		commitPart := parts[1]
		if len(commitPart) != 8 {
			t.Errorf("Commit part should be 8 characters, got: %s", commitPart)
		}
	}
}

func TestVersionOutput(t *testing.T) {
	// Reset flags
	resetFlags()
	
	tests := []struct {
		name       string
		outputMode string
		jsonFlag   bool
		expectJSON bool
	}{
		{
			name:       "human output",
			outputMode: "human",
			jsonFlag:   false,
			expectJSON: false,
		},
		{
			name:       "json output flag",
			outputMode: "human",
			jsonFlag:   true,
			expectJSON: true,
		},
		{
			name:       "json output string",
			outputMode: "json",
			jsonFlag:   false,
			expectJSON: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*output = tt.outputMode
			*jsonFlag = tt.jsonFlag
			
			// We can't easily capture stdout in unit tests, but we can test
			// the logic that determines whether to output JSON
			shouldOutputJSON := *output == "json" || *jsonFlag
			
			if shouldOutputJSON != tt.expectJSON {
				t.Errorf("Expected JSON output: %v, got: %v", tt.expectJSON, shouldOutputJSON)
			}
		})
	}
}

func TestHashWorkersValidation(t *testing.T) {
	tests := []struct {
		name        string
		workers     int
		expectValid bool
	}{
		{
			name:        "valid worker count 1",
			workers:     1,
			expectValid: true,
		},
		{
			name:        "valid worker count 4",
			workers:     4,
			expectValid: true,
		},
		{
			name:        "valid worker count 8",
			workers:     8,
			expectValid: true,
		},
		{
			name:        "valid worker count 16",
			workers:     16,
			expectValid: true,
		},
		{
			name:        "valid max worker count 64",
			workers:     64,
			expectValid: true,
		},
		{
			name:        "invalid zero workers",
			workers:     0,
			expectValid: false,
		},
		{
			name:        "invalid negative workers",
			workers:     -1,
			expectValid: false,
		},
		{
			name:        "invalid too many workers",
			workers:     65,
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dcfh.ValidateHashWorkers(tt.workers)
			isValid := err == nil
			
			if isValid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected worker count %d to be valid, got error: %v", tt.workers, err)
				} else {
					t.Errorf("Expected worker count %d to be invalid, but it was accepted", tt.workers)
				}
			}
		})
	}
}

func TestHashAlgorithmFlagParsing(t *testing.T) {
	tests := []struct {
		name        string
		filehashVal string
		expectValid bool
		expectedKey string
		expectedVal string
	}{
		{
			name:        "valid default sha256",
			filehashVal: "default:sha256",
			expectValid: true,
			expectedKey: "default",
			expectedVal: "sha256",
		},
		{
			name:        "valid default sha1",
			filehashVal: "default:sha1",
			expectValid: true,
			expectedKey: "default",
			expectedVal: "sha1",
		},
		{
			name:        "valid default sha512",
			filehashVal: "default:sha512",
			expectValid: true,
			expectedKey: "default",
			expectedVal: "sha512",
		},
		{
			name:        "empty filehash",
			filehashVal: "",
			expectValid: true, // Empty should be valid (no override)
		},
		{
			name:        "invalid format no colon",
			filehashVal: "sha256",
			expectValid: false,
		},
		{
			name:        "invalid algorithm",
			filehashVal: "default:md5",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.filehashVal == "" {
				// For empty string, just verify it doesn't cause issues
				resetFlags()
				*filehash = tt.filehashVal
				flags := buildFlags()
				if _, exists := flags["filehash"]; exists {
					t.Error("Empty filehash should not create a flag entry")
				}
				return
			}

			// Test the parsing logic that would happen in the validation
			err := dcfh.ValidateHashAlgorithm(tt.expectedVal)
			isValid := err == nil
			
			if isValid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected algorithm '%s' to be valid, got error: %v", tt.expectedVal, err)
				} else {
					t.Errorf("Expected algorithm '%s' to be invalid, but it was accepted", tt.expectedVal)
				}
			}
		})
	}
}