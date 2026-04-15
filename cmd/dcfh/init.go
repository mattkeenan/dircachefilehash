package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

var initCmd = &cobra.Command{
	Use:   "init <directory>",
	Short: "Initialise a new dcfh repository",
	Long: `Initialise a new dcfh repository in the specified directory.

Creates a new .dcfh directory structure with configuration files
and an empty main index. The directory must exist and not already
contain a .dcfh repository.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		directory := args[0]
		start := time.Now()

		// Convert to absolute path and resolve symlinks
		absDir, err := filepath.Abs(directory)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for %s: %w", directory, err)
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
			return fmt.Errorf("failed to access directory %s: %w", absDir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", absDir)
		}

		// Check if .dcfh already exists
		dcfhDir := filepath.Join(absDir, ".dcfh")
		if _, err := os.Stat(dcfhDir); err == nil {
			return fmt.Errorf(".dcfh directory already exists in %s", absDir)
		}

		// Create cache - this will automatically create .dcfh directory structure
		cache := dcfh.CreateDirectoryCache(absDir, absDir)
		defer func() { _ = cache.Close() }()

		// Apply configuration overrides
		flags := buildFlags()
		if err := cache.ApplyConfigOverrides(flags); err != nil {
			return fmt.Errorf("failed to apply configuration overrides: %w", err)
		}

		format := getOutputFormat()
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
		} else if flagVerbose > 0 {
			fmt.Printf("✓ Repository structure created\n")
			fmt.Printf("✓ Configuration files initialised\n")
			fmt.Printf("✓ Run 'dcfh update' to scan and index files\n")
			fmt.Printf("✓ Completed in %v\n", duration.Round(time.Millisecond))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
