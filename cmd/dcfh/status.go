package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the status of files in the repository",
	Long: `Show the status of files in the current dcfh repository.

Compares the current state of files with the last recorded state
in the index. Shows files that have been modified, added, or deleted
since the last update operation.

Filter flags (--name, --min-size, --start-date, --end-date, --mtime,
--hash, …) narrow which changes are reported. Filters compose with
the scope-marker syntax: every filter flag belongs to a --print or
--ignore segment. Tokens before the first marker form an implicit
--print segment, so a bare flag list works like find(1):

  dcfh status --name '*.go'                        — print *.go changes
  dcfh status --print --name '*.go' --ignore --name '*_test.go'
                                                   — print .go but not test
  dcfh status --ignore --name '*.tmp'              — print everything except *.tmp
  dcfh status --no-ignore-file                     — bypass .dcfh/ignore

Across segments: --print groups AND together, --ignore groups OR
together (any matching ignore subtracts). An empty --print segment
(no filter flags between it and the next marker, or end of argv) is
the identity — it matches everything and constrains nothing.

--name / --iname / --path / --ipath patterns use gitignore syntax
(*, ?, [abc], **, leading '/' anchors to the repo root, trailing '/'
matches directories only, '!' negates) — the same dialect as a line
in .dcfh/ignore.

The scan refreshes the cache against on-disk truth regardless of
filters; --ignore is the one exception, since it can short-circuit
the scan walker.`,
	// With DisableFlagParsing the cobra-level arg validator sees raw
	// flag tokens (--json, --print, …) as positionals and would reject
	// them. The RunE preamble enforces "no positional args" after
	// scope-marker parsing instead.
	Args:               cobra.ArbitraryArgs,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		_, prints, ignores, positionals, noIgnoreFile, err := resolveScopes(args, cmdStatus)
		if err != nil {
			return err
		}
		if len(positionals) > 0 {
			return fmt.Errorf("status accepts no positional arguments, got: %v", positionals)
		}
		if err := finaliseRootFlags(cmd); err != nil {
			return err
		}

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

		status, err := repo.Diff(ctx, dcfh.DiffRequest{
			Options:      buildOptions(),
			Prints:       prints,
			Ignores:      ignores,
			NoIgnoreFile: noIgnoreFile,
		})
		if err != nil {
			return err
		}

		relCwd, sinceStr, info, err := statusFraming(ctx, repo, repoRoot)
		if err != nil {
			return fmt.Errorf("failed to read repository info: %w", err)
		}

		format := getOutputFormat()
		if format == OutputJSON {
			fileCount := info.EntryCount
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
					ModifiedBytes: status.ModifiedBytes,
					AddedBytes:    status.AddedBytes,
					DeletedBytes:  status.DeletedBytes,
					HasChanges:    status.HasChanges(),
					Since:         sinceStr,
				},
				IndexInfo: IndexInfo{
					FileCount: fileCount,
				},
			}
			outputJSON(output)
			return nil
		}

		renderStatusHuman(os.Stdout, repoRoot, relCwd, sinceStr, status, info.EntryCount)
		return nil
	},
}

func init() {
	registerHelpFlags(statusCmd.Flags(), cmdStatus)
	rootCmd.AddCommand(statusCmd)
}
