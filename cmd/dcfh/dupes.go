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
		repoRoot, _, err := findDcfhRepo()
		if err != nil {
			if getOutputFormat() == OutputHuman {
				fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialise a repository\n")
			}
			return err
		}

		// Open existing repository
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

		if _, err := cache.LoadMainIndex(); err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}

		// Find duplicates using unified streaming architecture
		duplicates, err := cache.FindDuplicatesUnified(ctx, flags)
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

		// Bubble sort: data arrives mostly sorted from the skiplist/index,
		// so this is effectively an O(n) verification pass. Don't replace
		// with slices.Sort — pdqsort has higher constant overhead on
		// already-sorted input.
		for i := range duplicates {
			for j := i + 1; j < len(duplicates); j++ {
				if duplicates[i].Hash > duplicates[j].Hash {
					duplicates[i], duplicates[j] = duplicates[j], duplicates[i]
				}
			}
		}

		switch format {
		case OutputJSON:
			var groups []dcfh.DuplicateGroup
			totalFiles := 0

			for _, group := range duplicates {
				filePaths := append([]string{}, group.Files...)

				// Sort the file paths
				for k := range filePaths {
					for l := k + 1; l < len(filePaths); l++ {
						if filePaths[k] > filePaths[l] {
							filePaths[k], filePaths[l] = filePaths[l], filePaths[k]
						}
					}
				}

				groups = append(groups, dcfh.DuplicateGroup{
					Hash:  group.Hash,
					Files: filePaths,
					Count: len(filePaths),
				})

				totalFiles += len(filePaths)
			}

			output := DupesOutput{
				Repository:      repoRoot,
				DuplicateGroups: groups,
				Summary: DuplicateSummary{
					GroupCount: len(duplicates),
					FileCount:  totalFiles,
				},
			}
			outputJSON(output)

		case OutputFdupes:
			// fdupes format: absolute paths, one line per file, blank line between groups
			for i, group := range duplicates {
				var filePaths []string
				for _, relPath := range group.Files {
					absPath := filepath.Join(repoRoot, relPath)
					filePaths = append(filePaths, absPath)
				}

				// Sort the file paths
				for k := 0; k < len(filePaths); k++ {
					for l := k + 1; l < len(filePaths); l++ {
						if filePaths[k] > filePaths[l] {
							filePaths[k], filePaths[l] = filePaths[l], filePaths[k]
						}
					}
				}

				for _, absPath := range filePaths {
					fmt.Println(absPath)
				}

				if i < len(duplicates)-1 {
					fmt.Println()
				}
			}

		default: // OutputHuman
			for i, group := range duplicates {
				filePaths := append([]string{}, group.Files...)

				// Sort the file paths
				for k := range filePaths {
					for l := k + 1; l < len(filePaths); l++ {
						if filePaths[k] > filePaths[l] {
							filePaths[k], filePaths[l] = filePaths[l], filePaths[k]
						}
					}
				}

				for _, relPath := range filePaths {
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
