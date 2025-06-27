package main

import (
	"fmt"
	"os"
	"path/filepath"
	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

func main() {
	// Create test directory
	testDir, err := os.MkdirTemp("", "dcfh-debug-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(testDir)
	
	// Create a test file
	testFile := filepath.Join(testDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		panic(err)
	}
	
	fmt.Printf("Test directory: %s\n", testDir)
	fmt.Printf("Created test file: %s\n", testFile)
	
	// Initialize dcfh
	dc := dcfh.NewDirectoryCache(testDir, "")
	
	// Set verbose level 3 for trace logging
	dcfh.SetVerboseLevel(3)
	
	// Check what the scan finds
	fmt.Printf("Checking scan result...\n")
	
	// Check if .dcfhignore exists
	ignoreFile := filepath.Join(testDir, ".dcfhignore")
	if _, err := os.Stat(ignoreFile); err == nil {
		fmt.Printf("Found .dcfhignore file\n")
	} else {
		fmt.Printf("No .dcfhignore file\n")
	}
	
	// Test direct file scan first
	fmt.Printf("Files in directory:\n")
	entries, err := os.ReadDir(testDir)
	if err != nil {
		fmt.Printf("ReadDir error: %v\n", err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			fmt.Printf("  %s\n", entry.Name())
		}
	}
	
	// Run status
	result, err := dc.Status(map[string]string{})
	if err != nil {
		fmt.Printf("Status error: %v\n", err)
		return
	}
	
	fmt.Printf("Status result:\n")
	fmt.Printf("  Added: %d files\n", len(result.Added))
	fmt.Printf("  Modified: %d files\n", len(result.Modified))
	fmt.Printf("  Deleted: %d files\n", len(result.Deleted))
	
	if len(result.Added) > 0 {
		fmt.Printf("  Added files: %v\n", result.Added)
	}
	
	// Check if cache file was created
	cacheFile := filepath.Join(testDir, ".dcfh", "cache.idx")
	if stat, err := os.Stat(cacheFile); err == nil {
		fmt.Printf("Cache file created: %d bytes\n", stat.Size())
	} else {
		fmt.Printf("Cache file not created: %v\n", err)
	}
	
	// Check main index too
	mainFile := filepath.Join(testDir, ".dcfh", "main.idx")
	if stat, err := os.Stat(mainFile); err == nil {
		fmt.Printf("Main index file: %d bytes\n", stat.Size())
	} else {
		fmt.Printf("Main index not created: %v\n", err)
	}
}