package main

import (
	"fmt"
	"os"
	"time"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// showUpdateUsage displays usage information for the update command
func showUpdateUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh update [paths...]\n\n")
	fmt.Fprintf(os.Stderr, "Update the index with current file states.\n\n")
	fmt.Fprintf(os.Stderr, "Description:\n")
	fmt.Fprintf(os.Stderr, "  Scans the repository (or specified paths) and updates the index\n")
	fmt.Fprintf(os.Stderr, "  with current file information including hashes, sizes, and timestamps.\n")
	fmt.Fprintf(os.Stderr, "  This operation synchronises the index with the actual file system state.\n\n")
	fmt.Fprintf(os.Stderr, "Arguments:\n")
	fmt.Fprintf(os.Stderr, "  [paths...]           Specific paths to update (optional)\n")
	fmt.Fprintf(os.Stderr, "                      If no paths specified, updates entire repository\n\n")
	fmt.Fprintf(os.Stderr, "Global options:\n")
	fmt.Fprintf(os.Stderr, "  --verbose, -v        Show verbose progress information\n")
	fmt.Fprintf(os.Stderr, "  --json               Output result in JSON format\n")
	fmt.Fprintf(os.Stderr, "  --symlinks=MODE      Handle symlinks: all, contained, none\n")
	fmt.Fprintf(os.Stderr, "  --hash-workers=N     Number of concurrent hash workers\n")
	fmt.Fprintf(os.Stderr, "  --filehash=ALG       Override hash algorithm (format: default:sha256)\n")
	fmt.Fprintf(os.Stderr, "  --dry-run            Show what would be done without updating\n\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh update                          # Update entire repository\n")
	fmt.Fprintf(os.Stderr, "  dcfh update file.txt dir/            # Update specific files/directories\n")
	fmt.Fprintf(os.Stderr, "  dcfh --verbose update                # Update with progress information\n")
	fmt.Fprintf(os.Stderr, "  dcfh --hash-workers=8 update         # Use 8 parallel hash workers\n")
	fmt.Fprintf(os.Stderr, "  dcfh --filehash=default:sha1 update  # Use SHA-1 instead of default hash\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json update                   # Output results in JSON format\n")
	fmt.Fprintf(os.Stderr, "  dcfh --dry-run update               # Preview changes without updating\n\n")
	fmt.Fprintf(os.Stderr, "Performance:\n")
	fmt.Fprintf(os.Stderr, "  Use --hash-workers to control parallelism for large repositories\n")
	fmt.Fprintf(os.Stderr, "  Partial updates (specific paths) are faster than full repository scans\n")
}

// handleUpdate handles the "dcfh update [paths...]" command
func handleUpdate(args []string, shutdownChan <-chan struct{}) {
	// Check for help request
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		showUpdateUsage()
		return
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
	defer func() { _ = cache.Close() }()

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

	if err := cache.Update(shutdownChan, flags, paths...); err != nil {
		outputError(fmt.Sprintf("Failed to update index: %v", err))
		os.Exit(1)
	}

	duration := time.Since(start)
	fileCount, totalSize, _ := cache.Stats()

	if format == OutputJSON {
		output := UpdateOutput{
			Success:      true,
			Message:      "Successfully updated index",
			Repository:   repoRoot,
			PathsUpdated: paths,
			FileCount:    fileCount,
			TotalSize:    totalSize,
			TimeElapsed:  duration.Round(time.Millisecond).String(),
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
		}
	}
}
