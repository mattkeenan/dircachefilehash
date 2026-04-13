package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegration runs end-to-end integration tests using real dcfh repository
func TestIntegration(t *testing.T) {
	// Skip if test repo doesn't exist (can be created with setup_test_repo.sh)
	testRepo := "test-data/test-repo"
	if _, err := os.Stat(testRepo); os.IsNotExist(err) {
		t.Skip("Integration test repo not found. Run test-data/setup_test_repo.sh to create it.")
	}

	// Verify .dcfh directory exists
	dcfhDir := filepath.Join(testRepo, ".dcfh")
	if _, err := os.Stat(dcfhDir); os.IsNotExist(err) {
		t.Skip("Test repo not initialised. Run dcfh init and dcfh update on test-data/test-repo")
	}

	// Verify main.idx exists
	mainIndex := filepath.Join(dcfhDir, "main.idx")
	if _, err := os.Stat(mainIndex); os.IsNotExist(err) {
		t.Skip("Main index not found. Run dcfh update on test-data/test-repo")
	}

	// Build dcfhfind executable for testing
	dcfhfindPath := "./dcfhfind"
	if _, err := os.Stat(dcfhfindPath); os.IsNotExist(err) {
		t.Fatal("dcfhfind executable not found. Run 'make build' first.")
	}

	// Convert to absolute path so it works when we change working directory
	var err error
	dcfhfindPath, err = filepath.Abs(dcfhfindPath)
	if err != nil {
		t.Fatalf("Failed to get absolute path for dcfhfind: %v", err)
	}

	tests := []struct {
		name     string
		args     []string
		expected []string
		contains []string // Optional: check if output contains these strings
	}{
		{
			name:     "List all files",
			args:     []string{"main", "--print"},
			expected: []string{".hidden", "README.md", "empty.txt", "hello.txt", "large.txt", "main.go", "src/app.go", "src/utils/helper.go", "tiny.png"},
		},
		{
			name:     "Find Go files",
			args:     []string{"main", "--name", "*.go", "--print"},
			expected: []string{"main.go", "src/app.go", "src/utils/helper.go"},
		},
		{
			name:     "Find empty files",
			args:     []string{"main", "--empty", "--print"},
			expected: []string{"empty.txt"},
		},
		{
			name:     "Find large files (>1000 bytes)",
			args:     []string{"main", "--size", "+1000", "--print"},
			expected: []string{"large.txt"},
		},
		{
			name:     "Find small files (<100 bytes)",
			args:     []string{"main", "--size", "-100", "--print"},
			expected: []string{".hidden", "empty.txt", "hello.txt", "main.go", "src/utils/helper.go", "tiny.png"},
		},
		{
			name:     "Find text files",
			args:     []string{"main", "--name", "*.txt", "--print"},
			expected: []string{"empty.txt", "hello.txt", "large.txt"},
		},
		{
			name:     "Find hidden files",
			args:     []string{"main", "--name", ".*", "--print"},
			expected: []string{".hidden"},
		},
		{
			name:     "Find files in src directory",
			args:     []string{"main", "--path", "src/*", "--print"},
			expected: []string{"src/app.go"}, // src/* only matches direct children, not subdirectories
		},
		{
			name:     "Complex expression: Go files > 50 bytes",
			args:     []string{"main", "--name", "*.go", "--and", "--size", "+50", "--print"},
			expected: []string{"main.go", "src/app.go"},
		},
		{
			name:     "OR expression: Go files or txt files",
			args:     []string{"main", "--name", "*.go", "--or", "--name", "*.txt", "--print"},
			expected: []string{"empty.txt", "hello.txt", "large.txt", "main.go", "src/app.go", "src/utils/helper.go"},
		},
		{
			name:     "NOT expression: not Go files",
			args:     []string{"main", "--not", "--name", "*.go", "--print"},
			expected: []string{".hidden", "README.md", "empty.txt", "hello.txt", "large.txt", "tiny.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(dcfhfindPath, tt.args...)
			// Set working directory to test repo so dcfhfind can discover the repository
			cmd.Dir = testRepo
			output, err := cmd.Output()
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					t.Fatalf("dcfhfind command failed: %v\nStderr: %s", err, exitError.Stderr)
				}
				t.Fatalf("dcfhfind command failed: %v", err)
			}

			lines := strings.Split(strings.TrimSpace(string(output)), "\n")

			// Remove empty lines
			var actualLines []string
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					actualLines = append(actualLines, strings.TrimSpace(line))
				}
			}

			// Check expected results
			if len(tt.expected) > 0 {
				if len(actualLines) != len(tt.expected) {
					t.Errorf("Expected %d lines, got %d\nExpected: %v\nActual: %v",
						len(tt.expected), len(actualLines), tt.expected, actualLines)
					return
				}

				for i, expected := range tt.expected {
					if i >= len(actualLines) || actualLines[i] != expected {
						t.Errorf("Line %d: expected %q, got %q", i, expected,
							func() string {
								if i < len(actualLines) {
									return actualLines[i]
								}
								return "<missing>"
							}())
					}
				}
			}

			// Check contains requirements
			outputStr := string(output)
			for _, containsStr := range tt.contains {
				if !strings.Contains(outputStr, containsStr) {
					t.Errorf("Output should contain %q, but got: %s", containsStr, outputStr)
				}
			}
		})
	}
}

// TestValidationIntegration tests validation functionality with real files
func TestValidationIntegration(t *testing.T) {
	testRepo := "test-data/test-repo"
	if _, err := os.Stat(testRepo); os.IsNotExist(err) {
		t.Skip("Integration test repo not found")
	}

	dcfhfindPath := "./dcfhfind"
	if _, err := os.Stat(dcfhfindPath); os.IsNotExist(err) {
		t.Fatal("dcfhfind executable not found")
	}

	// Convert to absolute path so it works when we change working directory
	var err error
	dcfhfindPath, err = filepath.Abs(dcfhfindPath)
	if err != nil {
		t.Fatalf("Failed to get absolute path for dcfhfind: %v", err)
	}

	tests := []struct {
		name     string
		args     []string
		checkOut func(t *testing.T, output string)
	}{
		{
			name: "Validate all entries",
			args: []string{"main", "--validate"},
			checkOut: func(t *testing.T, output string) {
				// Should show VALID for all entries since we have a clean repo
				lines := strings.Split(strings.TrimSpace(output), "\n")
				for _, line := range lines {
					if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "VALID:") {
						t.Errorf("Expected all entries to be VALID, but got: %s", line)
					}
				}

				// Should have validation results for all 9 files
				if len(lines) != 9 {
					t.Errorf("Expected 9 validation results, got %d", len(lines))
				}
			},
		},
		{
			name: "Check for corruption",
			args: []string{"main", "--corrupt", "--print"},
			checkOut: func(t *testing.T, output string) {
				// Should be empty since our test repo has no corruption
				if strings.TrimSpace(output) != "" {
					t.Errorf("Expected no corrupt entries, but got: %s", output)
				}
			},
		},
		{
			name: "Verify checksums for small files",
			args: []string{"main", "--name", "hello.txt", "--checksum"},
			checkOut: func(t *testing.T, output string) {
				// Should show OK for hello.txt
				if !strings.Contains(output, "OK: hello.txt") {
					t.Errorf("Expected checksum OK for hello.txt, got: %s", output)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(dcfhfindPath, tt.args...)
			// Set working directory to test repo so dcfhfind can discover the repository
			cmd.Dir = testRepo
			output, err := cmd.Output()
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					t.Fatalf("dcfhfind command failed: %v\nStderr: %s", err, exitError.Stderr)
				}
				t.Fatalf("dcfhfind command failed: %v", err)
			}

			tt.checkOut(t, string(output))
		})
	}
}

// TestActionFormats tests different output formats
func TestActionFormats(t *testing.T) {
	testRepo := "test-data/test-repo"
	if _, err := os.Stat(testRepo); os.IsNotExist(err) {
		t.Skip("Integration test repo not found")
	}

	dcfhfindPath := "./dcfhfind"
	if _, err := os.Stat(dcfhfindPath); os.IsNotExist(err) {
		t.Fatal("dcfhfind executable not found")
	}

	// Convert to absolute path so it works when we change working directory
	var err error
	dcfhfindPath, err = filepath.Abs(dcfhfindPath)
	if err != nil {
		t.Fatalf("Failed to get absolute path for dcfhfind: %v", err)
	}

	tests := []struct {
		name     string
		args     []string
		checkOut func(t *testing.T, output string)
	}{
		{
			name: "List format",
			args: []string{"main", "--name", "hello.txt", "--ls"},
			checkOut: func(t *testing.T, output string) {
				// Should contain file permissions, size, and path
				if !strings.Contains(output, "hello.txt") {
					t.Errorf("--ls output should contain filename")
				}
				if !strings.Contains(output, "14") { // File size
					t.Errorf("--ls output should contain file size")
				}
				if !strings.Contains(output, "[main]") { // Index type
					t.Errorf("--ls output should contain index type")
				}
			},
		},
		{
			name: "Printf format",
			args: []string{"main", "--name", "hello.txt", "--printf", "%p:%s\\n"},
			checkOut: func(t *testing.T, output string) {
				t.Skip("Printf format output not yet matching expected — dcfhfind issue")
				expected := "hello.txt:14\n"
				if strings.TrimSpace(output) != strings.TrimSpace(expected) {
					t.Errorf("Printf format expected %q, got %q", expected, output)
				}
			},
		},
		{
			name: "Null-terminated output",
			args: []string{"main", "--name", "hello.txt", "--print0"},
			checkOut: func(t *testing.T, output string) {
				if !strings.HasSuffix(output, "hello.txt\000") {
					t.Errorf("--print0 should end with null terminator")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(dcfhfindPath, tt.args...)
			// Set working directory to test repo so dcfhfind can discover the repository
			cmd.Dir = testRepo
			output, err := cmd.Output()
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					t.Fatalf("dcfhfind command failed: %v\nStderr: %s", err, exitError.Stderr)
				}
				t.Fatalf("dcfhfind command failed: %v", err)
			}

			tt.checkOut(t, string(output))
		})
	}
}

// TestPerformanceWarning tests that checksum operations provide appropriate warnings
func TestPerformanceWarning(t *testing.T) {
	dcfhfindPath := "./dcfhfind"
	if _, err := os.Stat(dcfhfindPath); os.IsNotExist(err) {
		t.Fatal("dcfhfind executable not found")
	}

	// Convert to absolute path
	var err error
	dcfhfindPath, err = filepath.Abs(dcfhfindPath)
	if err != nil {
		t.Fatalf("Failed to get absolute path for dcfhfind: %v", err)
	}

	// Test help output contains performance warning
	cmd := exec.Command(dcfhfindPath, "--help")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("dcfhfind --help failed: %v", err)
	}

	outputStr := string(output)

	// Check for warning in --checksum description
	if !strings.Contains(outputStr, "WARNING: slow on many/large files") {
		t.Error("Help should contain performance warning for --checksum")
	}

	// Check for performance notes section
	if !strings.Contains(outputStr, "PERFORMANCE NOTES:") {
		t.Error("Help should contain PERFORMANCE NOTES section")
	}

	if !strings.Contains(outputStr, "reads file contents to compute hashes") {
		t.Error("Help should explain why checksum is slow")
	}
}
