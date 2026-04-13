package main

import (
	"fmt"
	"os"
	"path/filepath"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// showStatusUsage displays usage information for the status command
func showStatusUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh status\n\n")
	fmt.Fprintf(os.Stderr, "Show the status of files in the current dcfh repository.\n\n")
	fmt.Fprintf(os.Stderr, "Description:\n")
	fmt.Fprintf(os.Stderr, "  Compares the current state of files with the last recorded state\n")
	fmt.Fprintf(os.Stderr, "  in the index. Shows files that have been modified, added, or deleted\n")
	fmt.Fprintf(os.Stderr, "  since the last update operation.\n\n")
	fmt.Fprintf(os.Stderr, "Output categories:\n")
	fmt.Fprintf(os.Stderr, "  Modified             Files that exist in index but have changed\n")
	fmt.Fprintf(os.Stderr, "  Added                New files not present in index\n")
	fmt.Fprintf(os.Stderr, "  Deleted              Files in index but no longer exist on disk\n\n")
	fmt.Fprintf(os.Stderr, "Global options:\n")
	fmt.Fprintf(os.Stderr, "  --verbose, -v        Show additional information and progress\n")
	fmt.Fprintf(os.Stderr, "  --json               Output result in JSON format\n")
	fmt.Fprintf(os.Stderr, "  --symlinks=MODE      Handle symlinks: all, contained, none\n")
	fmt.Fprintf(os.Stderr, "  --hash-workers=N     Number of concurrent hash workers\n\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh status                          # Show status in current repository\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json status                   # Show status in JSON format\n")
	fmt.Fprintf(os.Stderr, "  dcfh --verbose status                # Show verbose status information\n")
	fmt.Fprintf(os.Stderr, "  dcfh --symlinks=none status          # Ignore symlinks during status check\n\n")
	fmt.Fprintf(os.Stderr, "Tips:\n")
	fmt.Fprintf(os.Stderr, "  Use 'dcfh update' to synchronise the index with current file state\n")
	fmt.Fprintf(os.Stderr, "  Status check is read-only and does not modify the index\n")
}

// handleStatus handles the "dcfh status" command
func handleStatus(args []string, shutdownChan <-chan struct{}) {
	// Check for help request
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		showStatusUsage()
		return
	}

	if len(args) != 0 {
		outputError("Usage: dcfh status")
		outputError("Use 'dcfh status --help' for detailed usage information")
		os.Exit(1)
	}

	// Find the dcfh repository root
	repoRoot, _, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		format := validateOutputFormat()
		if format == OutputHuman {
			fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialise a repository\n")
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
	defer func() { _ = cache.Close() }()

	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}

	status, err := cache.Status(shutdownChan, flags)
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
