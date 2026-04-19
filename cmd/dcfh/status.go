package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

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

		// Get current working directory relative to repo root
		cwd, _ := os.Getwd()
		relCwd, _ := filepath.Rel(repoRoot, cwd)
		if relCwd == "." {
			relCwd = ""
		}

		// Open existing repository via the Repo abstraction
		repo, err := dcfh.OpenRepo(ctx, metaDir)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}
		defer func() { _ = repo.Close() }()

		status, err := repo.Survey(ctx, dcfh.SurveyRequest{Options: buildOptions()})
		if err != nil {
			return err
		}

		info, err := repo.Info(ctx)
		if err != nil {
			return fmt.Errorf("failed to read repository info: %w", err)
		}

		// Get "since" timestamp from index header (v3+) or file mtime (v2 fallback)
		sinceStr := ""
		if !info.IndexTimestamp.IsZero() {
			sinceStr = info.IndexTimestamp.Format(time.RFC3339)
		} else if fi, err := os.Stat(info.IndexFile); err == nil {
			sinceStr = fi.ModTime().UTC().Format(time.RFC3339)
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

		// Text output
		fmt.Printf("On branch main\n")
		if relCwd != "" {
			fmt.Printf("Working directory: %s\n", relCwd)
		}
		fmt.Printf("Repository root: %s\n", repoRoot)
		fmt.Println()

		// Show status
		if !status.HasChanges() {
			fmt.Println("Nothing to commit, working tree clean")
			fileCount := info.EntryCount
			if sinceStr != "" {
				fmt.Printf("Index contains %d files since %s\n", fileCount, sinceStr)
			} else {
				fmt.Printf("Index contains %d files\n", fileCount)
			}
			return nil
		}

		if len(status.Modified) > 0 {
			fmt.Println("Changes not staged for commit:")
			fmt.Println("  (use \"dcfh update\" to update the index)")
			fmt.Println()
			for _, path := range status.Modified {
				fmt.Printf("\tmodified:   %s\n", path)
			}
			fmt.Println()
		}

		if len(status.Added) > 0 {
			fmt.Println("Untracked files:")
			fmt.Println("  (use \"dcfh update\" to include in what will be committed)")
			fmt.Println()
			for _, path := range status.Added {
				fmt.Printf("\t%s\n", path)
			}
			fmt.Println()
		}

		if len(status.Deleted) > 0 {
			fmt.Println("Changes not staged for commit:")
			fmt.Println("  (use \"dcfh update\" to update the index)")
			fmt.Println()
			for _, path := range status.Deleted {
				fmt.Printf("\tdeleted:    %s\n", path)
			}
			fmt.Println()
		}

		sinceSuffix := ""
		if sinceStr != "" {
			sinceSuffix = " since " + sinceStr
		}
		fmt.Printf("Summary: %d modified (%s), %d added (%s), %d deleted (%s)%s\n",
			len(status.Modified), formatFileSize(status.ModifiedBytes),
			len(status.Added), formatFileSize(status.AddedBytes),
			len(status.Deleted), formatFileSize(status.DeletedBytes),
			sinceSuffix)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
