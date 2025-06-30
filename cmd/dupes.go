package main

import (
	"fmt"
	"os"
	"path/filepath"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// handleDupes handles the "dcfh dupes" command
func handleDupes(args []string) {
	if len(args) != 0 {
		outputError("Usage: dcfh dupes")
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