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
		repoRoot, metaDir, err := findDcfhRepo()
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

		// Open existing repository via the Repo abstraction
		repo, err := dcfh.OpenRepo(ctx, metaDir)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}
		defer func() { _ = repo.Close() }()

		if format == OutputHuman && flagVerbose > 0 {
			fmt.Println("Scanning directory...")
		}

		start := time.Now()

		result, err := repo.Apply(ctx, dcfh.ApplyRequest{Options: buildOptions(), Paths: args})
		if err != nil {
			return fmt.Errorf("failed to update index: %w", err)
		}

		duration := time.Since(start)
		fileCount, totalSize := result.FileCount, result.TotalSize

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
