package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// handleInit handles the "dcfh init <directory>" command
func handleInit(args []string) {
	if len(args) != 1 {
		outputError("Usage: dcfh init <directory>")
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
	cache := dcfh.NewDirectoryCache(absDir, absDir)
	defer cache.Close()
	
	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}
	
	// Create empty main index
	if err := cache.CreateEmptyMainIndex(); err != nil {
		outputError(fmt.Sprintf("Failed to create empty index: %v", err))
		os.Exit(1)
	}

	format := validateOutputFormat()
	duration := time.Since(start)
	
	// Always show the initialization message (git-style)
	fmt.Printf("Initialized empty dcfh repository in %s\n", dcfhDir)

	if format == OutputJSON {
		output := InitOutput{
			Success:     true,
			Message:     "Successfully initialized dcfh repository",
			Repository:  absDir,
			FileCount:   0,
			TotalSize:   0,
			TimeElapsed: duration.Round(time.Millisecond).String(),
		}
		outputJSON(output)
	} else if options.GetInt("verbose") > 0 {
		fmt.Printf("✓ Repository structure created\n")
		fmt.Printf("✓ Configuration files initialized\n")
		fmt.Printf("✓ Run 'dcfh update' to scan and index files\n")
		fmt.Printf("✓ Completed in %v\n", duration.Round(time.Millisecond))
	}
}