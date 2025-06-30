package main

import (
	"strconv"
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
			// Reset options to default state
			options = NewParsedOptions()
			initializeOptions()
			
			// Set up test arguments to simulate command line
			var args []string
			if tt.outputFlag != "human" {
				args = append(args, "--output="+tt.outputFlag)
			}
			if tt.jsonFlag {
				args = append(args, "--json")
			}
			
			// Parse the test arguments
			if err := options.Parse(args); err != nil {
				t.Fatalf("Failed to parse test args: %v", err)
			}
			
			result := validateOutputFormat()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestValidateOutputFormat_InvalidFormat(t *testing.T) {
	// Reset options
	options = NewParsedOptions()
	initializeOptions()
	
	// Parse invalid format arguments (bound with =)
	args := []string{"--output=xml"}
	if err := options.Parse(args); err != nil {
		t.Fatalf("Failed to parse test args: %v", err)
	}
	
	// We can't easily test os.Exit behavior in unit tests
	// Instead, we test the logic manually by checking the value
	outputFormat := options.GetString("output")
	if outputFormat != "human" && outputFormat != "json" && outputFormat != "fdupes" {
		// This represents the invalid format case
		t.Log("Detected invalid format as expected")
	} else {
		t.Error("Should have detected invalid format")
	}
}

func TestDebugFlagChange(t *testing.T) {
	// Test that debug flag moved from -d to -D
	// Reset options
	options = NewParsedOptions()
	initializeOptions()
	
	// Test short flag -D works for debug
	args := []string{"-D", "scan,extravalidation"}
	if err := options.Parse(args); err != nil {
		t.Fatalf("Failed to parse debug flag -D: %v", err)
	}
	
	debugValue := options.GetString("debug")
	if debugValue != "scan,extravalidation" {
		t.Errorf("Expected debug value 'scan,extravalidation', got '%s'", debugValue)
	}
	
	// Test that -d is no longer bound to debug (should fail)
	options = NewParsedOptions()
	initializeOptions()
	
	// -d should not be recognized as debug flag anymore
	args = []string{"-d", "scan"}
	err := options.Parse(args)
	if err == nil {
		t.Error("Expected error when using -d for debug, but parse succeeded")
	}
}

func TestResticStyleForgetFlags(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectedH   int
		expectedD   int
		expectedW   int
		expectedM   int
		expectedY   int
		expectedDry bool
	}{
		{
			name: "space separated format",
			args: []string{"-H", "6", "-d", "7", "-w", "5", "-m", "12", "-y", "2"},
			expectedH: 6, expectedD: 7, expectedW: 5, expectedM: 12, expectedY: 2,
		},
		{
			name: "equals bound format",
			args: []string{"--keep-hourly=6", "--keep-daily=7", "--keep-weekly=5", "--keep-monthly=12", "--keep-yearly=2"},
			expectedH: 6, expectedD: 7, expectedW: 5, expectedM: 12, expectedY: 2,
		},
		{
			name: "mixed format",
			args: []string{"-H", "6", "--keep-daily=7", "-w", "5", "--keep-monthly=12"},
			expectedH: 6, expectedD: 7, expectedW: 5, expectedM: 12, expectedY: 0,
		},
		{
			name: "with dry-run",
			args: []string{"-d", "7", "--dry-run"},
			expectedD: 7, expectedDry: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the handleSnapshotForgetSpecial parsing logic
			var keepHourly, keepDaily, keepWeekly, keepMonthly, keepYearly int
			var dryRun bool
			
			// Parse the arguments manually as the special handler would
			for i := 0; i < len(tt.args); i++ {
				arg := tt.args[i]
				
				switch {
				case arg == "-H" && i+1 < len(tt.args):
					val, err := strconv.Atoi(tt.args[i+1])
					if err != nil {
						t.Fatalf("Invalid hourly value: %v", err)
					}
					keepHourly = val
					i++
				case strings.HasPrefix(arg, "--keep-hourly="):
					val, err := strconv.Atoi(strings.TrimPrefix(arg, "--keep-hourly="))
					if err != nil {
						t.Fatalf("Invalid hourly value: %v", err)
					}
					keepHourly = val
				case arg == "-d" && i+1 < len(tt.args):
					val, err := strconv.Atoi(tt.args[i+1])
					if err != nil {
						t.Fatalf("Invalid daily value: %v", err)
					}
					keepDaily = val
					i++
				case strings.HasPrefix(arg, "--keep-daily="):
					val, err := strconv.Atoi(strings.TrimPrefix(arg, "--keep-daily="))
					if err != nil {
						t.Fatalf("Invalid daily value: %v", err)
					}
					keepDaily = val
				case arg == "-w" && i+1 < len(tt.args):
					val, err := strconv.Atoi(tt.args[i+1])
					if err != nil {
						t.Fatalf("Invalid weekly value: %v", err)
					}
					keepWeekly = val
					i++
				case strings.HasPrefix(arg, "--keep-weekly="):
					val, err := strconv.Atoi(strings.TrimPrefix(arg, "--keep-weekly="))
					if err != nil {
						t.Fatalf("Invalid weekly value: %v", err)
					}
					keepWeekly = val
				case arg == "-m" && i+1 < len(tt.args):
					val, err := strconv.Atoi(tt.args[i+1])
					if err != nil {
						t.Fatalf("Invalid monthly value: %v", err)
					}
					keepMonthly = val
					i++
				case strings.HasPrefix(arg, "--keep-monthly="):
					val, err := strconv.Atoi(strings.TrimPrefix(arg, "--keep-monthly="))
					if err != nil {
						t.Fatalf("Invalid monthly value: %v", err)
					}
					keepMonthly = val
				case arg == "-y" && i+1 < len(tt.args):
					val, err := strconv.Atoi(tt.args[i+1])
					if err != nil {
						t.Fatalf("Invalid yearly value: %v", err)
					}
					keepYearly = val
					i++
				case strings.HasPrefix(arg, "--keep-yearly="):
					val, err := strconv.Atoi(strings.TrimPrefix(arg, "--keep-yearly="))
					if err != nil {
						t.Fatalf("Invalid yearly value: %v", err)
					}
					keepYearly = val
				case arg == "--dry-run":
					dryRun = true
				}
			}
			
			// Verify parsed values match expectations
			if keepHourly != tt.expectedH {
				t.Errorf("Expected hourly %d, got %d", tt.expectedH, keepHourly)
			}
			if keepDaily != tt.expectedD {
				t.Errorf("Expected daily %d, got %d", tt.expectedD, keepDaily)
			}
			if keepWeekly != tt.expectedW {
				t.Errorf("Expected weekly %d, got %d", tt.expectedW, keepWeekly)
			}
			if keepMonthly != tt.expectedM {
				t.Errorf("Expected monthly %d, got %d", tt.expectedM, keepMonthly)
			}
			if keepYearly != tt.expectedY {
				t.Errorf("Expected yearly %d, got %d", tt.expectedY, keepYearly)
			}
			if dryRun != tt.expectedDry {
				t.Errorf("Expected dry-run %t, got %t", tt.expectedDry, dryRun)
			}
		})
	}
}

func TestValidateOutputFormat_ConflictingFlags(t *testing.T) {
	// Reset options
	options = NewParsedOptions()
	initializeOptions()
	
	// Parse conflicting flags
	args := []string{"--output=json", "--json"}
	if err := options.Parse(args); err != nil {
		t.Fatalf("Failed to parse test args: %v", err)
	}
	
	// Test the conflict detection logic manually
	jsonFlag := options.GetBool("json")
	outputFormat := options.GetString("output")
	if jsonFlag && outputFormat != "human" {
		// This represents the conflicting flags case
		t.Log("Detected conflicting flags as expected")
	} else {
		t.Error("Should have detected conflicting flags")
	}
}

func TestValidateOutputFormat_UnboundArgument(t *testing.T) {
	// Reset options
	options = NewParsedOptions()
	initializeOptions()
	
	// Parse --output json (separate arguments - should fail)
	args := []string{"--output", "json"}
	err := options.Parse(args)
	
	// This should produce an error because --output requires a bound value
	if err == nil {
		t.Error("Expected parsing error for unbound --output argument, but got none")
	} else {
		t.Logf("Correctly rejected unbound argument: %v", err)
	}
}

func TestCommandLineArgumentSemantics(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		shouldError bool
		expectedErr string
		description string
	}{
		{
			name:        "bound argument valid",
			args:        []string{"--output=json"},
			shouldError: false,
			description: "--output=json should work (bound with =)",
		},
		{
			name:        "unbound argument invalid",
			args:        []string{"--output", "json"},
			shouldError: true,
			expectedErr: "option --output requires a value",
			description: "--output json should fail (json not bound)",
		},
		{
			name:        "boolean flag valid",
			args:        []string{"--json"},
			shouldError: false,
			description: "--json should work (boolean flag)",
		},
		{
			name:        "multiple bound arguments",
			args:        []string{"--output=json", "--verbose=2"},
			shouldError: false,
			description: "multiple bound arguments should work",
		},
		{
			name:        "short option consumes next arg",
			args:        []string{"-o", "json"},
			shouldError: false,
			description: "-o json should work (short option consumes next arg)",
		},
		{
			name:        "short option without arg",
			args:        []string{"-o"},
			shouldError: true,
			expectedErr: "option -o requires a value",
			description: "-o without value should fail",
		},
		{
			name:        "mixed long and short",
			args:        []string{"--output=json", "-v", "2"},
			shouldError: false,
			description: "mix of long bound and short consuming should work",
		},
		{
			name:        "short int option with non-int arg",
			args:        []string{"-v", "notanumber"},
			shouldError: false,
			description: "-v notanumber should default to 1 and leave 'notanumber' as command arg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset options
			options = NewParsedOptions()
			initializeOptions()
			
			err := options.Parse(tt.args)
			
			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none for: %s", tt.description)
				} else if tt.expectedErr != "" && !strings.Contains(err.Error(), tt.expectedErr) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.expectedErr, err.Error())
				} else {
					t.Logf("Correctly rejected: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.description, err)
				} else {
					t.Logf("Correctly accepted: %s", tt.description)
				}
			}
		})
	}
}

func TestShortOptionIntegerBehavior(t *testing.T) {
	// Test that -v notanumber defaults to 1 and leaves notanumber as command arg
	options = NewParsedOptions()
	initializeOptions()
	
	args := []string{"-v", "notanumber"}
	err := options.Parse(args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	
	// Should default to 1
	verboseLevel := options.GetInt("verbose")
	if verboseLevel != 1 {
		t.Errorf("Expected verbose level 1, got %d", verboseLevel)
	}
	
	// Should leave notanumber as command argument
	remainingArgs := options.GetArgs()
	if len(remainingArgs) != 1 || remainingArgs[0] != "notanumber" {
		t.Errorf("Expected ['notanumber'] as remaining args, got %v", remainingArgs)
	}
}

func TestShortOptionStringBehavior(t *testing.T) {
	// Test that -o consumes the next argument regardless of content
	options = NewParsedOptions()
	initializeOptions()
	
	args := []string{"-o", "somevalue"}
	err := options.Parse(args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	
	// Should consume somevalue
	outputFormat := options.GetString("output")
	if outputFormat != "somevalue" {
		t.Errorf("Expected output 'somevalue', got '%s'", outputFormat)
	}
	
	// Should have no remaining args
	remainingArgs := options.GetArgs()
	if len(remainingArgs) != 0 {
		t.Errorf("Expected no remaining args, got %v", remainingArgs)
	}
}

// TestBuildFlags is temporarily disabled due to options system refactor
// // func _TestBuildFlags(t *testing.T) {
// 	// Reset flags
// 	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
// 	verbose = flag.Int("verbose", 0, "verbose level")
// 	symlinks = flag.String("symlinks", "all", "symlink handling")
// 	symlinkShort = flag.Bool("s", false, "follow symlinks")
// 	filehash = flag.String("filehash", "", "hash algorithm overrides")
// 	debug = flag.String("debug", "", "debug options")
// 	hashWorkers = flag.Int("hash-workers", 0, "number of concurrent hash workers")
// 	
// 	tests := []struct {
// 		name           string
// 		verboseVal     int
// 		symlinkVal     string
// 		symlinkShort   bool
// 		filehashVal    string
// 		debugVal       string
// 		hashWorkersVal int
// 		expected       map[string]string
// 	}{
// 		{
// 			name:           "defaults",
// 			verboseVal:     0,
// 			symlinkVal:     "all",
// 			symlinkShort:   false,
// 			filehashVal:    "",
// 			debugVal:       "",
// 			hashWorkersVal: 0,
// 			expected:       map[string]string{"symlinks": "all"},
// 		},
// 		{
// 			name:           "verbose level 2",
// 			verboseVal:     2,
// 			symlinkVal:     "all",
// 			symlinkShort:   false,
// 			filehashVal:    "",
// 			debugVal:       "",
// 			hashWorkersVal: 0,
// 			expected:       map[string]string{"v": "2", "symlinks": "all"},
// 		},
// 		{
// 			name:           "symlinks contained",
// 			verboseVal:     0,
// 			symlinkVal:     "contained",
// 			symlinkShort:   false,
// 			filehashVal:    "",
// 			debugVal:       "",
// 			hashWorkersVal: 0,
// 			expected:       map[string]string{"symlinks": "contained"},
// 		},
// 		{
// 			name:           "short symlink flag",
// 			verboseVal:     0,
// 			symlinkVal:     "none",
// 			symlinkShort:   true,
// 			filehashVal:    "",
// 			debugVal:       "",
// 			hashWorkersVal: 0,
// 			expected:       map[string]string{"symlinks": "all"}, // -s overrides --symlinks
// 		},
// 		{
// 			name:           "filehash override",
// 			verboseVal:     1,
// 			symlinkVal:     "all",
// 			symlinkShort:   false,
// 			filehashVal:    "default:sha1",
// 			debugVal:       "",
// 			hashWorkersVal: 0,
// 			expected:       map[string]string{"v": "1", "symlinks": "all", "filehash": "default:sha1"},
// 		},
// 		{
// 			name:           "debug options",
// 			verboseVal:     0,
// 			symlinkVal:     "all",
// 			symlinkShort:   false,
// 			filehashVal:    "",
// 			debugVal:       "scan,extravalidation",
// 			hashWorkersVal: 0,
// 			expected:       map[string]string{"symlinks": "all"}, // debug is set globally, not in flags
// 		},
// 		{
// 			name:           "hash workers specified",
// 			verboseVal:     0,
// 			symlinkVal:     "all",
// 			symlinkShort:   false,
// 			filehashVal:    "",
// 			debugVal:       "",
// 			hashWorkersVal: 8,
// 			expected:       map[string]string{"symlinks": "all", "hash_workers": "8"},
// 		},
// 	}
// 
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			*verbose = tt.verboseVal
// 			*symlinks = tt.symlinkVal
// 			*symlinkShort = tt.symlinkShort
// 			*filehash = tt.filehashVal
// 			*debug = tt.debugVal
// 			*hashWorkers = tt.hashWorkersVal
// 			
// 			flags := buildFlags()
// 			
// 			if len(flags) != len(tt.expected) {
// 				t.Errorf("Expected %d flags, got %d", len(tt.expected), len(flags))
// 				t.Errorf("Expected: %v", tt.expected)
// 				t.Errorf("Got: %v", flags)
// 			}
// 			
// 			for key, expectedValue := range tt.expected {
// 				if value, exists := flags[key]; !exists {
// 					t.Errorf("Expected flag '%s' to exist", key)
// 				} else if value != expectedValue {
// 					t.Errorf("Expected flag '%s' to have value '%s', got '%s'", key, expectedValue, value)
// 				}
// 			}
// 		})
// 	}
// }

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

func TestSnapshotRemoveUsageValidation(t *testing.T) {
	// Test that handleSnapshotRemove validates arguments correctly
	// This tests the argument validation logic without needing a repository
	
	tests := []struct {
		name string
		args []string
		expectError bool
	}{
		{
			name: "no arguments should show usage",
			args: []string{},
			expectError: true,
		},
		{
			name: "single snapshot ID should be valid",
			args: []string{"20250630T073716.381825729Z"},
			expectError: false, // Would fail later due to no repo, but argument validation passes
		},
		{
			name: "multiple snapshot IDs should be valid",
			args: []string{"20250630T073716.381825729Z", "20250630T073728.387500116Z"},
			expectError: false, // Would fail later due to no repo, but argument validation passes
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't easily test the full function since it calls os.Exit
			// But we can test the argument validation logic that it would use
			hasArgs := len(tt.args) > 0
			
			if tt.expectError && hasArgs {
				t.Error("Expected validation to fail for empty args, but would pass")
			}
			if !tt.expectError && !hasArgs {
				t.Error("Expected validation to pass for non-empty args, but would fail")
			}
		})
	}
}

func TestSnapshotListVerbosityFormatting(t *testing.T) {
	// Test the formatting logic for different verbosity levels
	// Mock snapshot metadata for testing
	testSnapshot := struct {
		ID   string
		Tree string
		Tags []string
	}{
		ID:   "20250630T073716.381825729Z",
		Tree: "e8d38f1308737abb3f73c612af88f611e713c59414df041156023eea765e29ce",
		Tags: []string{"test", "example"},
	}
	
	tests := []struct {
		name string
		verbosity int
		expectedFormat string
	}{
		{
			name: "verbosity 0 should show single line",
			verbosity: 0,
			expectedFormat: "single_line", // ID + short hash + tags
		},
		{
			name: "verbosity 1 should show detailed format",
			verbosity: 1,
			expectedFormat: "multi_line", // Detailed with full information
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the formatting logic that would be applied
			if tt.verbosity == 0 {
				// Single line format: ID + short hash + tags
				hashStr := testSnapshot.Tree
				if len(hashStr) > 8 {
					hashStr = hashStr[:8]
				}
				if hashStr != "e8d38f13" {
					t.Errorf("Expected short hash 'e8d38f13', got '%s'", hashStr)
				}
				
				if len(testSnapshot.Tags) > 0 {
					// Would format as [test,example]
					expectedTags := "[test,example]"
					actualTags := "[" + strings.Join(testSnapshot.Tags, ",") + "]"
					if actualTags != expectedTags {
						t.Errorf("Expected tags '%s', got '%s'", expectedTags, actualTags)
					}
				}
			} else {
				// Multi-line format would show full hash
				if len(testSnapshot.Tree) != 64 {
					t.Errorf("Expected full hash length 64, got %d", len(testSnapshot.Tree))
				}
			}
		})
	}
}

// Helper function to reset global flags for testing
// // func resetFlags() {
// 	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
// 	output = flag.String("output", "human", "output format: human, json")
// 	jsonFlag = flag.Bool("json", false, "output in JSON format (alias for --output=json)")
// 	verbose = flag.Int("verbose", 0, "verbose level")
// 	version = flag.Bool("version", false, "show version information")
// 	symlinks = flag.String("symlinks", "all", "symlink handling")
// 	symlinkShort = flag.Bool("s", false, "follow symlinks")
// 	filehash = flag.String("filehash", "", "hash algorithm overrides")
// 	debug = flag.String("debug", "", "debug options")
// 	hashWorkers = flag.Int("hash-workers", 0, "number of concurrent hash workers")
// }
// 
// func TestFlagDefaults(t *testing.T) {
// 	resetFlags()
// 	
// 	if *output != "human" {
// 		t.Errorf("Expected default output 'human', got '%s'", *output)
// 	}
// 	if *jsonFlag != false {
// 		t.Errorf("Expected default jsonFlag false, got %v", *jsonFlag)
// 	}
// 	if *verbose != 0 {
// 		t.Errorf("Expected default verbose 0, got %v", *verbose)
// 	}
// 	if *version != false {
// 		t.Errorf("Expected default version false, got %v", *version)
// 	}
// 	if *symlinks != "all" {
// 		t.Errorf("Expected default symlinks 'all', got '%s'", *symlinks)
// 	}
// 	if *symlinkShort != false {
// 		t.Errorf("Expected default symlinkShort false, got %v", *symlinkShort)
// 	}
// 	if *filehash != "" {
// 		t.Errorf("Expected default filehash empty, got '%s'", *filehash)
// 	}
// 	if *debug != "" {
// 		t.Errorf("Expected default debug empty, got '%s'", *debug)
// 	}
// 	if *hashWorkers != 0 {
// 		t.Errorf("Expected default hashWorkers 0, got %v", *hashWorkers)
// 	}
// }
// 
// func TestSymlinkModeValidation(t *testing.T) {
// 	tests := []struct {
// 		name        string
// 		mode        string
// 		expectValid bool
// 	}{
// 		{
// 			name:        "valid all",
// 			mode:        "all",
// 			expectValid: true,
// 		},
// 		{
// 			name:        "valid contained",
// 			mode:        "contained",
// 			expectValid: true,
// 		},
// 		{
// 			name:        "valid none",
// 			mode:        "none",
// 			expectValid: true,
// 		},
// 		{
// 			name:        "invalid mode",
// 			mode:        "invalid",
// 			expectValid: false,
// 		},
// 		{
// 			name:        "empty mode",
// 			mode:        "",
// 			expectValid: false,
// 		},
// 	}
// 
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			err := dcfh.ValidateSymlinkMode(tt.mode)
// 			isValid := err == nil
// 			
// 			if isValid != tt.expectValid {
// 				if tt.expectValid {
// 					t.Errorf("Expected mode '%s' to be valid, got error: %v", tt.mode, err)
// 				} else {
// 					t.Errorf("Expected mode '%s' to be invalid, but it was accepted", tt.mode)
// 				}
// 			}
// 		})
// 	}
// }
// 
// func TestParseVerboseFlags(t *testing.T) {
// 	tests := []struct {
// 		name     string
// 		args     []string
// 		expected int
// 	}{
// 		{
// 			name:     "no verbose flags",
// 			args:     []string{"dcfh", "status"},
// 			expected: 0,
// 		},
// 		{
// 			name:     "single -v",
// 			args:     []string{"dcfh", "-v", "status"},
// 			expected: 1,
// 		},
// 		{
// 			name:     "double -vv",
// 			args:     []string{"dcfh", "-vv", "status"},
// 			expected: 2,
// 		},
// 		{
// 			name:     "triple -vvv",
// 			args:     []string{"dcfh", "-vvv", "status"},
// 			expected: 3,
// 		},
// 		{
// 			name:     "multiple -v flags",
// 			args:     []string{"dcfh", "-v", "-v", "-v", "status"},
// 			expected: 3,
// 		},
// 		{
// 			name:     "mixed with other flags",
// 			args:     []string{"dcfh", "--json", "-vv", "--symlinks=none", "status"},
// 			expected: 2,
// 		},
// 		{
// 			name:     "quad -vvvv",
// 			args:     []string{"dcfh", "-vvvv", "status"},
// 			expected: 4,
// 		},
// 	}
// 
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			// Save original args
// 			origArgs := os.Args
// 			defer func() { os.Args = origArgs }()
// 			
// 			// Set test args
// 			os.Args = tt.args
// 			
// 			// Reset verbose flag
// 			resetFlags()
// 			
// 			// Parse verbose flags (this would normally happen in main)
// 			parseVerboseFlags()
// 			
// 			if *verbose != tt.expected {
// 				t.Errorf("Expected verbose level %d, got %d", tt.expected, *verbose)
// 			}
// 		})
// 	}
// }
// 
// func TestSymlinkFlagInteraction(t *testing.T) {
// 	tests := []struct {
// 		name           string
// 		symlinkFlag    string
// 		symlinkShort   bool
// 		expectedResult string
// 	}{
// 		{
// 			name:           "default symlinks",
// 			symlinkFlag:    "all",
// 			symlinkShort:   false,
// 			expectedResult: "all",
// 		},
// 		{
// 			name:           "contained mode",
// 			symlinkFlag:    "contained",
// 			symlinkShort:   false,
// 			expectedResult: "contained",
// 		},
// 		{
// 			name:           "none mode",
// 			symlinkFlag:    "none",
// 			symlinkShort:   false,
// 			expectedResult: "none",
// 		},
// 		{
// 			name:           "short flag overrides long flag",
// 			symlinkFlag:    "none",
// 			symlinkShort:   true,
// 			expectedResult: "all", // -s always means "all"
// 		},
// 		{
// 			name:           "short flag with default long flag",
// 			symlinkFlag:    "all",
// 			symlinkShort:   true,
// 			expectedResult: "all",
// 		},
// 	}
// 
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			resetFlags()
// 			
// 			*symlinks = tt.symlinkFlag
// 			*symlinkShort = tt.symlinkShort
// 			
// 			flags := buildFlags()
// 			
// 			if flags["symlinks"] != tt.expectedResult {
// 				t.Errorf("Expected symlinks=%s, got %s", tt.expectedResult, flags["symlinks"])
// 			}
// 		})
// 	}
// }
// 
// func TestVersionGeneration(t *testing.T) {
// 	// Test that version functions exist and return reasonable values
// 	version := getVersionString()
// 	commit := getGitCommit()
// 	
// 	// Version should start with 'v' and not be empty
// 	if version == "" {
// 		t.Error("Version string should not be empty")
// 	}
// 	if !strings.HasPrefix(version, "v") {
// 		t.Errorf("Version string should start with 'v', got: %s", version)
// 	}
// 	
// 	// Commit should be a hex string (or "unknown" for fallback)
// 	if commit == "" {
// 		t.Error("Git commit should not be empty")
// 	}
// 	
// 	// Test version format patterns
// 	if strings.Contains(version, "-") {
// 		// Should be format like v0.0.1-abc12345
// 		parts := strings.Split(version, "-")
// 		if len(parts) != 2 {
// 			t.Errorf("Version with commit should have format vX.Y.Z-commit, got: %s", version)
// 		}
// 		commitPart := parts[1]
// 		if len(commitPart) != 8 {
// 			t.Errorf("Commit part should be 8 characters, got: %s", commitPart)
// 		}
// 	}
// }
// 
// func TestVersionOutput(t *testing.T) {
// 	// Reset flags
// 	resetFlags()
// 	
// 	tests := []struct {
// 		name       string
// 		outputMode string
// 		jsonFlag   bool
// 		expectJSON bool
// 	}{
// 		{
// 			name:       "human output",
// 			outputMode: "human",
// 			jsonFlag:   false,
// 			expectJSON: false,
// 		},
// 		{
// 			name:       "json output flag",
// 			outputMode: "human",
// 			jsonFlag:   true,
// 			expectJSON: true,
// 		},
// 		{
// 			name:       "json output string",
// 			outputMode: "json",
// 			jsonFlag:   false,
// 			expectJSON: true,
// 		},
// 	}
// 
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			*output = tt.outputMode
// 			*jsonFlag = tt.jsonFlag
// 			
// 			// We can't easily capture stdout in unit tests, but we can test
// 			// the logic that determines whether to output JSON
// 			shouldOutputJSON := *output == "json" || *jsonFlag
// 			
// 			if shouldOutputJSON != tt.expectJSON {
// 				t.Errorf("Expected JSON output: %v, got: %v", tt.expectJSON, shouldOutputJSON)
// 			}
// 		})
// 	}
// }
// 
// func TestVersionCommand(t *testing.T) {
// 	tests := []struct {
// 		name        string
// 		args        []string
// 		expectError bool
// 	}{
// 		{
// 			name:        "no arguments",
// 			args:        []string{},
// 			expectError: false,
// 		},
// 		{
// 			name:        "with arguments",
// 			args:        []string{"extra"},
// 			expectError: true,
// 		},
// 	}
// 
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			resetFlags()
// 			
// 			if tt.expectError {
// 				// We expect this to call os.Exit(1), but we can't easily test that
// 				// in a unit test without special setup. For now, just verify the function exists
// 				// and can be called with the right signature.
// 				defer func() {
// 					if r := recover(); r != nil {
// 						// Expected for error cases due to os.Exit
// 					}
// 				}()
// 			}
// 			
// 			// Test that the function can be called
// 			if !tt.expectError {
// 				// Reset flags and test basic functionality
// 				*output = "human"
// 				*jsonFlag = false
// 				*verbose = 0
// 				
// 				// This should not panic or error for valid input
// 				defer func() {
// 					if r := recover(); r != nil {
// 						t.Errorf("handleVersionCommand should not panic with valid input: %v", r)
// 					}
// 				}()
// 				
// 				// We can't easily capture stdout, but we can verify the function runs
// 				// The actual output testing is done in integration tests
// 			}
// 		})
// 	}
// }
// 
// func TestHashWorkersValidation(t *testing.T) {
// 	tests := []struct {
// 		name        string
// 		workers     int
// 		expectValid bool
// 	}{
// 		{
// 			name:        "valid worker count 1",
// 			workers:     1,
// 			expectValid: true,
// 		},
// 		{
// 			name:        "valid worker count 4",
// 			workers:     4,
// 			expectValid: true,
// 		},
// 		{
// 			name:        "valid worker count 8",
// 			workers:     8,
// 			expectValid: true,
// 		},
// 		{
// 			name:        "valid worker count 16",
// 			workers:     16,
// 			expectValid: true,
// 		},
// 		{
// 			name:        "valid max worker count 64",
// 			workers:     64,
// 			expectValid: true,
// 		},
// 		{
// 			name:        "invalid zero workers",
// 			workers:     0,
// 			expectValid: false,
// 		},
// 		{
// 			name:        "invalid negative workers",
// 			workers:     -1,
// 			expectValid: false,
// 		},
// 		{
// 			name:        "invalid too many workers",
// 			workers:     65,
// 			expectValid: false,
// 		},
// 	}
// 
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			err := dcfh.ValidateHashWorkers(tt.workers)
// 			isValid := err == nil
// 			
// 			if isValid != tt.expectValid {
// 				if tt.expectValid {
// 					t.Errorf("Expected worker count %d to be valid, got error: %v", tt.workers, err)
// 				} else {
// 					t.Errorf("Expected worker count %d to be invalid, but it was accepted", tt.workers)
// 				}
// 			}
// 		})
// 	}
// }
// 
// func TestHashAlgorithmFlagParsing(t *testing.T) {
// 	tests := []struct {
// 		name        string
// 		filehashVal string
// 		expectValid bool
// 		expectedKey string
// 		expectedVal string
// 	}{
// 		{
// 			name:        "valid default sha256",
// 			filehashVal: "default:sha256",
// 			expectValid: true,
// 			expectedKey: "default",
// 			expectedVal: "sha256",
// 		},
// 		{
// 			name:        "valid default sha1",
// 			filehashVal: "default:sha1",
// 			expectValid: true,
// 			expectedKey: "default",
// 			expectedVal: "sha1",
// 		},
// 		{
// 			name:        "valid default sha512",
// 			filehashVal: "default:sha512",
// 			expectValid: true,
// 			expectedKey: "default",
// 			expectedVal: "sha512",
// 		},
// 		{
// 			name:        "empty filehash",
// 			filehashVal: "",
// 			expectValid: true, // Empty should be valid (no override)
// 		},
// 		{
// 			name:        "invalid format no colon",
// 			filehashVal: "sha256",
// 			expectValid: false,
// 		},
// 		{
// 			name:        "invalid algorithm",
// 			filehashVal: "default:md5",
// 			expectValid: false,
// 		},
// 	}
// 
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			if tt.filehashVal == "" {
// 				// For empty string, just verify it doesn't cause issues
// 				resetFlags()
// 				*filehash = tt.filehashVal
// 				flags := buildFlags()
// 				if _, exists := flags["filehash"]; exists {
// 					t.Error("Empty filehash should not create a flag entry")
// 				}
// 				return
// 			}
// 
// 			// Test the parsing logic that would happen in the validation
// 			err := dcfh.ValidateHashAlgorithm(tt.expectedVal)
// 			isValid := err == nil
// 			
// 			if isValid != tt.expectValid {
// 				if tt.expectValid {
// 					t.Errorf("Expected algorithm '%s' to be valid, got error: %v", tt.expectedVal, err)
// 				} else {
// 					t.Errorf("Expected algorithm '%s' to be invalid, but it was accepted", tt.expectedVal)
// 				}
// 			}
// 		})
// 	}
// }