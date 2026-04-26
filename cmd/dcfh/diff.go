package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

var diffCmd = &cobra.Command{
	Use:   "diff <left-ref> <right-ref>",
	Short: "Compare any two index references",
	Long: `Compare two index references and report the differences.

References use the same selector vocabulary as dcfhfind:
  main           the canonical main.idx
  cache          the cache.idx
  cache+main     cache deltas applied over main (cache wins)
  fs-scan        live filesystem scan; refreshes cache.idx as a side-effect
  snapshot:<x>   a snapshot, identified by exact id or by tag (latest match)
  <path>.idx     an arbitrary index file

Examples:
  dcfh diff main fs-scan                       # equivalent to dcfh status
  dcfh diff snapshot:monthly main              # what's changed since the monthly snapshot
  dcfh diff snapshot:monthly fs-scan           # changes vs current filesystem
  dcfh diff snapshot:<id-a> snapshot:<id-b>    # difference between two snapshots`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		left, right := args[0], args[1]

		repoRoot, metaDir, err := findDcfhRepo()
		if err != nil {
			if getOutputFormat() == OutputHuman {
				fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialise a repository\n")
			}
			return err
		}

		repo, err := dcfh.OpenRepo(ctx, metaDir)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}
		defer func() { _ = repo.Close() }()

		result, err := repo.DiffRefs(ctx, dcfh.DiffRefsRequest{
			Options: buildOptions(),
			Left:    left,
			Right:   right,
		})
		if err != nil {
			return err
		}

		if getOutputFormat() == OutputJSON {
			outputDiffJSON(left, right, result)
			return nil
		}

		// Byte-equivalence with `dcfh status` on the canonical case.
		if left == "main" && right == "fs-scan" {
			relCwd, sinceStr, info, err := statusFraming(ctx, repo, repoRoot)
			if err != nil {
				return fmt.Errorf("failed to read repository info: %w", err)
			}
			renderStatusHuman(os.Stdout, repoRoot, relCwd, sinceStr, result, info.EntryCount)
			return nil
		}

		renderDiffHuman(os.Stdout, left, right, result)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(diffCmd)
}
