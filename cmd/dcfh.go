package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// Global flags
var (
	jsonOutput = flag.Bool("json", false, "output in JSON format")
	verbose    = flag.Bool("verbose", false, "verbose output")
	version    = flag.Bool("version", false, "show version information")
)

// Command-specific flag sets
var (
	initFlags   = flag.NewFlagSet("init", flag.ExitOnError)
	statusFlags = flag.NewFlagSet("status", flag.ExitOnError)
	updateFlags = flag.NewFlagSet("update", flag.ExitOnError)
	dupesFlags  = flag.NewFlagSet("dupes", flag.ExitOnError)
)

// Output structures for JSON
type InitOutput struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	Repository  string `json:"repository"`
	FileCount   int    `json:"file_count"`
	TotalSize   int64  `json:"total_size"`
	TimeElapsed string `json:"time_elapsed,omitempty"`
}

type StatusOutput struct {
	Repository string        `json:"repository"`
	WorkingDir string        `json:"working_dir,omitempty"`
	Modified   []string      `json:"modified"`
	Added      []string      `json:"added"`
	Deleted    []string      `json:"deleted"`
	Summary    StatusSummary `json:"summary"`
	IndexInfo  IndexInfo     `json:"index_info"`
}

type StatusSummary struct {
	ModifiedCount int  `json:"modified_count"`
	AddedCount    int  `json:"added_count"`
	DeletedCount  int  `json:"deleted_count"`
	HasChanges    bool `json:"has_changes"`
}

type IndexInfo struct {
	FileCount int `json:"file_count"`
}

type UpdateOutput struct {
	Success      bool           `json:"success"`
	Message      string         `json:"message"`
	Repository   string         `json:"repository"`
	PathsUpdated []string       `json:"paths_updated,omitempty"`
	FileCount    int            `json:"file_count"`
	TotalSize    int64          `json:"total_size"`
	TimeElapsed  string         `json:"time_elapsed"`
	Duplicates   *DuplicateInfo `json:"duplicates,omitempty"`
}

type DuplicateInfo struct {
	SetCount  int `json:"set_count"`
	FileCount int `json:"file_count"`
}

type DupesOutput struct {
	Repository      string                `json:"repository"`
	DuplicateGroups []dcfh.DuplicateGroup `json:"duplicate_groups"`
	Summary         DuplicateSummary      `json:"summary"`
}

type DuplicateSummary struct {
	GroupCount int `json:"group_count"`
	FileCount  int `json:"file_count"`
}

type ErrorOutput struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

func showUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh [GLOBAL_OPTIONS] <command> [COMMAND_OPTIONS]\n\n")
	fmt.Fprintf(os.Stderr, "Global Options:\n")
	fmt.Fprintf(os.Stderr, "  --json           Output in JSON format\n")
	fmt.Fprintf(os.Stderr, "  --verbose        Verbose output\n")
	fmt.Fprintf(os.Stderr, "  --version        Show version information\n")
	fmt.Fprintf(os.Stderr, "  --help           Show this help message\n")
	fmt.Fprintf(os.Stderr, "\nCommands:\n")
	fmt.Fprintf(os.Stderr, "  init <dir>       Initialize a new dcfh repository in the specified directory\n")
	fmt.Fprintf(os.Stderr, "  status           Show the status of files in the current dcfh repository\n")
	fmt.Fprintf(os.Stderr, "  update [paths...] Update the index with current file states\n")
	fmt.Fprintf(os.Stderr, "  dupes            Find and display duplicate files\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh init .\n")
	fmt.Fprintf(os.Stderr, "  dcfh init /home/user/documents\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json status\n")
	fmt.Fprintf(os.Stderr, "  dcfh update\n")
	fmt.Fprintf(os.Stderr, "  dcfh update file.txt dir/\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json dupes\n")
}

func main() {
	// Set up global flags
	flag.Usage = showUsage

	// Parse global flags first
	flag.Parse()

	// Handle version flag
	if *version {
		handleVersion()
		return
	}

	// Get remaining arguments after global flags
	args := flag.Args()
	if len(args) < 1 {
		showUsage()
		os.Exit(1)
	}

	command := args[0]

	switch command {
	case "init":
		handleInit(args[1:])
	case "status":
		handleStatus(args[1:])
	case "update":
		handleUpdate(args[1:])
	case "dupes":
		handleDupes(args[1:])
	default:
		outputError(fmt.Sprintf("Unknown command: %s", command))
		os.Exit(1)
	}
}

// buildFlags creates a flags map from global CLI flags
func buildFlags() map[string]string {
	flags := make(map[string]string)
	
	if *verbose {
		flags["v"] = "1" // Basic verbose level
	}
	
	return flags
}

func handleVersion() {
	fmt.Println("dcfh version 1.0.0")
	fmt.Println("Directory Cache File Hash - A fast file indexing and duplicate detection tool")
}

func outputError(message string) {
	if *jsonOutput {
		errorOut := ErrorOutput{
			Success: false,
			Error:   message,
		}
		json.NewEncoder(os.Stderr).Encode(errorOut)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	}
}

func outputJSON(data interface{}) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.Encode(data)
}

func handleInit(args []string) {
	if len(args) != 1 {
		outputError("Usage: dcfh init <directory>")
		os.Exit(1)
	}

	directory := args[0]
	start := time.Now()

	// Convert to absolute path
	absDir, err := filepath.Abs(directory)
	if err != nil {
		outputError(fmt.Sprintf("Failed to get absolute path for %s: %v", directory, err))
		os.Exit(1)
	}

	// Check if directory exists
	info, err := os.Stat(absDir)
	if err != nil {
		outputError(fmt.Sprintf("Failed to access directory %s: %v", absDir, err))
		os.Exit(1)
	}
	if !info.IsDir() {
		outputError(fmt.Sprintf("%s is not a directory", absDir))
		os.Exit(1)
	}

	// Check if .dcfh already exists
	dcfhDir := filepath.Join(absDir, ".dcfh")
	if _, err := os.Stat(dcfhDir); err == nil {
		outputError(fmt.Sprintf(".dcfh directory already exists in %s", absDir))
		os.Exit(1)
	}

	// Create cache - this will automatically create .dcfh directory and index
	cache := dcfh.NewDirectoryCache(absDir, absDir)

	if !*jsonOutput && *verbose {
		fmt.Printf("Initialized empty dcfh repository in %s\n", dcfhDir)
		fmt.Println("Scanning directory and creating initial index...")
	}

	if err := cache.Update(buildFlags()); err != nil {
		outputError(fmt.Sprintf("Failed to create initial index: %v", err))
		os.Exit(1)
	}

	duration := time.Since(start)
	fileCount, totalSize, _ := cache.Stats()

	if *jsonOutput {
		output := InitOutput{
			Success:     true,
			Message:     "Successfully initialized dcfh repository",
			Repository:  absDir,
			FileCount:   fileCount,
			TotalSize:   totalSize,
			TimeElapsed: duration.Round(time.Millisecond).String(),
		}
		outputJSON(output)
	} else {
		fmt.Printf("Initialized empty dcfh repository in %s\n", dcfhDir)
		fmt.Printf("✓ Successfully indexed %d files, total size: %d bytes\n", fileCount, totalSize)
		if *verbose {
			fmt.Printf("✓ Completed in %v\n", duration.Round(time.Millisecond))
		}
	}
}

func handleStatus(args []string) {
	if len(args) != 0 {
		outputError("Usage: dcfh status")
		os.Exit(1)
	}

	// Find the dcfh repository root
	repoRoot, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		if !*jsonOutput {
			fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialize a repository\n")
		}
		os.Exit(1)
	}

	// Get current working directory relative to repo root
	cwd, _ := os.Getwd()
	relCwd, _ := filepath.Rel(repoRoot, cwd)
	if relCwd == "." {
		relCwd = ""
	}

	// Create cache and get status
	cache := dcfh.NewDirectoryCache(repoRoot, dcfhDir)
	defer cache.CleanupTempFilesOnExit() // Ensure cleanup of any temp files

	status, err := cache.Status(buildFlags())
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}

	if *jsonOutput {
		fileCount := cache.Length() // Use Length() method
		output := StatusOutput{
			Repository: repoRoot,
			WorkingDir: relCwd,
			Modified:   status.Modified,
			Added:      status.Added,
			Deleted:    status.Deleted,
			Summary: StatusSummary{
				ModifiedCount: len(status.Modified),
				AddedCount:    len(status.Added),
				DeletedCount:  len(status.Deleted),
				HasChanges:    status.HasChanges(),
			},
			IndexInfo: IndexInfo{
				FileCount: fileCount,
			},
		}
		outputJSON(output)
		return
	}

	// Text output
	fmt.Printf("On branch main\n")
	if relCwd != "" {
		fmt.Printf("Working directory: %s\n", relCwd)
	}
	fmt.Printf("Repository root: %s\n", repoRoot)
	fmt.Println()

	// Show status
	if !status.HasChanges() {
		fmt.Println("Nothing to commit, working tree clean")
		fileCount := cache.Length() // Use Length() method
		fmt.Printf("Index contains %d files\n", fileCount)
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

func handleUpdate(args []string) {
	// Find the dcfh repository root
	repoRoot, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		if !*jsonOutput {
			fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialize a repository\n")
		}
		os.Exit(1)
	}

	// Get paths to update (if any)
	var paths []string
	if len(args) > 0 {
		paths = args
		if !*jsonOutput {
			fmt.Printf("Updating specified paths in %s\n", repoRoot)
			if *verbose {
				for _, path := range paths {
					fmt.Printf("  %s\n", path)
				}
			}
		}
	} else {
		if !*jsonOutput {
			fmt.Printf("Updating entire repository in %s\n", repoRoot)
		}
	}

	// Update the index
	cache := dcfh.NewDirectoryCache(repoRoot, dcfhDir)
	defer cache.CleanupTempFilesOnExit() // Ensure cleanup of any temp files

	if !*jsonOutput && *verbose {
		fmt.Println("Scanning directory...")
	}

	start := time.Now()

	if err := cache.Update(buildFlags(), paths...); err != nil {
		outputError(fmt.Sprintf("Failed to update index: %v", err))
		os.Exit(1)
	}

	duration := time.Since(start)
	fileCount, totalSize, _ := cache.Stats()

	// Check for duplicates
	var duplicateInfo *DuplicateInfo
	duplicates, err := cache.FindDuplicates(buildFlags())
	if err != nil {
		outputError(fmt.Sprintf("Failed to find duplicates: %v", err))
		os.Exit(1)
	}

	if len(duplicates) > 0 {
		duplicateCount := 0
		for _, group := range duplicates {
			duplicateCount += len(group.Files)
		}
		duplicateInfo = &DuplicateInfo{
			SetCount:  len(duplicates),
			FileCount: duplicateCount,
		}
	}

	if *jsonOutput {
		output := UpdateOutput{
			Success:      true,
			Message:      "Successfully updated index",
			Repository:   repoRoot,
			PathsUpdated: paths,
			FileCount:    fileCount,
			TotalSize:    totalSize,
			TimeElapsed:  duration.Round(time.Millisecond).String(),
			Duplicates:   duplicateInfo,
		}
		outputJSON(output)
	} else {
		fmt.Printf("✓ Updated index in %v\n", duration.Round(time.Millisecond))
		fmt.Printf("✓ Indexed %d files, total size: %d bytes\n", fileCount, totalSize)

		if duplicateInfo != nil {
			fmt.Printf("⚠ Found %d duplicate files in %d sets\n", duplicateInfo.FileCount, duplicateInfo.SetCount)
		}
	}
}

func handleDupes(args []string) {
	if len(args) != 0 {
		outputError("Usage: dcfh dupes")
		os.Exit(1)
	}

	// Find the dcfh repository root
	repoRoot, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		if !*jsonOutput {
			fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialize a repository\n")
		}
		os.Exit(1)
	}

	// Load existing index
	cache := dcfh.NewDirectoryCache(repoRoot, dcfhDir)
	defer cache.CleanupTempFilesOnExit() // Ensure cleanup of any temp files

	if _, err := cache.LoadMainIndex(); err != nil {
		outputError(fmt.Sprintf("Failed to load index: %v", err))
		os.Exit(1)
	}

	// Find duplicates
	duplicates, err := cache.FindDuplicates(buildFlags())
	if err != nil {
		outputError(fmt.Sprintf("Failed to find duplicates: %v", err))
		os.Exit(1)
	}

	if len(duplicates) == 0 {
		if *jsonOutput {
			output := DupesOutput{
				Repository:      repoRoot,
				DuplicateGroups: []dcfh.DuplicateGroup{},
				Summary: DuplicateSummary{
					GroupCount: 0,
					FileCount:  0,
				},
			}
			outputJSON(output)
		}
		return // No output if no duplicates found (like fdupes in text mode)
	}

	// Sort groups by hash for consistent output
	for i := 0; i < len(duplicates); i++ {
		for j := i + 1; j < len(duplicates); j++ {
			if duplicates[i].Hash > duplicates[j].Hash {
				duplicates[i], duplicates[j] = duplicates[j], duplicates[i]
			}
		}
	}

	if *jsonOutput {
		var groups []dcfh.DuplicateGroup
		totalFiles := 0

		for _, group := range duplicates {
			// Use relative paths directly (like git)
			var filePaths []string
			for _, relPath := range group.Files {
				filePaths = append(filePaths, relPath)
			}

			// Sort the file paths
			for k := 0; k < len(filePaths); k++ {
				for l := k + 1; l < len(filePaths); l++ {
					if filePaths[k] > filePaths[l] {
						filePaths[k], filePaths[l] = filePaths[l], filePaths[k]
					}
				}
			}

			groups = append(groups, dcfh.DuplicateGroup{
				Hash:  group.Hash,
				Files: filePaths,
				Count: len(filePaths),
			})

			totalFiles += len(filePaths)
		}

		output := DupesOutput{
			Repository:      repoRoot,
			DuplicateGroups: groups,
			Summary: DuplicateSummary{
				GroupCount: len(duplicates),
				FileCount:  totalFiles,
			},
		}
		outputJSON(output)
	} else {
		// Text output (fdupes format) with relative paths
		for i, group := range duplicates {
			// Use relative paths directly and sort them
			var filePaths []string
			for _, relPath := range group.Files {
				filePaths = append(filePaths, relPath)
			}

			// Sort the file paths
			for k := 0; k < len(filePaths); k++ {
				for l := k + 1; l < len(filePaths); l++ {
					if filePaths[k] > filePaths[l] {
						filePaths[k], filePaths[l] = filePaths[l], filePaths[k]
					}
				}
			}

			// Print each file in the duplicate group (relative paths)
			for _, relPath := range filePaths {
				fmt.Println(relPath)
			}

			// Add blank line between groups (except after the last group)
			if i < len(duplicates)-1 {
				fmt.Println()
			}
		}
	}
}

// findDcfhRepo searches for .dcfh directory starting from current directory
// and moving up the directory tree
// Returns: (repoRoot, dcfhDir, error)
func findDcfhRepo() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current directory: %w", err)
	}

	dir := cwd
	for {
		dcfhPath := filepath.Join(dir, ".dcfh")
		indexFile := filepath.Join(dcfhPath, "main.idx")

		if info, err := os.Stat(dcfhPath); err == nil && info.IsDir() {
			if _, err := os.Stat(indexFile); err == nil {
				return dir, dir, nil // repoRoot and dcfhDir are the same
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
