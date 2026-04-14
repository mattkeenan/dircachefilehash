package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// showInitUsage displays usage information for the init command
func showInitUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh init <directory>\n\n")
	fmt.Fprintf(os.Stderr, "Initialise a new dcfh repository in the specified directory.\n\n")
	fmt.Fprintf(os.Stderr, "Description:\n")
	fmt.Fprintf(os.Stderr, "  Creates a new .dcfh directory structure with configuration files\n")
	fmt.Fprintf(os.Stderr, "  and an empty main index. The directory must exist and not already\n")
	fmt.Fprintf(os.Stderr, "  contain a .dcfh repository.\n\n")
	fmt.Fprintf(os.Stderr, "Arguments:\n")
	fmt.Fprintf(os.Stderr, "  <directory>          Path to directory to initialise (required)\n\n")
	fmt.Fprintf(os.Stderr, "Global options:\n")
	fmt.Fprintf(os.Stderr, "  --verbose, -v        Show verbose output during initialization\n")
	fmt.Fprintf(os.Stderr, "  --json               Output result in JSON format\n\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh init .                          # Initialise in current directory\n")
	fmt.Fprintf(os.Stderr, "  dcfh init /home/user/documents       # Initialise in specific directory\n")
	fmt.Fprintf(os.Stderr, "  dcfh --verbose init .                # Initialise with verbose output\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json init /path/to/dir        # Initialise with JSON output\n\n")
	fmt.Fprintf(os.Stderr, "After initialization:\n")
	fmt.Fprintf(os.Stderr, "  Run 'dcfh update' to scan and index files in the repository\n")
}

// handleInit handles the "dcfh init <directory>" command
func handleInit(args []string) {
	// Check for help request
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		showInitUsage()
		return
	}

	if len(args) != 1 {
		outputError("Usage: dcfh init <directory>")
		outputError("Use 'dcfh init --help' for detailed usage information")
		os.Exit(1)
	}

	directory := args[0]
	start := time.Now()

	// Convert to absolute path and resolve symlinks
	absDir, err := filepath.Abs(directory)
	if err != nil {
		outputError(fmt.Sprintf("Failed to get absolute path for %s: %v", directory, err))
		os.Exit(1)
	}

	// Resolve symlinks to get the real path
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		// If symlink resolution fails, fall back to absolute path
		realDir = absDir
	}
	absDir = realDir

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

	// Create cache - this will automatically create .dcfh directory structure
	cache := dcfh.CreateDirectoryCache(absDir, absDir)
	defer func() { _ = cache.Close() }()

	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}

	// Create empty main index - NewDirectoryCache already handles this automatically
	// No need to call any index creation functions explicitly

	format := validateOutputFormat()
	duration := time.Since(start)

	// Always show the initialization message (git-style)
	fmt.Printf("Initialised empty dcfh repository in %s\n", dcfhDir)

	if format == OutputJSON {
		output := InitOutput{
			Success:     true,
			Message:     "Successfully initialised dcfh repository",
			Repository:  absDir,
			FileCount:   0,
			TotalSize:   0,
			TimeElapsed: duration.Round(time.Millisecond).String(),
		}
		outputJSON(output)
	} else if options.GetInt("verbose") > 0 {
		fmt.Printf("✓ Repository structure created\n")
		fmt.Printf("✓ Configuration files initialised\n")
		fmt.Printf("✓ Run 'dcfh update' to scan and index files\n")
		fmt.Printf("✓ Completed in %v\n", duration.Round(time.Millisecond))
	}
}
