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
since the last update operation.`,
	Args: cobra.NoArgs,
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

		// Open existing repository via the Repo abstraction
		repo, err := dcfh.OpenRepo(ctx, metaDir)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}
		defer func() { _ = repo.Close() }()

		status, err := repo.Diff(ctx, dcfh.DiffRequest{Options: buildOptions()})
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
	rootCmd.AddCommand(statusCmd)
}
