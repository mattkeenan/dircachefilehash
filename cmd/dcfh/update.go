package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

var updateCmd = &cobra.Command{
	Use:   "update [paths...]",
	Short: "Update the index with current file states",
	Long: `Update the index with current file states.

Scans the repository (or specified paths) and updates the index
with current file information including hashes, sizes, and timestamps.
This operation synchronises the index with the actual file system state.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Find the dcfh repository root
		repoRoot, _, err := findDcfhRepo()
		if err != nil {
			if getOutputFormat() == OutputHuman {
				fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialise a repository\n")
			}
			return err
		}

		// Get paths to update (if any)
		format := getOutputFormat()
		if len(args) > 0 {
			if format == OutputHuman {
				fmt.Printf("Updating specified paths in %s\n", repoRoot)
				if flagVerbose > 0 {
					for _, path := range args {
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
		cache, err := dcfh.OpenDirectoryCache(repoRoot, repoRoot)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}
		defer func() { _ = cache.Close() }()

		// Apply configuration overrides
		flags := buildFlags()
		if err := cache.ApplyConfigOverrides(flags); err != nil {
			return fmt.Errorf("failed to apply configuration overrides: %w", err)
		}

		if format == OutputHuman && flagVerbose > 0 {
			fmt.Println("Scanning directory...")
		}

		start := time.Now()

		if err := cache.Update(ctx, flags, args...); err != nil {
			return fmt.Errorf("failed to update index: %w", err)
		}

		duration := time.Since(start)
		fileCount, totalSize, _ := cache.Stats()

		if format == OutputJSON {
			output := UpdateOutput{
				Success:      true,
				Message:      "Successfully updated index",
				Repository:   repoRoot,
				PathsUpdated: args,
				FileCount:    fileCount,
				TotalSize:    totalSize,
				TimeElapsed:  duration.Round(time.Millisecond).String(),
			}
			outputJSON(output)
		} else {
			if len(args) > 0 {
				fmt.Printf("Updated %d specified paths\n", len(args))
			} else {
				fmt.Printf("Updated index\n")
			}
			if flagVerbose > 0 {
				fmt.Printf("✓ Completed in %v\n", duration.Round(time.Millisecond))
				fmt.Printf("✓ Indexed %d files, total size: %d bytes\n", fileCount, totalSize)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
