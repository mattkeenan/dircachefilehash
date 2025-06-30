package main

import (
	"fmt"
	"os"
	"path/filepath"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// handleStatus handles the "dcfh status" command
func handleStatus(args []string) {
	if len(args) != 0 {
		outputError("Usage: dcfh status")
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

	// Get current working directory relative to repo root
	cwd, _ := os.Getwd()
	relCwd, _ := filepath.Rel(repoRoot, cwd)
	if relCwd == "." {
		relCwd = ""
	}

	// Create cache and get status
	cache := dcfh.NewDirectoryCache(repoRoot, repoRoot)
	defer cache.Close()
	
	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}

	status, err := cache.Status(flags)
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}

	format := validateOutputFormat()
	if format == OutputJSON {
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