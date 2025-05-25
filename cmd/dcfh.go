package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

func main() {
	if len(os.Args) < 2 {
		showUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		handleInit()
	case "status":
		handleStatus()
	case "update":
		handleUpdate()
	case "dupes":
		handleDupes()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		showUsage()
		os.Exit(1)
	}
}

func showUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh <command>\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  init <dir>    Initialize a new dcfh repository in the specified directory\n")
	fmt.Fprintf(os.Stderr, "  status        Show the status of files in the current dcfh repository\n")
	fmt.Fprintf(os.Stderr, "  update        Update the index with current file states\n")
	fmt.Fprintf(os.Stderr, "  dupes         Find and display duplicate files\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh init .\n")
	fmt.Fprintf(os.Stderr, "  dcfh init /home/user/documents\n")
	fmt.Fprintf(os.Stderr, "  dcfh status\n")
	fmt.Fprintf(os.Stderr, "  dcfh update\n")
	fmt.Fprintf(os.Stderr, "  dcfh dupes\n")
}

func handleInit() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: dcfh init <directory>\n")
		os.Exit(1)
	}

	directory := os.Args[2]

	// Convert to absolute path
	absDir, err := filepath.Abs(directory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to get absolute path for %s: %v\n", directory, err)
		os.Exit(1)
	}

	// Check if directory exists
	info, err := os.Stat(absDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to access directory %s: %v\n", absDir, err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %s is not a directory\n", absDir)
		os.Exit(1)
	}

	// Check if .dcfh already exists
	dcfhDir := filepath.Join(absDir, ".dcfh")
	if _, err := os.Stat(dcfhDir); err == nil {
		fmt.Fprintf(os.Stderr, "Error: .dcfh directory already exists in %s\n", absDir)
		os.Exit(1)
	}

	// Create .dcfh directory
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to create .dcfh directory: %v\n", err)
		os.Exit(1)
	}

	// Initialize index
	indexFile := filepath.Join(dcfhDir, "index")
	cache := dcfh.NewDirectoryCache(absDir, indexFile)

	fmt.Printf("Initialized empty dcfh repository in %s\n", dcfhDir)
	fmt.Println("Scanning directory and creating initial index...")

	if err := cache.Update(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to create initial index: %v\n", err)
		os.Exit(1)
	}

	fileCount, totalSize, _ := cache.Stats()
	fmt.Printf("✓ Successfully indexed %d files, total size: %d bytes\n", fileCount, totalSize)
}

func handleStatus() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: dcfh status\n")
		os.Exit(1)
	}

	// Find the dcfh repository root
	repoRoot, indexFile, err := findDcfhRepo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialize a repository\n")
		os.Exit(1)
	}

	// Get current working directory relative to repo root
	cwd, _ := os.Getwd()
	relCwd, _ := filepath.Rel(repoRoot, cwd)
	if relCwd == "." {
		relCwd = ""
	}

	fmt.Printf("On branch main\n")
	if relCwd != "" {
		fmt.Printf("Working directory: %s\n", relCwd)
	}
	fmt.Printf("Repository root: %s\n", repoRoot)
	fmt.Println()

	// Create cache and get status
	cache := dcfh.NewDirectoryCache(repoRoot, indexFile)
	status, err := cache.Status()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Show status
	if !status.HasChanges() {
		fmt.Println("Nothing to commit, working tree clean")
		// Get entry count from loaded index
		entries := cache.GetEntries()
		fmt.Printf("Index contains %d files\n", len(entries))
		return
	}

	if len(status.Modified) > 0 {
		fmt.Println("Changes not staged for commit:")
		fmt.Println("  (use \"dcfh update\" to update the index)")
		fmt.Println()
		for _, path := range status.Modified {
			fmt.Printf("\tmodified:   %s\n", path)
		}
		fmt.Println()
	}

	if len(status.Added) > 0 {
		fmt.Println("Untracked files:")
		fmt.Println("  (use \"dcfh update\" to include in what will be committed)")
		fmt.Println()
		for _, path := range status.Added {
			fmt.Printf("\t%s\n", path)
		}
		fmt.Println()
	}

	if len(status.Deleted) > 0 {
		fmt.Println("Changes not staged for commit:")
		fmt.Println("  (use \"dcfh update\" to update the index)")
		fmt.Println()
		for _, path := range status.Deleted {
			fmt.Printf("\tdeleted:    %s\n", path)
		}
		fmt.Println()
	}

	fmt.Printf("Summary: %d modified, %d added, %d deleted\n",
		len(status.Modified), len(status.Added), len(status.Deleted))
}

func handleUpdate() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: dcfh update\n")
		os.Exit(1)
	}

	// Find the dcfh repository root
	repoRoot, indexFile, err := findDcfhRepo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialize a repository\n")
		os.Exit(1)
	}

	fmt.Printf("Updating index in %s\n", repoRoot)

	// Update the index
	cache := dcfh.NewDirectoryCache(repoRoot, indexFile)

	fmt.Println("Scanning directory...")
	start := time.Now()

	if err := cache.Update(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to update index: %v\n", err)
		os.Exit(1)
	}

	duration := time.Since(start)
	fileCount, totalSize, _ := cache.Stats()

	fmt.Printf("✓ Updated index in %v\n", duration.Round(time.Millisecond))
	fmt.Printf("✓ Indexed %d files, total size: %d bytes\n", fileCount, totalSize)

	// Show some statistics
	entries := cache.GetEntries()
	if len(entries) > 0 {
		duplicates := cache.FindDuplicates()
		if len(duplicates) > 0 {
			duplicateCount := 0
			for _, files := range duplicates {
				duplicateCount += len(files)
			}
			fmt.Printf("⚠ Found %d duplicate files in %d sets\n", duplicateCount, len(duplicates))
		}
	}
}

func handleDupes() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: dcfh dupes\n")
		os.Exit(1)
	}

	// Find the dcfh repository root
	repoRoot, indexFile, err := findDcfhRepo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialize a repository\n")
		os.Exit(1)
	}

	// Load existing index
	cache := dcfh.NewDirectoryCache(repoRoot, indexFile)
	if err := cache.LoadIndex(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to load index: %v\n", err)
		os.Exit(1)
	}

	// Find duplicates
	duplicates := cache.FindDuplicates()

	if len(duplicates) == 0 {
		return // No output if no duplicates found, like fdupes
	}

	// Sort hashes for consistent output
	var hashes []string
	for hash := range duplicates {
		hashes = append(hashes, hash)
	}

	// Sort hashes to ensure deterministic output
	for i := 0; i < len(hashes); i++ {
		for j := i + 1; j < len(hashes); j++ {
			if hashes[i] > hashes[j] {
				hashes[i], hashes[j] = hashes[j], hashes[i]
			}
		}
	}

	// Print duplicates in fdupes format
	for i, hash := range hashes {
		files := duplicates[hash]

		// Sort files within each duplicate group by path
		for k := 0; k < len(files); k++ {
			for l := k + 1; l < len(files); l++ {
				if files[k].RelativePath > files[l].RelativePath {
					files[k], files[l] = files[l], files[k]
				}
			}
		}

		// Print each file in the duplicate group
		for _, file := range files {
			// Convert relative path to absolute path for display
			absPath := filepath.Join(repoRoot, file.RelativePath)
			fmt.Println(absPath)
		}

		// Add blank line between groups (except after the last group)
		if i < len(hashes)-1 {
			fmt.Println()
		}
	}
}

// findDcfhRepo searches for .dcfh directory starting from current directory
// and moving up the directory tree
func findDcfhRepo() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current directory: %w", err)
	}

	dir := cwd
	for {
		dcfhDir := filepath.Join(dir, ".dcfh")
		indexFile := filepath.Join(dcfhDir, "index")

		if info, err := os.Stat(dcfhDir); err == nil && info.IsDir() {
			if _, err := os.Stat(indexFile); err == nil {
				return dir, indexFile, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return "", "", fmt.Errorf("not a dcfh repository (or any of the parent directories)")
}
