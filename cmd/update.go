package main

import (
	"fmt"
	"os"
	"time"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// handleUpdate handles the "dcfh update [paths...]" command
func handleUpdate(args []string) {
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

	// Get paths to update (if any)
	var paths []string
	format := validateOutputFormat()
	if len(args) > 0 {
		paths = args
		if format == OutputHuman {
			fmt.Printf("Updating specified paths in %s\n", repoRoot)
			if options.GetInt("verbose") > 0 {
				for _, path := range paths {
					fmt.Printf("  %s\n", path)
				}
			}
		}
	} else {
		if format == OutputHuman {
			fmt.Printf("Updating entire repository in %s\n", repoRoot)
		}
	}

	// Update the index
	cache := dcfh.NewDirectoryCache(repoRoot, repoRoot)
	defer cache.Close()
	
	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}

	if format == OutputHuman && options.GetInt("verbose") > 0 {
		fmt.Println("Scanning directory...")
	}

	start := time.Now()

	if err := cache.Update(flags, paths...); err != nil {
		outputError(fmt.Sprintf("Failed to update index: %v", err))
		os.Exit(1)
	}

	duration := time.Since(start)
	fileCount, totalSize, _ := cache.Stats()

	// Check for duplicates
	var duplicateInfo *DuplicateInfo
	duplicates, err := cache.FindDuplicates(flags)
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

	if format == OutputJSON {
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
		if len(paths) > 0 {
			fmt.Printf("Updated %d specified paths\n", len(paths))
		} else {
			fmt.Printf("Updated index\n")
		}
		if options.GetInt("verbose") > 0 {
			fmt.Printf("✓ Completed in %v\n", duration.Round(time.Millisecond))
			fmt.Printf("✓ Indexed %d files, total size: %d bytes\n", fileCount, totalSize)
			if duplicateInfo != nil {
				fmt.Printf("⚠ Found %d duplicate files in %d sets\n", duplicateInfo.FileCount, duplicateInfo.SetCount)
			}
		}
	}
}