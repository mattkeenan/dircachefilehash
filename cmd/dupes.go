package main

import (
	"fmt"
	"os"
	"path/filepath"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// showDupesUsage displays usage information for the dupes command
func showDupesUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh dupes\n\n")
	fmt.Fprintf(os.Stderr, "Find and display duplicate files in the repository.\n\n")
	fmt.Fprintf(os.Stderr, "Description:\n")
	fmt.Fprintf(os.Stderr, "  Analyzes the index to identify files with identical content (same hash)\n")
	fmt.Fprintf(os.Stderr, "  but different paths. Groups duplicate files and shows file counts and\n")
	fmt.Fprintf(os.Stderr, "  total duplicate space that could be reclaimed.\n\n")
	fmt.Fprintf(os.Stderr, "Output formats:\n")
	fmt.Fprintf(os.Stderr, "  --output=human       Human-readable grouped format (default)\n")
	fmt.Fprintf(os.Stderr, "  --output=json        JSON format with detailed group information\n")
	fmt.Fprintf(os.Stderr, "  --output=fdupes      fdupes-compatible format for scripting\n\n")
	fmt.Fprintf(os.Stderr, "Global options:\n")
	fmt.Fprintf(os.Stderr, "  --verbose, -v        Show additional information about duplicate groups\n")
	fmt.Fprintf(os.Stderr, "  --json               Output result in JSON format (alias for --output=json)\n\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh dupes                           # Find duplicates in human format\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json dupes                    # Find duplicates in JSON format\n")
	fmt.Fprintf(os.Stderr, "  dcfh --output=fdupes dupes           # fdupes-compatible output\n")
	fmt.Fprintf(os.Stderr, "  dcfh --verbose dupes                 # Show detailed duplicate information\n\n")
	fmt.Fprintf(os.Stderr, "Output information:\n")
	fmt.Fprintf(os.Stderr, "  Each group shows files with identical content\n")
	fmt.Fprintf(os.Stderr, "  Group size indicates number of duplicate copies\n")
	fmt.Fprintf(os.Stderr, "  Summary shows total groups and potential space savings\n\n")
	fmt.Fprintf(os.Stderr, "Notes:\n")
	fmt.Fprintf(os.Stderr, "  Requires up-to-date index - run 'dcfh update' if files have changed\n")
	fmt.Fprintf(os.Stderr, "  Only finds duplicates of indexed files (ignores .dcfhignore patterns)\n")
}

// handleDupes handles the "dcfh dupes" command
func handleDupes(args []string) {
	// Check for help request
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		showDupesUsage()
		return
	}
	
	if len(args) != 0 {
		outputError("Usage: dcfh dupes")
		outputError("Use 'dcfh dupes --help' for detailed usage information")
		os.Exit(1)
	}

	// Find the dcfh repository root
	repoRoot, _, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		format := validateOutputFormat()
		if format == OutputHuman {
			fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialize a repository\n")
		}
		os.Exit(1)
	}

	// Load existing index
	cache := dcfh.NewDirectoryCache(repoRoot, repoRoot)
	defer cache.Close()
	
	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}

	if _, err := cache.LoadMainIndex(); err != nil {
		outputError(fmt.Sprintf("Failed to load index: %v", err))
		os.Exit(1)
	}

	// Find duplicates
	duplicates, err := cache.FindDuplicates(flags)
	if err != nil {
		outputError(fmt.Sprintf("Failed to find duplicates: %v", err))
		os.Exit(1)
	}

	format := getEffectiveOutputFormat(cache)
	if len(duplicates) == 0 {
		if format == OutputJSON {
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

	switch format {
	case OutputJSON:
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

	case OutputFdupes:
		// fdupes format: absolute paths, one line per file, blank line between groups
		for i, group := range duplicates {
			// Convert to absolute paths and sort them
			var filePaths []string
			for _, relPath := range group.Files {
				absPath := filepath.Join(repoRoot, relPath)
				filePaths = append(filePaths, absPath)
			}

			// Sort the file paths
			for k := 0; k < len(filePaths); k++ {
				for l := k + 1; l < len(filePaths); l++ {
					if filePaths[k] > filePaths[l] {
						filePaths[k], filePaths[l] = filePaths[l], filePaths[k]
					}
				}
			}

			// Print each file in the duplicate group (absolute paths)
			for _, absPath := range filePaths {
				fmt.Println(absPath)
			}

			// Add blank line between groups (except after the last group)
			if i < len(duplicates)-1 {
				fmt.Println()
			}
		}

	default: // OutputHuman
		// Human format: relative paths with context
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