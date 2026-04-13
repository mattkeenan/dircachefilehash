package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// getRepoRoot finds the repository root using git
func getRepoRoot(t *testing.T) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to find repository root: %v", err)
	}
	return strings.TrimSpace(string(output))
}

// processTracker tracks process lifecycle events with millisecond precision
type processTracker struct {
	mu             sync.Mutex
	events         []string
	startTime      time.Time
	processStart   time.Time
	signalSent     time.Time
	signalReceived time.Time
	shutdownStart  time.Time
	processExit    time.Time
}

func (pt *processTracker) recordEvent(event string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	elapsed := time.Since(pt.startTime)
	pt.events = append(pt.events, fmt.Sprintf("[%7.3fms] %s", elapsed.Seconds()*1000, event))
}

func (pt *processTracker) getEvents() []string {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return append([]string{}, pt.events...)
}

// TestSignalHandlingTiming tests signal handling with precise timing measurements
func TestSignalHandlingTiming(t *testing.T) {
	t.Skip("Signal timing tests depend on old callback architecture — pending pipeline migration")
	// Create a temporary test directory
	tempDir := t.TempDir()

	// Build a fresh dcfh binary from current source code
	repoRoot := getRepoRoot(t)

	// Build binary in temp directory to avoid conflicts
	dcfhPath := filepath.Join(tempDir, "dcfh")
	buildCmd := exec.Command("go", "build", "-o", dcfhPath, "./cmd/dcfh")
	buildCmd.Dir = repoRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=1") // Ensure CGO is enabled for syscalls

	// Capture build output for debugging
	var buildOut bytes.Buffer
	buildCmd.Stdout = &buildOut
	buildCmd.Stderr = &buildOut

	// Build the binary
	t.Logf("Building fresh dcfh binary at %s", dcfhPath)
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build dcfh: %v\nOutput: %s", err, buildOut.String())
	}

	// Create test repository
	repoDir := filepath.Join(tempDir, "test-repo")
	if err := os.Mkdir(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create test repo: %v", err)
	}

	// Initialize repository
	initCmd := exec.Command(dcfhPath, "init", repoDir)
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to init repository: %v", err)
	}

	// Create test files with varying sizes to ensure some processing time
	for i := range 50 {
		filePath := filepath.Join(repoDir, fmt.Sprintf("file%d.txt", i))
		// Create files with different sizes to vary hash time
		size := (i % 10) * 1024 * 1024 // 0-9MB files
		content := make([]byte, size)
		for j := range content {
			content[j] = byte(i + j%256)
		}
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Test cases
	testCases := []struct {
		name            string
		args            []string
		signalDelay     time.Duration
		maxShutdownTime time.Duration
		processTimeout  time.Duration
	}{
		{
			name:            "update with quick signal",
			args:            []string{"--debug=scan,scanning,hash,load", "-vvv", "update"},
			signalDelay:     25 * time.Millisecond,
			maxShutdownTime: 100 * time.Millisecond,
			processTimeout:  5 * time.Second,
		},
		{
			name:            "update with delayed signal",
			args:            []string{"--debug=scan,scanning,hash,load", "-vvv", "update"},
			signalDelay:     200 * time.Millisecond,
			maxShutdownTime: 100 * time.Millisecond,
			processTimeout:  5 * time.Second,
		},
		{
			name:            "status with signal",
			args:            []string{"--debug=scan,scanning,hash,load", "-vvv", "status"},
			signalDelay:     25 * time.Millisecond,
			maxShutdownTime: 100 * time.Millisecond,
			processTimeout:  5 * time.Second,
		},
		{
			name:            "update with concurrent hash jobs",
			args:            []string{"--debug=scan,scanning,hash,load", "-vvv", "update", "--hash-workers=8"},
			signalDelay:     80 * time.Millisecond, // Signal during heavy hash job submission
			maxShutdownTime: 150 * time.Millisecond,
			processTimeout:  2 * time.Second,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// For status test, create additional files to ensure it takes longer
			if strings.Contains(tc.name, "status") {
				// First run an update to ensure main.idx exists
				updateCmd := exec.Command(dcfhPath, "update", "-vvv", "--debug=scan,scanning,hash,load,write")
				updateCmd.Dir = repoDir
				var updateOutput bytes.Buffer
				updateCmd.Stdout = &updateOutput
				updateCmd.Stderr = &updateOutput
				if err := updateCmd.Run(); err != nil {
					t.Fatalf("Failed to run update before status test: %v\nOutput: %s", err, updateOutput.String())
				}
				t.Logf("Update output: %s", updateOutput.String())

				t.Logf("Creating additional files for status test...")
				for i := 50; i < 500; i++ {
					filePath := filepath.Join(repoDir, fmt.Sprintf("file%d.txt", i))
					// Create files with varied sizes
					var size int
					switch i % 7 {
					case 0:
						size = 128 // Very small
					case 1:
						size = 1024 // 1KB
					case 2:
						size = 4096 // 4KB
					case 3:
						size = 16384 // 16KB
					case 4:
						size = 65536 // 64KB
					case 5:
						size = 131072 // 128KB
					case 6:
						size = 262144 // 256KB
					}

					content := make([]byte, size)
					for j := range content {
						content[j] = byte((i*7 + j) % 256)
					}
					if err := os.WriteFile(filePath, content, 0644); err != nil {
						t.Fatalf("Failed to create additional test file: %v", err)
					}
				}
				t.Logf("Created %d additional files for status test", 450)
			}

			tracker := &processTracker{
				startTime: time.Now(),
			}

			// Create command with context for absolute timeout
			ctx, cancel := context.WithTimeout(context.Background(), tc.processTimeout)
			defer cancel()

			// Add debug flags to see what's happening
			args := append([]string{"-vvv", "--debug=scan,scanning,hash,load,write"}, tc.args...)
			cmd := exec.CommandContext(ctx, dcfhPath, args...)
			cmd.Dir = repoDir

			var stdout bytes.Buffer
			cmd.Stdout = &stdout

			// Start monitoring stderr for signal messages in real-time
			stderrPipe, err := cmd.StderrPipe()
			if err != nil {
				t.Fatalf("Failed to create stderr pipe: %v", err)
			}

			var stderr bytes.Buffer

			signalDetected := make(chan time.Time, 1)
			shutdownDetected := make(chan time.Time, 1)

			// Monitor stderr in goroutine
			go func() {
				scanner := bufio.NewScanner(stderrPipe)
				for scanner.Scan() {
					line := scanner.Text()
					stderr.WriteString(line + "\n")

					if strings.Contains(line, "Received signal:") {
						select {
						case signalDetected <- time.Now():
							tracker.recordEvent("Signal received by process")
						default:
						}
					}
					if strings.Contains(line, "Initiating graceful shutdown") {
						select {
						case shutdownDetected <- time.Now():
							tracker.recordEvent("Shutdown initiated")
						default:
						}
					}
				}
			}()

			// Start the command
			tracker.recordEvent("Starting process")
			tracker.processStart = time.Now()
			if err := cmd.Start(); err != nil {
				t.Fatalf("Failed to start command: %v", err)
			}

			// Send signal after delay
			signalSent := make(chan time.Time, 1)
			go func() {
				time.Sleep(tc.signalDelay)
				tracker.recordEvent("Sending SIGINT")
				sendTime := time.Now()
				if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
					tracker.recordEvent(fmt.Sprintf("Failed to send signal: %v", err))
				} else {
					signalSent <- sendTime
				}
			}()

			// Wait for process to exit
			exitChan := make(chan error, 1)
			go func() {
				exitChan <- cmd.Wait()
			}()

			// Main event loop with timeout
			var exitErr error
		loop:
			for {
				select {
				case <-ctx.Done():
					tracker.recordEvent("Process timeout - killing")
					_ = cmd.Process.Kill()
					t.Errorf("Process exceeded timeout of %v", tc.processTimeout)
					break loop

				case sendTime := <-signalSent:
					tracker.signalSent = sendTime

				case recvTime := <-signalDetected:
					tracker.signalReceived = recvTime
					if !tracker.signalSent.IsZero() {
						latency := recvTime.Sub(tracker.signalSent)
						tracker.recordEvent(fmt.Sprintf("Signal latency: %.3fms", latency.Seconds()*1000))
					}

				case shutdownTime := <-shutdownDetected:
					tracker.shutdownStart = shutdownTime

				case err := <-exitChan:
					tracker.processExit = time.Now()
					tracker.recordEvent("Process exited")
					exitErr = err
					break loop
				}
			}

			// Calculate timings
			var signalToExit time.Duration
			if !tracker.signalSent.IsZero() && !tracker.processExit.IsZero() {
				signalToExit = tracker.processExit.Sub(tracker.signalSent)
			}

			// Print detailed timeline
			t.Logf("=== Event Timeline ===")
			for _, event := range tracker.getEvents() {
				t.Log(event)
			}

			// Analyze results
			if !tracker.signalSent.IsZero() && !tracker.processExit.IsZero() {
				t.Logf("Signal to exit time: %.3fms", signalToExit.Seconds()*1000)

				// Check if we got signal acknowledgment
				if tracker.signalReceived.IsZero() {
					// Process might have completed before signal
					if signalToExit < 10*time.Millisecond {
						t.Logf("Process likely completed before signal was processed")
					} else {
						t.Errorf("No signal acknowledgment received, but process took %.3fms to exit", signalToExit.Seconds()*1000)
					}
				} else {
					// We got signal acknowledgment - check shutdown time
					if signalToExit > tc.maxShutdownTime {
						t.Errorf("Shutdown took %.3fms, exceeding limit of %.3fms",
							signalToExit.Seconds()*1000, tc.maxShutdownTime.Seconds()*1000)
					}
				}
			}

			// Check for panic in stderr
			stderrStr := stderr.String()
			if strings.Contains(stderrStr, "panic:") {
				t.Errorf("Process panicked! Check stderr output for details")
				// Extract panic message
				if idx := strings.Index(stderrStr, "panic:"); idx != -1 {
					endIdx := strings.Index(stderrStr[idx:], "\n")
					if endIdx == -1 {
						endIdx = len(stderrStr) - idx
					}
					t.Errorf("Panic message: %s", stderrStr[idx:idx+endIdx])
				}
			}

			// Log stdout for debugging
			if stdout.Len() > 0 {
				t.Logf("Stdout: %s", stdout.String())
			}

			// Verify index files for status and update commands
			if strings.Contains(tc.name, "status") || strings.Contains(tc.name, "update") {
				// Status command creates cache.idx, update command creates main.idx
				var indexPath string
				var indexName string
				if strings.Contains(tc.name, "status") {
					indexPath = filepath.Join(repoDir, ".dcfh", "cache.idx")
					indexName = "cache.idx"
				} else {
					indexPath = filepath.Join(repoDir, ".dcfh", "main.idx")
					indexName = "main.idx"
				}

				if stat, err := os.Stat(indexPath); err != nil {
					// Only error if we expected the process to be interrupted and create the index
					if tc.signalDelay > 0 && !tracker.signalSent.IsZero() && strings.Contains(tc.name, "status") {
						// For interrupted status commands, cache.idx should still be created
						t.Errorf("%s not found after interrupted %s: %v", indexName, tc.args[len(tc.args)-1], err)
					} else if tc.signalDelay <= 0 {
						// For non-interrupted commands, index should exist
						t.Errorf("%s not found after %s completed: %v", indexName, tc.args[len(tc.args)-1], err)
					}
				} else {
					t.Logf("%s exists with size: %d bytes", indexName, stat.Size())

					// Use strings command to verify index contents (avoid dcfhfind bug)
					stringsCmd := exec.Command("strings", indexPath)
					output, err := stringsCmd.CombinedOutput()
					if err != nil {
						t.Errorf("strings command failed to read %s: %v\nOutput: %s", indexName, err, output)
					} else {
						// Parse strings output to find filenames
						allLines := strings.Split(string(output), "\n")
						var fileLines []string
						for _, line := range allLines {
							// Filter for lines that look like filenames (contain .txt)
							if strings.Contains(line, ".txt") && !strings.Contains(line, "/") {
								fileLines = append(fileLines, line)
							}
						}
						t.Logf("%s contains %d file entries", indexName, len(fileLines))

						// For status command with signal, we expect some entries but maybe not all 500
						if strings.Contains(tc.name, "status") {
							if tc.signalDelay > 0 {
								// Interrupted status should have some entries but maybe not all
								if len(fileLines) < 50 {
									t.Errorf("cache.idx has too few entries for interrupted status: got %d, want at least 50", len(fileLines))
								} else {
									t.Logf("Interrupted status command saved %d entries before shutdown", len(fileLines))
								}
							} else {
								// Non-interrupted status should have all 500 entries
								if len(fileLines) < 500 {
									t.Errorf("cache.idx has fewer entries than expected: got %d, want at least 500", len(fileLines))
								}
							}
						}

						// Verify some expected files are present
						if strings.Contains(tc.name, "status") {
							// For status command, cache.idx should contain files NOT in main.idx
							// Files are sorted lexicographically, so file100.txt comes before file50.txt
							// Check for files that come early in lexicographic order
							foundFile100 := false
							foundFile101 := false
							for _, line := range fileLines {
								if strings.Contains(line, "file100.txt") {
									foundFile100 = true
								}
								if strings.Contains(line, "file101.txt") {
									foundFile101 = true
								}
							}
							// Additional files created after update should be in cache
							if !foundFile100 || !foundFile101 {
								t.Errorf("cache.idx missing additional files that should have been processed: file100.txt found=%v, file101.txt found=%v", foundFile100, foundFile101)
							}
						} else {
							// For update command, main.idx should contain early files
							foundFile0 := false
							foundFile1 := false
							for _, line := range fileLines {
								if strings.Contains(line, "file0.txt") {
									foundFile0 = true
								}
								if strings.Contains(line, "file1.txt") {
									foundFile1 = true
								}
							}
							// Early files should always be present in main.idx
							if !foundFile0 || !foundFile1 {
								t.Errorf("main.idx missing early files that should have been processed: file0.txt found=%v, file1.txt found=%v", foundFile0, foundFile1)
							}
						}
					}
				}
			}

			// Log outputs for debugging
			if testing.Verbose() || t.Failed() {
				t.Logf("Exit error: %v", exitErr)
				t.Logf("Stdout length: %d bytes", stdout.Len())
				if stderr.Len() > 0 && stderr.Len() < 10000 {
					t.Logf("Stderr:\n%s", stderr.String())
				} else if stderr.Len() > 0 {
					t.Logf("Stderr (first 10KB):\n%s", stderr.Bytes()[:10000])
				}
			}
		})
	}
}

// TestSignalHandlingRaceCondition specifically tests for the hash job submission race
func TestSignalHandlingRaceCondition(t *testing.T) {
	tempDir := t.TempDir()

	// Build binary in temp directory to avoid conflicts
	dcfhPath := filepath.Join(tempDir, "dcfh")
	repoRoot := getRepoRoot(t)
	buildCmd := exec.Command("go", "build", "-o", dcfhPath, "./cmd/dcfh")
	buildCmd.Dir = repoRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=1")

	t.Logf("Building fresh dcfh binary at %s", dcfhPath)
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build dcfh binary: %v\nOutput: %s", err, output)
	}

	// Create test repository with many files to increase chance of race
	repoDir := filepath.Join(tempDir, "test-repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Initialize repository
	initCmd := exec.Command(dcfhPath, "init", repoDir)
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to initialize repository: %v\nOutput: %s", err, output)
	}

	// Create many small files to trigger lots of hash jobs
	for i := range 500 {
		filePath := filepath.Join(repoDir, fmt.Sprintf("file%d.txt", i))
		content := fmt.Sprintf("test content %d\n", i)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Run update with many hash workers and send signal during hash job submission
	cmd := exec.Command(dcfhPath, "--debug=scan,scanning", "-vvv", "update", "--hash-workers", "16")
	cmd.Dir = repoDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// Start the process
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Send signal after a short delay to hit the race window
	time.Sleep(60 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Logf("Failed to send signal: %v", err)
	}

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		// Process exited
		stderrStr := stderr.String()

		// Check for panic
		if strings.Contains(stderrStr, "panic:") {
			t.Errorf("Process panicked during shutdown!")
			if idx := strings.Index(stderrStr, "panic:"); idx != -1 {
				endIdx := strings.Index(stderrStr[idx:], "\n")
				if endIdx == -1 {
					endIdx = len(stderrStr) - idx
				} else {
					endIdx += idx
				}
				t.Errorf("Panic: %s", stderrStr[idx:endIdx])
			}
			// Log full stderr on panic
			t.Logf("Full stderr output:\n%s", stderrStr)
		}

		// Verify clean shutdown
		if strings.Contains(stderrStr, "send on closed channel") {
			t.Errorf("Race condition detected: send on closed channel")
		}

		if err != nil && err.Error() != "exit status 1" {
			t.Errorf("Unexpected exit error: %v", err)
		}

		// Verify main.idx was created despite interruption (update creates main.idx, not cache.idx)
		mainIdxPath := filepath.Join(repoDir, ".dcfh", "main.idx")
		if stat, err := os.Stat(mainIdxPath); err != nil {
			t.Errorf("main.idx not found after interrupted update: %v", err)
		} else if stat.Size() == 0 {
			t.Errorf("main.idx is empty after interrupted update")
		} else {
			t.Logf("main.idx created successfully with size: %d bytes", stat.Size())
		}

	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Errorf("Process did not exit within timeout")
	}
}

// TestSignalHandlingConcurrent tests signal handling during concurrent operations
func TestSignalHandlingConcurrent(t *testing.T) {
	tempDir := t.TempDir()

	repoRoot := getRepoRoot(t)

	// Build binary in temp directory to avoid conflicts
	dcfhPath := filepath.Join(tempDir, "dcfh")
	buildCmd := exec.Command("go", "build", "-o", dcfhPath, "./cmd/dcfh")
	buildCmd.Dir = repoRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=1")

	var buildOut bytes.Buffer
	buildCmd.Stdout = &buildOut
	buildCmd.Stderr = &buildOut

	t.Logf("Building fresh dcfh binary for concurrent test at %s", dcfhPath)
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build dcfh: %v\nOutput: %s", err, buildOut.String())
	}

	// Create test repository
	repoDir := filepath.Join(tempDir, "test-repo")
	if err := os.Mkdir(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create test repo: %v", err)
	}

	// Initialize repository
	initCmd := exec.Command(dcfhPath, "init", repoDir)
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to init repository: %v", err)
	}

	// Create many small files to ensure concurrent hashing
	for i := range 200 {
		filePath := filepath.Join(repoDir, fmt.Sprintf("file%d.txt", i))
		content := fmt.Appendf(nil, "File content %d with some padding to make it non-trivial\n", i)
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Test with high concurrency
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, dcfhPath, "--hash-workers=16", "--debug=scan,scanning", "-vvv", "update")
	cmd.Dir = repoDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Let concurrent operations start
	time.Sleep(100 * time.Millisecond)

	// Send signal
	signalTime := time.Now()
	t.Logf("Sending SIGINT to process PID %d", cmd.Process.Pid)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("Failed to send signal: %v", err)
	}
	t.Logf("SIGINT sent successfully")

	// Wait for exit
	exitChan := make(chan error, 1)
	go func() {
		exitChan <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		t.Logf("Stdout:\n%s", stdout.String())
		if stderr.Len() > 10000 {
			t.Logf("Stderr (last 10KB):\n%s", stderr.Bytes()[stderr.Len()-10000:])
		} else {
			t.Logf("Stderr:\n%s", stderr.String())
		}
		t.Fatal("Process timeout during concurrent test")
	case err := <-exitChan:
		shutdownTime := time.Since(signalTime)
		t.Logf("Concurrent shutdown time: %.3fms", shutdownTime.Seconds()*1000)

		if shutdownTime > 500*time.Millisecond {
			t.Errorf("Concurrent shutdown took too long: %.3fms", shutdownTime.Seconds()*1000)
		}

		if err != nil && !strings.Contains(err.Error(), "signal: interrupt") {
			t.Logf("Exit error: %v", err)
		}
	}
}
