package main

import (
	"fmt"
	"os"
	"path/filepath"
	
	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

func main() {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "callback-workflow-test-*")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir)
	
	fmt.Printf("Testing Callback workflow in: %s\n", tempDir)
	
	// Create a DirectoryCache 
	dc := dcfh.NewDirectoryCache(tempDir, tempDir)
	defer dc.Close()
	
	// Create some test files
	testFile1 := filepath.Join(tempDir, "file1.txt")
	testFile2 := filepath.Join(tempDir, "file2.txt")
	
	err = os.WriteFile(testFile1, []byte("hello world"), 0644)
	if err != nil {
		fmt.Printf("Failed to create test file 1: %v\n", err)
		os.Exit(1)
	}
	
	err = os.WriteFile(testFile2, []byte("different content"), 0644)
	if err != nil {
		fmt.Printf("Failed to create test file 2: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("Created test files: file1.txt, file2.txt\n")
	
	// Initialize dcfh repository
	err = os.MkdirAll(filepath.Join(tempDir, ".dcfh"), 0755)
	if err != nil {
		fmt.Printf("Failed to create .dcfh directory: %v\n", err)
		os.Exit(1)
	}
	
	// Test the Update workflow with debug enabled
	fmt.Printf("Testing Update workflow with debug=hash...\n")
	shutdownChan := make(chan struct{})
	flags := make(map[string]string)
	flags["debug"] = "scan,scanning,load,hash,write"
	flags["verbose"] = "3"
	
	err = dc.Update(shutdownChan, flags)
	if err != nil {
		fmt.Printf("Update failed: %v\n", err)
		os.Exit(1)
	}
	
	// Check main.idx
	mainIdxPath := filepath.Join(tempDir, ".dcfh", "main.idx")
	stat, err := os.Stat(mainIdxPath)
	if err != nil {
		fmt.Printf("main.idx not found: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("main.idx size: %d bytes\n", stat.Size())
	
	if stat.Size() == 88 {
		fmt.Printf("❌ FAILURE: main.idx only contains header (88 bytes), no entries written\n")
		os.Exit(1)
	} else if stat.Size() > 88 {
		fmt.Printf("✅ SUCCESS: main.idx contains header + entries (%d bytes)\n", stat.Size())
		
		// Expected: 88 (header) + 2*256 (two entries) = 600 bytes
		expectedSize := 88 + 2*256
		if stat.Size() == int64(expectedSize) {
			fmt.Printf("✅ PERFECT: Exact expected size for 2 entries\n")
		} else {
			fmt.Printf("⚠️  Note: Size %d doesn't match expected %d (may be normal due to alignment)\n", stat.Size(), expectedSize)
		}
	} else {
		fmt.Printf("❌ UNEXPECTED: main.idx size %d bytes is less than header size\n", stat.Size())
		os.Exit(1)
	}
}