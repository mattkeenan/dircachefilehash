package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

func main() {
	// Check if directory argument was provided
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <directory>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  %s /home/user/documents\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s .\n", os.Args[0])
		os.Exit(1)
	}

	directory := os.Args[1]

	// Convert to absolute path
	absDir, err := filepath.Abs(directory)
	if err != nil {
		log.Fatalf("Failed to get absolute path for %s: %v", directory, err)
	}

	// Check if directory exists and is actually a directory
	info, err := os.Stat(absDir)
	if err != nil {
		log.Fatalf("Failed to access directory %s: %v", absDir, err)
	}
	if !info.IsDir() {
		log.Fatalf("%s is not a directory", absDir)
	}

	// Create index filename in .dcfh subdirectory of the target directory
	indexFile := filepath.Join(absDir, ".dcfh", "index")

	fmt.Printf("Indexing directory: %s\n", absDir)
	fmt.Printf("Index file: %s\n", indexFile)

	// Create a new directory cache
	cache := dcfh.NewDirectoryCache(absDir, absDir)

	// Scan directory and create index
	fmt.Println("Scanning directory...")
	if err := cache.Update(); err != nil {
		log.Fatalf("Failed to create index: %v", err)
	}

	// Get statistics
	fileCount, totalSize, _ := cache.Stats()
	fmt.Printf("✓ Successfully indexed %d files, total size: %d bytes\n", fileCount, totalSize)

	// Show index length
	fmt.Printf("Index contains %d entries\n", cache.Length())

	// Check for duplicates
	duplicates, err := cache.FindDuplicates()
	if err != nil {
		log.Fatalf("Failed to find duplicates: %v", err)
	}
	
	if len(duplicates) > 0 {
		fmt.Printf("\n⚠ Found %d sets of duplicate files:\n", len(duplicates))
		count := 0
		for _, group := range duplicates {
			if count >= 3 { // Show only first 3 duplicate sets
				fmt.Printf("  ... and %d more duplicate sets\n", len(duplicates)-3)
				break
			}
			fmt.Printf("  Hash %s... has %d duplicates:\n", group.Hash[:8], group.Count)
			for _, file := range group.Files {
				fmt.Printf("    %s\n", file)
			}
			count++
		}
	} else {
		fmt.Println("\n✓ No duplicate files found")
	}
}
