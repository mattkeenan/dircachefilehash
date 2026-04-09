package dircachefilehash

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestHashPoolBasic(t *testing.T) {
	// Create temp directory with a real file to hash
	testDir, err := os.MkdirTemp("", "hashpool-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(testDir)

	// Create a test file
	testFile := filepath.Join(testDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create DirectoryCache
	dc := NewDirectoryCache(testDir, testDir)
	defer dc.Close()

	// Create a BEScanEntry for the test file
	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("failed to stat test file: %v", err)
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(testFile, &stat); err != nil {
		t.Fatalf("failed to syscall.Stat: %v", err)
	}

	entry := NewBEScanEntry("hello.txt", info, &stat)

	// Set up channels
	input := make(chan *PipelineEntry, 1)
	output := make(chan *PipelineEntry, 1)
	pool := newHashPool(dc, input, output, 2)

	// Send entry
	input <- &PipelineEntry{
		Entry:     entry,
		SeqNum:    0,
		Operation: OpNewFile,
		NeedsHash: true,
	}
	close(input)

	// Run pool
	if err := pool.Run(context.Background()); err != nil {
		t.Fatalf("hash pool run failed: %v", err)
	}

	// Read result
	pe := <-output
	if pe == nil {
		t.Fatal("expected output entry, got nil")
	}
	if pe.SeqNum != 0 {
		t.Errorf("expected SeqNum 0, got %d", pe.SeqNum)
	}

	// Verify hash was set
	hashType, err := pe.Entry.HashType()
	if err != nil {
		t.Fatalf("failed to get hash type: %v", err)
	}
	if hashType == 0 {
		t.Error("hash type should not be 0 after hashing")
	}

	hashStr, err := pe.Entry.HashString()
	if err != nil {
		t.Fatalf("failed to get hash string: %v", err)
	}
	if hashStr == "" {
		t.Error("hash string should not be empty after hashing")
	}
}

func TestHashPoolContextCancellation(t *testing.T) {
	testDir, err := os.MkdirTemp("", "hashpool-cancel-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(testDir)

	dc := NewDirectoryCache(testDir, testDir)
	defer dc.Close()

	input := make(chan *PipelineEntry)
	output := make(chan *PipelineEntry, 10)
	pool := newHashPool(dc, input, output, 1)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- pool.Run(ctx)
	}()

	// Cancel immediately — worker should exit
	cancel()

	err = <-done
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestHashPoolMultipleFiles(t *testing.T) {
	testDir, err := os.MkdirTemp("", "hashpool-multi-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(testDir)

	dc := NewDirectoryCache(testDir, testDir)
	defer dc.Close()

	const nFiles = 10
	input := make(chan *PipelineEntry, nFiles)
	output := make(chan *PipelineEntry, nFiles)
	pool := newHashPool(dc, input, output, 4)

	// Create files and entries
	for i := range nFiles {
		name := filepath.Join(testDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(name, []byte(fmt.Sprintf("content %d", i)), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
		info, _ := os.Stat(name)
		var stat syscall.Stat_t
		syscall.Stat(name, &stat)

		entry := NewBEScanEntry(fmt.Sprintf("file%d.txt", i), info, &stat)
		input <- &PipelineEntry{
			Entry:     entry,
			SeqNum:    uint64(i),
			Operation: OpNewFile,
			NeedsHash: true,
		}
	}
	close(input)

	if err := pool.Run(context.Background()); err != nil {
		t.Fatalf("hash pool run failed: %v", err)
	}

	// Verify all entries came through with hashes
	seen := make(map[uint64]bool)
	for pe := range output {
		seen[pe.SeqNum] = true
		hashType, _ := pe.Entry.HashType()
		if hashType == 0 {
			t.Errorf("entry %d has no hash type", pe.SeqNum)
		}
	}

	if len(seen) != nFiles {
		t.Errorf("expected %d entries, got %d", nFiles, len(seen))
	}
}

func TestHashPoolClosesOutput(t *testing.T) {
	input := make(chan *PipelineEntry)
	output := make(chan *PipelineEntry, 10)

	testDir, _ := os.MkdirTemp("", "hashpool-close-*")
	defer os.RemoveAll(testDir)
	dc := NewDirectoryCache(testDir, testDir)
	defer dc.Close()

	pool := newHashPool(dc, input, output, 1)
	close(input)

	if err := pool.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Output should be closed
	select {
	case _, ok := <-output:
		if ok {
			t.Error("output should be closed")
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for output close")
	}
}
