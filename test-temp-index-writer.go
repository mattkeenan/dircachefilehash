package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	
	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

func main() {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "temp-index-writer-test-*")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir)
	
	fmt.Printf("Testing TempIndexWriter in: %s\n", tempDir)
	
	// Create temp index file
	tempIndexPath := filepath.Join(tempDir, "test-temp.idx")
	
	// Create a DirectoryCache for the constructor
	dc := dcfh.NewDirectoryCache(tempDir, tempDir)
	defer dc.Close()
	
	// Create TempIndexWriter
	writer, err := dcfh.NewTempIndexWriter(dc, tempIndexPath)
	if err != nil {
		fmt.Printf("Failed to create TempIndexWriter: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("Created TempIndexWriter for: %s\n", tempIndexPath)
	
	// Create a simple test entry
	testData := make([]byte, 256)  // binaryEntry size
	copy(testData[:4], "test")     // Add some test data
	
	// Create IoVec
	iovec := syscall.Iovec{
		Base: &testData[0],
		Len:  uint64(len(testData)),
	}
	
	// Write the entry
	fmt.Printf("Writing test entry...\n")
	err = writer.WriteIoVecBatch([]syscall.Iovec{iovec})
	if err != nil {
		fmt.Printf("Failed to write entry: %v\n", err)
		os.Exit(1)
	}
	
	// Close the writer
	fmt.Printf("Closing TempIndexWriter...\n")
	err = writer.Close()
	if err != nil {
		fmt.Printf("Failed to close TempIndexWriter: %v\n", err)
		os.Exit(1)
	}
	
	// Check the file size
	stat, err := os.Stat(tempIndexPath)
	if err != nil {
		fmt.Printf("Failed to stat temp index file: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("Temp index file size: %d bytes\n", stat.Size())
	
	expectedSize := 88 + 256  // header + one entry
	if stat.Size() == int64(expectedSize) {
		fmt.Printf("✅ SUCCESS: File has expected size (%d bytes = 88 header + 256 entry)\n", expectedSize)
	} else if stat.Size() == 88 {
		fmt.Printf("❌ FAILURE: File only contains header (88 bytes), no entries written\n")
		os.Exit(1)
	} else {
		fmt.Printf("❌ UNEXPECTED: File size %d bytes doesn't match expected %d bytes\n", stat.Size(), expectedSize)
		os.Exit(1)
	}
}