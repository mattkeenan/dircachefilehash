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

Filter flags compose via the scope-marker syntax (see ` + "`dcfh status --help`" + `
for the full grammar and gitignore-pattern note). Only --ignore is
honoured at scan-time — it short-circuits the walker so subtracted
entries are never re-stat'd or re-hashed:

  dcfh update --ignore --name '*.tmp'              — skip *.tmp during scan
  dcfh update --no-ignore-file                     — bypass .dcfh/ignore

--print segments are accepted for symmetry with status/dupes but have
no effect on update (the cache is always refreshed against on-disk
truth).

Push-down side effect: an entry skipped at scan-time is dropped from
the rewritten main index (the merge never sees it). The cache index
keeps its prior entry, so no data is lost — but a subsequent
` + "`dcfh status`" + ` will report the file as added rather than
modified. Use --no-ignore-file or omit --ignore to round-trip
ignored entries through the index.

--interactive-tree opens a gdu-style full-screen tree of the change
after the run, for ad-hoc browsing. It is TTY-only: ignored with
--json or when stdout is piped. The viewer is read-only.`,
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

		state, prints, ignores, paths, noIgnoreFile, err := resolveScopes(args, cmdUpdate)
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

		// Only collect the op-classified change-set when the viewer will
		// actually run — avoids the extra bookkeeping on the piped/JSON
		// path and keeps it byte-for-byte unchanged.
		showTree := interactiveTreeWanted(state.interactiveTree)

		result, err := repo.Apply(ctx, dcfh.ApplyRequest{
			Options:        buildOptions(),
			Paths:          paths,
			Prints:         prints,
			Ignores:        ignores,
			NoIgnoreFile:   noIgnoreFile,
			CollectChanges: showTree,
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

		// Post-run interactive tree (TTY-only). The change labels come
		// from the enriched UpdateResult (design KD3) — the pre-update
		// state is gone after the atomic rename, so this is the only
		// no-extra-walk source of update's change-set.
		launchInteractiveTree(ctx, repo, "update", dcfh.ChangeSet{
			Added:        result.Added,
			Modified:     result.Modified,
			Deleted:      result.Deleted,
			DeletedSizes: result.DeletedSizes,
		}, showTree)
		return nil
	},
}

func init() {
	registerHelpFlags(updateCmd.Flags(), cmdUpdate)
	rootCmd.AddCommand(updateCmd)
}
