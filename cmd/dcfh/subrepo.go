package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var subrepoCmd = &cobra.Command{
	Use:   "subrepo",
	Short: "Discover and manage nested repositories",
	Long: `Discover and manage nested repositories.

Subcommands:
  find    Discover potential subrepos (directories containing .git)
  add     Register a subrepo for delegated hashing (not yet implemented)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default to "find" when no subcommand given
		return subrepoFindCmd.RunE(cmd, args)
	},
}

// subrepoEntry represents a discovered subrepo for JSON output
type subrepoEntry struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Active bool   `json:"active"`
}

var subrepoFindCmd = &cobra.Command{
	Use:     "find",
	Aliases: []string{"ls"},
	Short:   "Discover potential subrepos",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, _, err := findDcfhRepo()
		if err != nil {
			return err
		}

		format := getOutputFormat()
		var entries []subrepoEntry

		err = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip inaccessible directories
			}

			if !d.IsDir() {
				return nil
			}

			relPath, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return nil
			}

			// Skip the repo's own .dcfh directory and .git internals
			if relPath == ".dcfh" || d.Name() == ".git" {
				return filepath.SkipDir
			}

			// Check if this directory contains a .git/ subdirectory
			gitPath := filepath.Join(path, ".git")
			if info, statErr := os.Stat(gitPath); statErr == nil && info.IsDir() {
				entries = append(entries, subrepoEntry{
					Path:   relPath,
					Type:   "git",
					Active: false,
				})
			}

			return nil
		})

		if err != nil {
			return fmt.Errorf("failed to scan for subrepos: %w", err)
		}

		if format == OutputJSON {
			outputJSON(map[string]any{
				"repository": repoRoot,
				"subrepos":   entries,
				"count":      len(entries),
			})
			return nil
		}

		if len(entries) == 0 {
			fmt.Println("No potential subrepos found.")
			return nil
		}

		fmt.Printf("Potential subrepos in %s:\n\n", repoRoot)
		for _, e := range entries {
			status := "(inactive)"
			fmt.Printf("  %-10s %s %s\n", e.Type, e.Path, status)
		}
		fmt.Printf("\n%d potential subrepo(s) found.\n", len(entries))

		return nil
	},
}

var subrepoAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Register a subrepo for delegated hashing",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("subrepo add is not yet implemented. Use 'dcfh subrepo find' to discover potential subrepos")
	},
}

func init() {
	rootCmd.AddCommand(subrepoCmd)
	subrepoCmd.AddCommand(subrepoFindCmd)
	subrepoCmd.AddCommand(subrepoAddCmd)
}
