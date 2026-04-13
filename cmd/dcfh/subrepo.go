package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// handleSubrepo handles the subrepo command with various subcommands
func handleSubrepo(args []string) {
	// Default to "find" when no subcommand given
	if len(args) < 1 {
		handleSubrepoFind()
		return
	}

	subcommand := args[0]

	switch subcommand {
	case "find", "ls":
		handleSubrepoFind()
	case "add":
		handleSubrepoAdd(args[1:])
	case "help", "-h", "--help":
		showSubrepoUsage()
	default:
		outputError(fmt.Sprintf("Unknown subrepo subcommand: %s", subcommand))
		showSubrepoUsage()
		os.Exit(1)
	}
}

// showSubrepoUsage displays usage information for the subrepo command
func showSubrepoUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh subrepo <subcommand> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Subrepo management subcommands:\n")
	fmt.Fprintf(os.Stderr, "  find, ls         Discover potential subrepos (directories containing .git)\n")
	fmt.Fprintf(os.Stderr, "  add <path>       Register a subrepo for delegated hashing (not yet implemented)\n")
	fmt.Fprintf(os.Stderr, "  help             Show this help message\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh subrepo find\n")
	fmt.Fprintf(os.Stderr, "  dcfh subrepo add repo/myproject\n")
}

// subrepoEntry represents a discovered subrepo for JSON output
type subrepoEntry struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Active bool   `json:"active"`
}

// handleSubrepoFind walks the repo tree and lists directories containing .git/
func handleSubrepoFind() {
	repoRoot, _, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}

	format := validateOutputFormat()
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
		outputError(fmt.Sprintf("failed to scan for subrepos: %v", err))
		os.Exit(1)
	}

	if format == OutputJSON {
		outputJSON(map[string]any{
			"repository": repoRoot,
			"subrepos":   entries,
			"count":      len(entries),
		})
		return
	}

	if len(entries) == 0 {
		fmt.Println("No potential subrepos found.")
		return
	}

	fmt.Printf("Potential subrepos in %s:\n\n", repoRoot)
	for _, e := range entries {
		status := "(inactive)"
		fmt.Printf("  %-10s %s %s\n", e.Type, e.Path, status)
	}
	fmt.Printf("\n%d potential subrepo(s) found.\n", len(entries))
}

// handleSubrepoAdd is a stub for the future subrepo add command
func handleSubrepoAdd(args []string) {
	if len(args) < 1 {
		outputError("subrepo add requires a path argument")
		fmt.Fprintf(os.Stderr, "\nUsage: dcfh subrepo add <path>\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Error: subrepo add is not yet implemented.\n")
	fmt.Fprintf(os.Stderr, "Use 'dcfh subrepo find' to discover potential subrepos.\n")
	os.Exit(1)
}
