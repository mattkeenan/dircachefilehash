package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestUpdateInterruption tests that update operations handle interruptions gracefully
func TestUpdateInterruption(t *testing.T) {
	t.Skip("Interruption tests depend on old callback architecture — pending pipeline migration")
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "dcfh_interruption_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Build dcfh binary
	dcfhBinary := filepath.Join(tempDir, "dcfh")
	buildCmd := exec.Command("go", "build", "-o", dcfhBinary, ".")
	// Get current working directory (we're in cmd/dcfh)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	buildCmd.Dir = cwd
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build dcfh: %v\nOutput: %s", err, output)
	}

	// Create test repository
	repoDir := filepath.Join(tempDir, "test_repo")
	if err := os.Mkdir(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	// Initialize repository
	initCmd := exec.Command(dcfhBinary, "init", repoDir)
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to init repo: %v\nOutput: %s", err, output)
	}

	// Create test files
	for i := range 100 {
		fileName := filepath.Join(repoDir, fmt.Sprintf("file_%03d.txt", i))
		content := fmt.Sprintf("This is test file %d with some content to hash\n", i)
		if err := os.WriteFile(fileName, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Create subdirectories with files
	for i := range 10 {
		subDir := filepath.Join(repoDir, fmt.Sprintf("subdir_%02d", i))
		if err := os.Mkdir(subDir, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}
		for j := range 10 {
			fileName := filepath.Join(subDir, fmt.Sprintf("file_%02d.txt", j))
			content := fmt.Sprintf("Subdir %d file %d content\n", i, j)
			if err := os.WriteFile(fileName, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create subdir file: %v", err)
			}
		}
	}

	// Test 1: Interrupt during first update (no existing index)
	t.Run("InterruptFirstUpdate", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()

		updateCmd := exec.CommandContext(ctx, dcfhBinary, "update", "-vvv", "--debug=scan,scanning,hash,load,write")
		updateCmd.Dir = repoDir
		output, err := updateCmd.CombinedOutput()

		// Should have been interrupted
		if err == nil {
			t.Errorf("Expected interruption error, got none")
		}

		// Check for segfault or panic
		outputStr := string(output)
		if strings.Contains(outputStr, "SIGSEGV") || strings.Contains(outputStr, "panic") || strings.Contains(outputStr, "fatal error") {
			t.Errorf("Update crashed with segfault/panic:\n%s", outputStr)
		}

		// Cache index should exist with partial results
		cacheFile := filepath.Join(repoDir, ".dcfh", "cache.idx")
		if _, err := os.Stat(cacheFile); os.IsNotExist(err) {
			t.Logf("Warning: Cache file not created after interruption (might be too early)")
		}
	})

	// Complete a successful update first
	t.Run("CompleteUpdate", func(t *testing.T) {
		updateCmd := exec.Command(dcfhBinary, "update", "-vvv", "--debug=scan,scanning,hash,load,write")
		updateCmd.Dir = repoDir
		output, err := updateCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to complete update: %v\nOutput: %s", err, output)
		}
		t.Logf("Update output: %s", output)

		// Main index should exist
		mainFile := filepath.Join(repoDir, ".dcfh", "main.idx")
		if _, err := os.Stat(mainFile); os.IsNotExist(err) {
			t.Errorf("Main index not created after successful update")
		}
	})

	// Test 2: Interrupt during second update (with existing index)
	t.Run("InterruptSecondUpdate", func(t *testing.T) {
		// Modify some files
		for i := range 10 {
			fileName := filepath.Join(repoDir, fmt.Sprintf("file_%03d.txt", i))
			content := fmt.Sprintf("Modified content for file %d\n", i)
			if err := os.WriteFile(fileName, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to modify file: %v", err)
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()

		updateCmd := exec.CommandContext(ctx, dcfhBinary, "-vvv", "--debug=scan,scanning", "update")
		updateCmd.Dir = repoDir
		output, err := updateCmd.CombinedOutput()

		// Should have been interrupted
		if err == nil {
			t.Errorf("Expected interruption error, got none")
		}

		// Check for segfault or panic
		outputStr := string(output)
		if strings.Contains(outputStr, "SIGSEGV") || strings.Contains(outputStr, "panic") || strings.Contains(outputStr, "fatal error") {
			t.Errorf("Update crashed with segfault/panic:\n%s", outputStr)
		}

		// Cache should exist with partial results
		cacheFile := filepath.Join(repoDir, ".dcfh", "cache.idx")
		if _, err := os.Stat(cacheFile); err != nil {
			t.Logf("Cache file status: %v", err)
		}
	})

	// Test 3: Complete update after interruption (should use cached results)
	t.Run("UpdateAfterInterruption", func(t *testing.T) {
		updateCmd := exec.Command(dcfhBinary, "-vvv", "--debug=scan,scanning,hash", "update")
		updateCmd.Dir = repoDir
		output, err := updateCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to complete update after interruption: %v\nOutput: %s", err, output)
		}

		// Check that we didn't re-hash everything
		outputStr := string(output)
		hashCount := strings.Count(outputStr, "Hashing file:")
		if hashCount > 20 { // Should only hash the 10 modified files plus any missed from interruption
			t.Errorf("Too many files hashed (%d), cache might not be working", hashCount)
		}
	})

	// Test 4: Interrupt with specific paths
	t.Run("InterruptSpecificPaths", func(t *testing.T) {
		// Modify files in a subdirectory
		subDir := filepath.Join(repoDir, "subdir_05")
		for i := range 5 {
			fileName := filepath.Join(subDir, fmt.Sprintf("file_%02d.txt", i))
			content := fmt.Sprintf("Re-modified subdir file %d\n", i)
			if err := os.WriteFile(fileName, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to modify subdir file: %v", err)
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()

		updateCmd := exec.CommandContext(ctx, dcfhBinary, "update", "subdir_05")
		updateCmd.Dir = repoDir
		output, _ := updateCmd.CombinedOutput()

		// Check for segfault or panic
		outputStr := string(output)
		if strings.Contains(outputStr, "SIGSEGV") || strings.Contains(outputStr, "panic") || strings.Contains(outputStr, "fatal error") {
			t.Errorf("Update with paths crashed with segfault/panic:\n%s", outputStr)
		}
	})

	// Test 5: Status command after interruptions (tests cache loading)
	t.Run("StatusAfterInterruptions", func(t *testing.T) {
		statusCmd := exec.Command(dcfhBinary, "status")
		statusCmd.Dir = repoDir
		output, err := statusCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Status command failed: %v\nOutput: %s", err, output)
		}

		// Should not crash
		outputStr := string(output)
		if strings.Contains(outputStr, "SIGSEGV") || strings.Contains(outputStr, "panic") {
			t.Errorf("Status crashed:\n%s", outputStr)
		}
	})
}

// TestMemoryMappingIssues tests for memory mapping problems during interruptions
func TestMemoryMappingIssues(t *testing.T) {
	// This test specifically targets the scan index memory mapping issue
	tempDir, err := os.MkdirTemp("", "dcfh_mmap_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Build dcfh binary
	dcfhBinary := filepath.Join(tempDir, "dcfh")
	buildCmd := exec.Command("go", "build", "-race", "-o", dcfhBinary, ".")
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	buildCmd.Dir = cwd
	if _, err := buildCmd.CombinedOutput(); err != nil {
		t.Logf("Warning: Failed to build with race detector: %v", err)
		// Try without race detector
		buildCmd = exec.Command("go", "build", "-o", dcfhBinary, ".")
		buildCmd.Dir = cwd
		if output, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("Failed to build dcfh: %v\nOutput: %s", err, output)
		}
	}

	// Create large repository to increase chance of hitting memory issues
	repoDir := filepath.Join(tempDir, "large_repo")
	if err := os.Mkdir(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	// Initialize
	initCmd := exec.Command(dcfhBinary, "init", repoDir)
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to init repo: %v\nOutput: %s", err, output)
	}

	// Create many files to trigger mremap operations
	for i := range 1000 {
		fileName := filepath.Join(repoDir, fmt.Sprintf("file_%04d.txt", i))
		content := make([]byte, 1024) // 1KB per file
		for j := range content {
			content[j] = byte('A' + (i+j)%26)
		}
		if err := os.WriteFile(fileName, content, 0644); err != nil {
			t.Fatalf("Failed to create file %d: %v", i, err)
		}
	}

	// Start update and interrupt it multiple times
	for attempt := range 5 {
		t.Run(fmt.Sprintf("Attempt%d", attempt), func(t *testing.T) {
			// Vary the interruption timing
			delay := time.Duration(20+attempt*10) * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), delay)
			defer cancel()

			updateCmd := exec.CommandContext(ctx, dcfhBinary, "update")
			updateCmd.Dir = repoDir
			output, _ := updateCmd.CombinedOutput()

			// Check for memory-related crashes
			outputStr := string(output)
			if strings.Contains(outputStr, "unexpected fault address") ||
				strings.Contains(outputStr, "SIGSEGV") ||
				strings.Contains(outputStr, "runtime error: invalid memory address") {
				t.Errorf("Memory access error in attempt %d (delay %v):\n%s", attempt, delay, outputStr)
			}
		})
	}
}

// TestAdaptiveStatusInterruption tests that status operations save partial cache on interruption
func TestAdaptiveStatusInterruption(t *testing.T) {
	t.Skip("Status interruption tests depend on old callback architecture — pending pipeline migration")
	// Skip if strace is not available
	if _, err := exec.LookPath("strace"); err != nil {
		t.Skip("strace not available, skipping test")
	}

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "dcfh_adaptive_interrupt_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Build dcfh binary
	dcfhBinary := filepath.Join(tempDir, "dcfh")
	buildCmd := exec.Command("go", "build", "-o", dcfhBinary, ".")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	buildCmd.Dir = cwd
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build dcfh: %v\nOutput: %s", err, output)
	}

	// Adaptive test parameters
	numFiles := 500                         // Start with 500 files
	numLargeFiles := 5                      // Start with 5 large files
	largeFileSize := 10000                  // 10KB
	smallFileSize := 100                    // 100 bytes
	interruptDelay := 10 * time.Millisecond // Start with longer delay
	maxAttempts := 10

	// Regex patterns for strace analysis
	sigintPattern := regexp.MustCompile(`(kill|sigaction|SIGINT|signal\(2\)|rt_sigaction)`)
	scanOpenPattern := regexp.MustCompile(`open(at)?\(.*scan-\d+-\d+\.idx`)
	cacheWritePattern := regexp.MustCompile(`(writev|write|pwrite64).*cache.*\.idx`)
	cacheRenamePattern := regexp.MustCompile(`rename\(".*cache.*\.tmp", ".*cache\.idx"\)`)

	var interrupted bool
	for attempt := 0; attempt < maxAttempts && !interrupted; attempt++ {
		t.Run(fmt.Sprintf("Attempt%d_files%d_delay%v", attempt, numFiles, interruptDelay), func(t *testing.T) {
			// Create test repository
			repoDir := filepath.Join(tempDir, fmt.Sprintf("repo_%d", attempt))
			if err := os.MkdirAll(repoDir, 0755); err != nil {
				t.Fatalf("Failed to create repo dir: %v", err)
			}

			// Initialize repository
			initCmd := exec.Command(dcfhBinary, "init", repoDir)
			if output, err := initCmd.CombinedOutput(); err != nil {
				t.Fatalf("Failed to init repo: %v\nOutput: %s", err, output)
			}

			// Create test files with mixed sizes
			t.Logf("Creating %d files (%d large, %d small)", numFiles, numLargeFiles, numFiles-numLargeFiles)
			for i := 0; i < numFiles; i++ {
				fileName := filepath.Join(repoDir, fmt.Sprintf("file_%06d.txt", i))
				var content []byte
				if i < numLargeFiles {
					// Large file
					content = make([]byte, largeFileSize)
					for j := range content {
						content[j] = byte('L' + (i+j)%26)
					}
				} else {
					// Small file
					content = make([]byte, smallFileSize)
					for j := range content {
						content[j] = byte('S' + (i+j)%26)
					}
				}
				if err := os.WriteFile(fileName, content, 0644); err != nil {
					t.Fatalf("Failed to create file %d: %v", i, err)
				}
			}

			// Prepare strace output file
			straceFile := filepath.Join(tempDir, fmt.Sprintf("strace_%d.log", attempt))

			// Run status under strace with timeout and interruption
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			straceCmd := exec.CommandContext(ctx, "strace", "-f", "-o", straceFile,
				dcfhBinary, "--debug=scan,scanning", "status")
			straceCmd.Dir = repoDir

			// Capture stdout/stderr
			var stdout, stderr bytes.Buffer
			straceCmd.Stdout = &stdout
			straceCmd.Stderr = &stderr

			// Start the command
			if err := straceCmd.Start(); err != nil {
				t.Fatalf("Failed to start strace: %v", err)
			}

			// Wait for specified delay then interrupt
			time.Sleep(interruptDelay)

			// Find the dcfh process PID (child of strace)
			var dcfhPid int
			if children, err := exec.Command("pgrep", "-P", fmt.Sprintf("%d", straceCmd.Process.Pid)).Output(); err == nil {
				childPids := strings.SplitSeq(strings.TrimSpace(string(children)), "\n")
				for pidStr := range childPids {
					if pidStr != "" {
						if pid, err := strconv.Atoi(pidStr); err == nil {
							// Check if this process is dcfh by looking at command line
							if cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
								if strings.Contains(string(cmdline), "dcfh") {
									dcfhPid = pid
									break
								}
							}
						}
					}
				}
			}

			// Send interrupt signal to dcfh process, not strace
			if dcfhPid > 0 {
				if err := syscall.Kill(dcfhPid, syscall.SIGINT); err != nil {
					t.Logf("Failed to send interrupt to dcfh process %d: %v", dcfhPid, err)
				} else {
					t.Logf("Sent SIGINT to dcfh process %d", dcfhPid)
				}

				// Give dcfh a moment to handle the signal, then terminate strace
				time.Sleep(100 * time.Millisecond)
				if err := straceCmd.Process.Signal(os.Interrupt); err != nil {
					t.Logf("Failed to send interrupt to strace: %v", err)
				}
			} else {
				t.Logf("Could not find dcfh process, falling back to interrupting strace")
				if err := straceCmd.Process.Signal(os.Interrupt); err != nil {
					t.Logf("Failed to send interrupt to strace: %v", err)
				}
			}

			// Wait for completion
			_ = straceCmd.Wait()

			// Read strace output
			straceOutput, readErr := os.ReadFile(straceFile)
			if readErr != nil {
				t.Logf("Failed to read strace output: %v", readErr)
			}

			// Analyze results
			straceStr := string(straceOutput)
			stdoutStr := stdout.String()
			stderrStr := stderr.String()

			// Find signal handling in strace
			sigintMatch := sigintPattern.FindString(straceStr)
			scanOpenMatch := scanOpenPattern.FindString(straceStr)

			t.Logf("Signal match: %v", sigintMatch != "")
			t.Logf("Scan open match: %v", scanOpenMatch != "")

			// Also check for scan files directly
			scanFiles, _ := filepath.Glob(filepath.Join(repoDir, ".dcfh", "scan-*.idx"))
			t.Logf("Scan files found: %d", len(scanFiles))

			// Check if we interrupted during scan
			if sigintMatch != "" && (scanOpenMatch != "" || len(scanFiles) > 0) {
				// We found evidence of interruption and scan activity
				interrupted = true
				t.Logf("Successfully interrupted during scan phase")

				if scanOpenMatch != "" {
					// Find the positions to check order
					sigintPos := strings.Index(straceStr, sigintMatch)
					scanPos := strings.Index(straceStr, scanOpenMatch)
					t.Logf("Signal position: %d, Scan position: %d", sigintPos, scanPos)
				}

				// Check if cache was written
				cacheWriteMatch := cacheWritePattern.FindString(straceStr)
				cacheRenameMatch := cacheRenamePattern.FindString(straceStr)

				t.Logf("Cache write found: %v", cacheWriteMatch != "")
				t.Logf("Cache rename found: %v", cacheRenameMatch != "")

				// Check for cache file on disk
				cacheFile := filepath.Join(repoDir, ".dcfh", "cache.idx")
				if info, err := os.Stat(cacheFile); err == nil {
					t.Logf("Cache file created with size: %d bytes", info.Size())
				} else {
					t.Errorf("Cache file not found after interruption: %v", err)
				}

				// Check stdout/stderr for expected messages
				if strings.Contains(stderrStr, "interrupt") || strings.Contains(stdoutStr, "interrupt") {
					t.Logf("Found interruption message in output")
				}
				if strings.Contains(stderrStr, "WORKFLOW") && strings.Contains(stderrStr, "cache") {
					t.Logf("Found cache workflow messages")
				}
			}

			// If not interrupted, prepare for next attempt
			if !interrupted {
				t.Logf("Interruption too slow, adjusting parameters")
			}
		})

		// Adjust parameters for next attempt if needed
		if !interrupted {
			numFiles = numFiles * 2
			numLargeFiles = numLargeFiles * 3
			interruptDelay = time.Duration(float64(interruptDelay) * 0.75)

			// Safety check to prevent runaway
			if numFiles > 10000000 {
				t.Fatalf("Failed to achieve interruption after %d attempts with %d files", attempt+1, numFiles)
			}
		}
	}

	if !interrupted {
		t.Errorf("Failed to achieve proper scan interruption after %d attempts", maxAttempts)
	}
}

// Adaptive interrupt testing framework - shared components

// AdaptiveTestConfig holds configuration for adaptive interrupt tests
type AdaptiveTestConfig struct {
	Command           string        // Command to test ("status" or "update")
	InitialFiles      int           // Starting number of files
	InitialLargeFiles int           // Starting number of large files
	LargeFileSize     int           // Size of large files in bytes
	SmallFileSize     int           // Size of small files in bytes
	InitialDelay      time.Duration // Starting interrupt delay
	MaxAttempts       int           // Maximum attempts before giving up
	MaxFiles          int           // Safety limit on file count
	ExtraArgs         []string      // Additional command arguments
	RequireStrace     bool          // Whether strace is required
}

// AdaptiveTestResult contains results from an interrupt attempt
type AdaptiveTestResult struct {
	Interrupted    bool
	SignalFound    bool
	ScanActivity   bool
	CacheActivity  bool
	OutputContains map[string]bool // Key patterns found in output
	ScanFiles      []string        // Scan files found on disk
	CacheFileSize  int64           // Size of cache file if created
}

// DefaultAdaptiveConfig returns a sensible default configuration
func DefaultAdaptiveConfig(command string) AdaptiveTestConfig {
	return AdaptiveTestConfig{
		Command:           command,
		InitialFiles:      1000,                  // Start with reasonable number for testing
		InitialLargeFiles: 50,                    // Fewer large files
		LargeFileSize:     10000,                 // 10KB files (much smaller)
		SmallFileSize:     1000,                  // 1KB files
		InitialDelay:      15 * time.Millisecond, // Longer initial delay
		MaxAttempts:       15,                    // Fewer attempts
		MaxFiles:          500000,                // Lower limit for faster testing
		ExtraArgs:         []string{"-vvv", "--debug=scan,scanning,hash,load,write,algorithm,indexchaining"},
		RequireStrace:     true,
	}
}

// AdaptiveInterruptTest runs an adaptive interrupt test with the given configuration
func AdaptiveInterruptTest(t *testing.T, config AdaptiveTestConfig) {
	// Skip if strace is required but not available
	if config.RequireStrace {
		if _, err := exec.LookPath("strace"); err != nil {
			t.Skip("strace not available, skipping adaptive interrupt test")
		}
	}

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("dcfh_adaptive_%s_*", config.Command))
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Build dcfh binary
	dcfhBinary := buildAdaptiveTestBinary(t, tempDir)

	// Adaptive parameters (will be modified in loop)
	numFiles := config.InitialFiles
	numLargeFiles := config.InitialLargeFiles
	interruptDelay := config.InitialDelay

	// Regex patterns for analysis
	patterns := createAnalysisPatterns()

	var result AdaptiveTestResult
	for attempt := 0; attempt < config.MaxAttempts && !result.Interrupted; attempt++ {
		t.Run(fmt.Sprintf("Attempt%d_files%d_delay%v", attempt, numFiles, interruptDelay), func(t *testing.T) {
			// Create test repository for this attempt
			repoDir := filepath.Join(tempDir, fmt.Sprintf("repo_%d", attempt))
			createAdaptiveTestRepo(t, dcfhBinary, repoDir, numFiles, numLargeFiles,
				config.LargeFileSize, config.SmallFileSize)

			// Run adaptive interrupt attempt
			result = runAdaptiveInterruptAttempt(t, dcfhBinary, repoDir, config.Command,
				config.ExtraArgs, interruptDelay, patterns, tempDir, attempt)

			if result.Interrupted {
				// Success! Log the results
				t.Logf("Successfully interrupted %s during scan phase", config.Command)
				t.Logf("Final parameters: %d files, %d large files, %v delay",
					numFiles, numLargeFiles, interruptDelay)
				logAdaptiveTestResults(t, result)
			}
		})

		// Adjust parameters for next attempt if needed
		if !result.Interrupted {
			numFiles = numFiles * 2
			numLargeFiles = numLargeFiles * 3
			interruptDelay = time.Duration(float64(interruptDelay) * 0.75)

			// Safety check
			if numFiles > config.MaxFiles {
				t.Fatalf("Failed to achieve interruption after %d attempts with %d files",
					attempt+1, numFiles)
			}

			t.Logf("Interruption too fast, scaling up: %d files, %v delay",
				numFiles, interruptDelay)
		}
	}

	if !result.Interrupted {
		t.Errorf("Failed to achieve proper %s interruption after %d attempts",
			config.Command, config.MaxAttempts)
	}
}

// buildAdaptiveTestBinary builds the dcfh binary for testing
func buildAdaptiveTestBinary(t *testing.T, tempDir string) string {
	dcfhBinary := filepath.Join(tempDir, "dcfh")
	buildCmd := exec.Command("go", "build", "-o", dcfhBinary, ".")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	buildCmd.Dir = cwd

	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build dcfh: %v\nOutput: %s", err, output)
	}

	return dcfhBinary
}

// createAdaptiveTestRepo creates a test repository with the specified number of files
func createAdaptiveTestRepo(t *testing.T, dcfhBinary, repoDir string, numFiles, numLargeFiles, largeFileSize, smallFileSize int) {
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	// Initialize repository
	initCmd := exec.Command(dcfhBinary, "init", repoDir)
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to init repo: %v\nOutput: %s", err, output)
	}

	// Create test files with mixed sizes
	t.Logf("Creating %d files (%d large, %d small)", numFiles, numLargeFiles, numFiles-numLargeFiles)
	for i := range numFiles {
		fileName := filepath.Join(repoDir, fmt.Sprintf("file_%06d.txt", i))
		var content []byte

		if i < numLargeFiles {
			// Large file
			content = make([]byte, largeFileSize)
			for j := range content {
				content[j] = byte('L' + (i+j)%26)
			}
		} else {
			// Small file
			content = make([]byte, smallFileSize)
			for j := range content {
				content[j] = byte('S' + (i+j)%26)
			}
		}

		if err := os.WriteFile(fileName, content, 0644); err != nil {
			t.Fatalf("Failed to create file %d: %v", i, err)
		}
	}
	t.Logf("Created %d files (%d large, %d small)", numFiles, numLargeFiles, numFiles-numLargeFiles)
}

// createAnalysisPatterns returns regex patterns for analyzing strace output
func createAnalysisPatterns() map[string]*regexp.Regexp {
	return map[string]*regexp.Regexp{
		// Signal detection
		"signal": regexp.MustCompile(`(kill|sigaction|SIGINT|signal\(2\)|rt_sigaction)`),

		// v0.7: Any .idx file operations
		"indexOpen":     regexp.MustCompile(`open(at)?\([^,]*\.idx[^,]*,`),
		"tempIndexOpen": regexp.MustCompile(`open(at)?\([^,]*(main-index|status-cache)-\d+-\d+\.tmp[^,]*,`),

		// Write operations - capture fd number
		"writeOps": regexp.MustCompile(`write.*\((\d+),`),

		// File operations
		"close":  regexp.MustCompile(`close\(\d+\)`),
		"rename": regexp.MustCompile(`rename\(.*\.tmp.*,.*\.idx`),
		"unlink": regexp.MustCompile(`unlink\(.*\.tmp.*\)`),

		// Legacy v0.6 patterns (for backwards compatibility)
		"scanOpen":    regexp.MustCompile(`open(at)?\(.*scan-\d+-\d+\.idx`),
		"cacheWrite":  regexp.MustCompile(`(writev|write|pwrite64).*cache.*\.idx`),
		"cacheRename": regexp.MustCompile(`rename\(".*cache.*\.tmp", ".*cache\.idx"\)`),
	}
}

// runAdaptiveInterruptAttempt executes a single interrupt attempt and analyzes results
func runAdaptiveInterruptAttempt(t *testing.T, dcfhBinary, repoDir, command string, extraArgs []string, interruptDelay time.Duration, patterns map[string]*regexp.Regexp, tempDir string, attempt int) AdaptiveTestResult {
	// Prepare strace output file
	straceFile := filepath.Join(tempDir, fmt.Sprintf("strace_%s_%d.log", command, attempt))

	// Build command arguments
	args := []string{"-f", "-s", "1500", "-o", straceFile, dcfhBinary}
	args = append(args, extraArgs...)
	args = append(args, command)

	// Run command under strace with timeout and interruption
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	straceCmd := exec.CommandContext(ctx, "strace", args...)
	straceCmd.Dir = repoDir

	// Capture stdout/stderr
	var stdout, stderr bytes.Buffer
	straceCmd.Stdout = &stdout
	straceCmd.Stderr = &stderr

	// Start the command
	if err := straceCmd.Start(); err != nil {
		t.Fatalf("Failed to start strace: %v", err)
	}

	// Wait for specified delay then interrupt
	time.Sleep(interruptDelay)

	// Send interrupt signal
	if err := straceCmd.Process.Signal(os.Interrupt); err != nil {
		t.Logf("Failed to send interrupt: %v", err)
	}

	// Wait for completion
	_ = straceCmd.Wait()

	// Analyze results
	return analyzeInterruptResult(t, straceFile, repoDir, stdout.String(), stderr.String(), patterns)
}

// StraceEvent represents a single strace event with timing information
type StraceEvent struct {
	syscall   string
	filename  string
	operation string
	fd        int
}

// StraceAnalysis contains timeline-based analysis of strace output
type StraceAnalysis struct {
	fdToFile   map[int]string // fd -> file path
	signalTime time.Time      // When signal occurred
	preSignal  []StraceEvent  // Events before signal
	postSignal []StraceEvent  // Events after signal
}

// analyzeInterruptResult analyzes the results of an interrupt attempt using enhanced v0.7 analysis
func analyzeInterruptResult(t *testing.T, straceFile, repoDir, stdout, stderr string, patterns map[string]*regexp.Regexp) AdaptiveTestResult {
	result := AdaptiveTestResult{
		OutputContains: make(map[string]bool),
	}

	// Read strace output
	straceOutput, readErr := os.ReadFile(straceFile)
	if readErr != nil {
		t.Logf("Failed to read strace output: %v", readErr)
		return result
	}

	straceStr := string(straceOutput)

	// Enhanced v0.7 analysis with timeline tracking
	analysis := parseStraceTimeline(straceStr, patterns)

	// Basic signal detection
	result.SignalFound = patterns["signal"].MatchString(straceStr)

	// Get signal delivery line number for debugging (look for actual delivery, not setup)
	signalDeliveryLineNumber := -1
	lines := strings.Split(straceStr, "\n")
	signalDeliveryRegex := regexp.MustCompile(`--- SIG|killed by SIG|\+\+\+ killed by SIG|si_signo=SIG`)
	for i, line := range lines {
		if signalDeliveryRegex.MatchString(line) {
			signalDeliveryLineNumber = i
			break
		}
	}

	// v0.7: Check for temp index operations instead of scan files
	tempIndexActivity := patterns["tempIndexOpen"].MatchString(straceStr)
	indexActivity := patterns["indexOpen"].MatchString(straceStr)
	writeActivity := patterns["writeOps"].MatchString(straceStr)

	// Enhanced activity detection
	result.ScanActivity = tempIndexActivity || (indexActivity && writeActivity)
	result.CacheActivity = patterns["cacheWrite"].MatchString(straceStr) || patterns["cacheRename"].MatchString(straceStr)

	// Legacy v0.6: Check for scan files (for backwards compatibility)
	scanFiles, _ := filepath.Glob(filepath.Join(repoDir, ".dcfh", "scan-*.idx"))
	result.ScanFiles = scanFiles

	// Phase 1: Signal Timing Validation (correct approach)
	// Good timing = SIGINT found + index files opened + index files closed AFTER SIGINT
	sigintFound := result.SignalFound
	indexFilesOpened := len(analysis.fdToFile) > 0

	// Check if any index files were closed AFTER SIGINT signal
	indexFilesClosedAfterSignal := false
	for _, event := range analysis.postSignal {
		if event.syscall == "close" {
			indexFilesClosedAfterSignal = true
			break
		}
	}

	// Phase 1 success criteria: Signal timing validation
	goodSignalTiming := sigintFound && indexFilesOpened && indexFilesClosedAfterSignal

	// Legacy v0.6 criteria (for backwards compatibility)
	v6Interrupted := result.SignalFound && (result.ScanActivity || len(result.ScanFiles) > 0)

	// Use Phase 1 criteria as primary, fall back to v0.6 for older tests
	result.Interrupted = goodSignalTiming || v6Interrupted

	// Log enhanced Phase 1 analysis details
	if result.SignalFound {
		t.Logf("=== Phase 1: Signal Timing Analysis ===")
		t.Logf("SIGINT signal detected: %v", sigintFound)
		t.Logf("Signal delivery found at line: %d", signalDeliveryLineNumber)
		t.Logf("Total strace lines: %d", len(strings.Split(straceStr, "\n")))
		t.Logf("Index files opened: %v (count: %d)", indexFilesOpened, len(analysis.fdToFile))
		t.Logf("Index files tracked: %v", analysis.fdToFile)
		t.Logf("Index files closed after SIGINT: %v", indexFilesClosedAfterSignal)
		t.Logf("Phase 1 good timing: %v", goodSignalTiming)

		// Count writes by type and timing
		preSignalWrites := 0
		postSignalWrites := 0
		preSignalCloses := 0
		postSignalCloses := 0

		for _, event := range analysis.preSignal {
			switch event.syscall {
			case "write":
				preSignalWrites++
			case "close":
				preSignalCloses++
			}
		}
		for _, event := range analysis.postSignal {
			switch event.syscall {
			case "write":
				postSignalWrites++
			case "close":
				postSignalCloses++
			}
		}

		t.Logf("=== Signal Handling Analysis ===")
		t.Logf("Pre-signal writes to index files: %d", preSignalWrites)
		t.Logf("Post-signal writes to index files: %d", postSignalWrites)
		t.Logf("Pre-signal file closes: %d", preSignalCloses)
		t.Logf("Post-signal file closes: %d", postSignalCloses)

		if goodSignalTiming {
			if postSignalWrites > 0 {
				t.Logf("🚨 CONCERN: %d writes AFTER signal - should signal handling stop writes?", postSignalWrites)
			} else {
				t.Logf("✅ GOOD: No writes after signal - proper signal handling")
			}
		}

		t.Logf("=== Event Timeline ===")
		t.Logf("Pre-SIGINT events: %d", len(analysis.preSignal))
		for i, event := range analysis.preSignal {
			t.Logf("  Pre-%d: %s(fd=%d) file=%s", i, event.syscall, event.fd, event.filename)
		}
		t.Logf("Post-SIGINT events: %d", len(analysis.postSignal))
		for i, event := range analysis.postSignal {
			t.Logf("  Post-%d: %s(fd=%d) file=%s", i, event.syscall, event.fd, event.filename)
		}

		t.Logf("=== Legacy v0.6 ===")
		t.Logf("v0.6 scan files: %d", len(scanFiles))
		t.Logf("Final result: %v", result.Interrupted)
	}

	// Check for cache file creation
	cacheFile := filepath.Join(repoDir, ".dcfh", "cache.idx")
	if info, err := os.Stat(cacheFile); err == nil {
		result.CacheFileSize = info.Size()
	}

	// Check output for key patterns
	combinedOutput := stdout + stderr
	result.OutputContains["interrupt"] = strings.Contains(combinedOutput, "interrupt")
	result.OutputContains["workflow"] = strings.Contains(combinedOutput, "WORKFLOW") && strings.Contains(combinedOutput, "cache")
	result.OutputContains["scanning"] = strings.Contains(combinedOutput, "Scanning") || strings.Contains(combinedOutput, "SCAN")

	return result
}

// parseStraceTimeline parses strace output into a timeline of events for enhanced analysis
func parseStraceTimeline(straceStr string, patterns map[string]*regexp.Regexp) StraceAnalysis {
	analysis := StraceAnalysis{
		fdToFile:   make(map[int]string),
		preSignal:  []StraceEvent{},
		postSignal: []StraceEvent{},
	}

	lines := strings.Split(straceStr, "\n")
	sigintLineNumber := -1

	// Enhanced regex patterns for precise parsing (fixed for actual strace format)
	openRegex := regexp.MustCompile(`openat\(AT_FDCWD,\s*"([^"]*(?:\.idx|\.tmp)[^"]*)"[^)]*\)\s*=\s*(\d+)`)
	closeRegex := regexp.MustCompile(`close\((\d+)\)\s*=\s*0`)
	writeRegex := regexp.MustCompile(`write.*\((\d+),`)
	// Look for actual signal delivery/handling, not just setup
	signalDeliveryRegex := regexp.MustCompile(`--- SIG|killed by SIG|\+\+\+ killed by SIG|si_signo=SIG`)

	// First pass: Find actual signal delivery (not setup)
	for i, line := range lines {
		if signalDeliveryRegex.MatchString(line) {
			sigintLineNumber = i
			// Store the actual signal line for debugging
			analysis.signalTime = time.Now() // Just for the struct
			break
		}
	}

	// Second pass: Parse all file operations and classify by timeline position
	for i, line := range lines {
		// Parse open() syscalls to build fd->file mapping for index files
		if openMatch := openRegex.FindStringSubmatch(line); openMatch != nil {
			filename := openMatch[1]
			fd, err := strconv.Atoi(openMatch[2])
			if err == nil && (strings.Contains(filename, ".idx") || strings.Contains(filename, ".tmp")) {
				analysis.fdToFile[fd] = filename

				event := StraceEvent{
					fd:        fd,
					filename:  filename,
					syscall:   "open",
					operation: "file-open",
				}

				// Classify by position relative to SIGINT
				if sigintLineNumber == -1 || i < sigintLineNumber {
					analysis.preSignal = append(analysis.preSignal, event)
				} else {
					analysis.postSignal = append(analysis.postSignal, event)
				}
			}
		}

		// Parse close() syscalls for index file descriptors
		if closeMatch := closeRegex.FindStringSubmatch(line); closeMatch != nil {
			fd, err := strconv.Atoi(closeMatch[1])
			if err == nil {
				// Check if this fd maps to an index file
				if filename, exists := analysis.fdToFile[fd]; exists {
					event := StraceEvent{
						fd:        fd,
						filename:  filename,
						syscall:   "close",
						operation: "file-close",
					}

					// Classify by position relative to SIGINT
					if sigintLineNumber == -1 || i < sigintLineNumber {
						analysis.preSignal = append(analysis.preSignal, event)
					} else {
						analysis.postSignal = append(analysis.postSignal, event)
					}
				}
			}
		}

		// Track write operations to known index file descriptors
		if writeMatch := writeRegex.FindStringSubmatch(line); writeMatch != nil {
			fd, err := strconv.Atoi(writeMatch[1])
			if err == nil {
				// Check if this fd maps to an index file
				if filename, exists := analysis.fdToFile[fd]; exists {
					event := StraceEvent{
						fd:        fd,
						filename:  filename,
						syscall:   "write",
						operation: "file-write",
					}

					// Classify by position relative to SIGINT
					if sigintLineNumber == -1 || i < sigintLineNumber {
						analysis.preSignal = append(analysis.preSignal, event)
					} else {
						analysis.postSignal = append(analysis.postSignal, event)
					}
				}
			}
		}
	}

	return analysis
}

// logAdaptiveTestResults logs detailed results from a successful interrupt
func logAdaptiveTestResults(t *testing.T, result AdaptiveTestResult) {
	t.Logf("Signal detection: %v", result.SignalFound)
	t.Logf("Scan activity detected: %v", result.ScanActivity)
	t.Logf("Cache activity detected: %v", result.CacheActivity)
	t.Logf("Scan files found: %d", len(result.ScanFiles))

	if result.CacheFileSize > 0 {
		t.Logf("Cache file created: %d bytes", result.CacheFileSize)
	}

	for pattern, found := range result.OutputContains {
		if found {
			t.Logf("Output contains %s: %v", pattern, found)
		}
	}
}

// TestAdaptiveUpdateInterruption tests update operations with adaptive parameters
func TestAdaptiveUpdateInterruption(t *testing.T) {
	config := DefaultAdaptiveConfig("update")
	config.ExtraArgs = []string{"-vvv", "--debug=algorithm,extravalidation,hash,indexchaining,load,memorylayout,scan,scanning,symlinks,write"}
	AdaptiveInterruptTest(t, config)
}

// TestAdaptiveStatusInterruptionRefactored tests status operations using the refactored framework
func TestAdaptiveStatusInterruptionRefactored(t *testing.T) {
	config := DefaultAdaptiveConfig("status")
	config.ExtraArgs = []string{"--debug=scan,scanning"}
	AdaptiveInterruptTest(t, config)
}
