package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

var dupesCmd = &cobra.Command{
	Use:   "dupes",
	Short: "Find and display duplicate files",
	Long: `Find and display duplicate files in the repository.

Analyses the index to identify files with identical content (same hash)
but different paths. Groups duplicate files and shows file counts and
total duplicate space that could be reclaimed.`,
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

		duplicates, err := repo.Groups(ctx, dcfh.GroupsRequest{Options: buildOptions()})
		if err != nil {
			return fmt.Errorf("failed to find duplicates: %w", err)
		}

		format := getOutputFormat()
		if len(duplicates) == 0 {
			if format == OutputJSON {
				output := DupesOutput{
					Repository:      repoRoot,
					DuplicateGroups: []dcfh.DuplicateGroup{},
					Summary: DuplicateSummary{
						GroupCount: 0,
						FileCount:  0,
					},
				}
				outputJSON(output)
			}
			return nil // No output if no duplicates found (like fdupes in text mode)
		}

		// duplicates arrive sorted: groups by hash, Files within each group
		// by path. pkg.FindDuplicates is authoritative for both orderings
		// (see pkg/dupes.go). Nothing to sort here.

		switch format {
		case OutputJSON:
			totalFiles := 0
			for _, group := range duplicates {
				totalFiles += len(group.Files)
			}
			outputJSON(DupesOutput{
				Repository:      repoRoot,
				DuplicateGroups: duplicates,
				Summary: DuplicateSummary{
					GroupCount: len(duplicates),
					FileCount:  totalFiles,
				},
			})

		case OutputFdupes:
			// fdupes format: absolute paths, one line per file, blank
			// line between groups. Joining a constant prefix preserves
			// sort order, so paths stay ordered without resorting.
			for i, group := range duplicates {
				for _, relPath := range group.Files {
					fmt.Println(filepath.Join(repoRoot, relPath))
				}
				if i < len(duplicates)-1 {
					fmt.Println()
				}
			}

		default: // OutputHuman
			for i, group := range duplicates {
				for _, relPath := range group.Files {
					fmt.Println(relPath)
				}
				if i < len(duplicates)-1 {
					fmt.Println()
				}
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(dupesCmd)
}
