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
This operation synchronises the index with the actual file system state.

Filter flags compose via the scope-marker syntax (see ` + "`dcfh status --help`" + `).
Only --ignore is honoured at scan-time — it short-circuits the walker
so subtracted entries are never re-stat'd or re-hashed:

  dcfh update --ignore --name '*.tmp'              — skip *.tmp during scan
  dcfh update --no-ignore-file                     — bypass .dcfh/ignore

--print segments are accepted for symmetry with status/dupes but have
no effect on update (the cache is always refreshed against on-disk
truth).`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		repoRoot, metaDir, err := findDcfhRepo()
		if err != nil {
			if getOutputFormat() == OutputHuman {
				fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialise a repository\n")
			}
			return err
		}

		_, prints, ignores, paths, noIgnoreFile, err := resolveScopes(args, cmdUpdate)
		if err != nil {
			return err
		}
		if err := finaliseRootFlags(cmd); err != nil {
			return err
		}

		format := getOutputFormat()
		if len(paths) > 0 {
			if format == OutputHuman {
				fmt.Printf("Updating specified paths in %s\n", repoRoot)
				if flagVerbose > 0 {
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

		repo, err := dcfh.OpenRepo(ctx, metaDir)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}
		defer func() { _ = repo.Close() }()

		if format == OutputHuman && flagVerbose > 0 {
			fmt.Println("Scanning directory...")
		}

		start := time.Now()

		result, err := repo.Apply(ctx, dcfh.ApplyRequest{
			Options:      buildOptions(),
			Paths:        paths,
			Prints:       prints,
			Ignores:      ignores,
			NoIgnoreFile: noIgnoreFile,
		})
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
