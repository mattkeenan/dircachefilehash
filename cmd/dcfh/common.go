package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
	"github.com/mattkeenan/dircachefilehash/pkg/fsdedupe"
)

// Output structures for JSON
type InitOutput struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	Repository  string `json:"repository"`
	FileCount   int    `json:"file_count"`
	TotalSize   int64  `json:"total_size"`
	TimeElapsed string `json:"time_elapsed,omitempty"`
}

type StatusOutput struct {
	Repository string        `json:"repository"`
	WorkingDir string        `json:"working_dir,omitempty"`
	Modified   []string      `json:"modified"`
	Added      []string      `json:"added"`
	Deleted    []string      `json:"deleted"`
	Summary    StatusSummary `json:"summary"`
	IndexInfo  IndexInfo     `json:"index_info"`
}

type StatusSummary struct {
	ModifiedCount int    `json:"modified_count"`
	AddedCount    int    `json:"added_count"`
	DeletedCount  int    `json:"deleted_count"`
	ModifiedBytes int64  `json:"modified_bytes"`
	AddedBytes    int64  `json:"added_bytes"`
	DeletedBytes  int64  `json:"deleted_bytes"`
	HasChanges    bool   `json:"has_changes"`
	Since         string `json:"since,omitempty"`
}

type IndexInfo struct {
	FileCount int `json:"file_count"`
}

type UpdateOutput struct {
	Success      bool     `json:"success"`
	Message      string   `json:"message"`
	Repository   string   `json:"repository"`
	PathsUpdated []string `json:"paths_updated,omitempty"`
	FileCount    int      `json:"file_count"`
	TotalSize    int64    `json:"total_size"`
	TimeElapsed  string   `json:"time_elapsed"`
}

type DuplicateInfo struct {
	SetCount  int `json:"set_count"`
	FileCount int `json:"file_count"`
}

type DupesOutput struct {
	Repository      string                `json:"repository"`
	DuplicateGroups []dcfh.DuplicateGroup `json:"duplicate_groups"`
	Summary         DuplicateSummary      `json:"summary"`
	DedupeResult    *fsdedupe.Result      `json:"dedupe_result,omitempty"`
}

type DuplicateSummary struct {
	GroupCount int `json:"group_count"`
	FileCount  int `json:"file_count"`
}

type ErrorOutput struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

// OutputFormat represents the supported output formats
type OutputFormat string

const (
	OutputHuman  OutputFormat = "human"
	OutputJSON   OutputFormat = "json"
	OutputFdupes OutputFormat = "fdupes"
)

// getOutputFormat returns the current output format from cobra flags.
// The PersistentPreRunE on rootCmd has already validated and resolved
// --json to --output=json, so we can just read flagOutput directly.
func getOutputFormat() OutputFormat {
	return OutputFormat(flagOutput)
}

// buildOptions builds a typed Options struct for Repo calls from cobra flag values.
func buildOptions() dcfh.Options {
	return dcfh.Options{
		Verbose:          flagVerbose,
		Filehash:         flagFilehash,
		Symlinks:         flagSymlinks,
		HashWorkers:      flagHashWorkers,
		IndexLockTimeout: flagIndexLockTimeout,
	}
}

// outputError outputs an error message in the appropriate format
func outputError(message string) {
	format := getOutputFormat()
	if format == OutputJSON {
		errorOut := ErrorOutput{
			Success: false,
			Error:   message,
		}
		if err := json.NewEncoder(os.Stderr).Encode(errorOut); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	}
}

// outputMessage outputs a message in the appropriate format
func outputMessage(message string) {
	format := getOutputFormat()
	if format == OutputJSON {
		successOut := struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}{
			Success: true,
			Message: message,
		}
		if err := json.NewEncoder(os.Stdout).Encode(successOut); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		}
	} else {
		fmt.Println(message)
	}
}

// outputJSON outputs data as formatted JSON
func outputJSON(data any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
	}
}

// findDcfhRepo returns the (rootDir, metaDir) for the current repository.
// Results are cached after the first successful call.
func findDcfhRepo() (string, string, error) {
	if cachedRepoRoot != "" {
		return cachedRepoRoot, cachedMetaDir, nil
	}

	rootDir, metaDir, err := dcfh.DiscoverRepository("")
	if err != nil {
		return "", "", err
	}

	cachedRepoRoot = rootDir
	cachedMetaDir = metaDir
	return rootDir, metaDir, nil
}

// renderStatusHuman renders a StatusResult in the long-standing dcfh-status
// format. Shared between `dcfh status` and `dcfh diff main fs-scan` so the
// two paths are byte-identical on the canonical case.
//
// repoRoot/relCwd/sinceStr/fileCount are status-specific framing values;
// callers driving non-status diffs should use renderDiffHuman instead.
func renderStatusHuman(w io.Writer, repoRoot, relCwd, sinceStr string, sr *dcfh.StatusResult, fileCount int) {
	fmt.Fprintf(w, "On branch main\n")
	if relCwd != "" {
		fmt.Fprintf(w, "Working directory: %s\n", relCwd)
	}
	fmt.Fprintf(w, "Repository root: %s\n", repoRoot)
	fmt.Fprintln(w)

	if !sr.HasChanges() {
		fmt.Fprintln(w, "Nothing to commit, working tree clean")
		if sinceStr != "" {
			fmt.Fprintf(w, "Index contains %d files since %s\n", fileCount, sinceStr)
		} else {
			fmt.Fprintf(w, "Index contains %d files\n", fileCount)
		}
		return
	}

	if len(sr.Modified) > 0 {
		fmt.Fprintln(w, "Changes not staged for commit:")
		fmt.Fprintln(w, "  (use \"dcfh update\" to update the index)")
		fmt.Fprintln(w)
		for _, path := range sr.Modified {
			fmt.Fprintf(w, "\tmodified:   %s\n", path)
		}
		fmt.Fprintln(w)
	}

	if len(sr.Added) > 0 {
		fmt.Fprintln(w, "Untracked files:")
		fmt.Fprintln(w, "  (use \"dcfh update\" to include in what will be committed)")
		fmt.Fprintln(w)
		for _, path := range sr.Added {
			fmt.Fprintf(w, "\t%s\n", path)
		}
		fmt.Fprintln(w)
	}

	if len(sr.Deleted) > 0 {
		fmt.Fprintln(w, "Changes not staged for commit:")
		fmt.Fprintln(w, "  (use \"dcfh update\" to update the index)")
		fmt.Fprintln(w)
		for _, path := range sr.Deleted {
			fmt.Fprintf(w, "\tdeleted:    %s\n", path)
		}
		fmt.Fprintln(w)
	}

	sinceSuffix := ""
	if sinceStr != "" {
		sinceSuffix = " since " + sinceStr
	}
	fmt.Fprintf(w, "Summary: %d modified (%s), %d added (%s), %d deleted (%s)%s\n",
		len(sr.Modified), formatFileSize(sr.ModifiedBytes),
		len(sr.Added), formatFileSize(sr.AddedBytes),
		len(sr.Deleted), formatFileSize(sr.DeletedBytes),
		sinceSuffix)
}

// renderDiffHuman renders a StatusResult as a generic two-sided diff.
// Used by `dcfh diff` for any combination other than (main, fs-scan), which
// routes through renderStatusHuman to preserve dcfh-status output.
func renderDiffHuman(w io.Writer, leftLabel, rightLabel string, sr *dcfh.StatusResult) {
	fmt.Fprintf(w, "Diff: %s -> %s\n", leftLabel, rightLabel)
	fmt.Fprintln(w)

	if !sr.HasChanges() {
		fmt.Fprintln(w, "No differences.")
		return
	}

	if len(sr.Modified) > 0 {
		fmt.Fprintf(w, "Modified (%d, %s):\n", len(sr.Modified), formatFileSize(sr.ModifiedBytes))
		for _, path := range sr.Modified {
			fmt.Fprintf(w, "\tmodified:   %s\n", path)
		}
		fmt.Fprintln(w)
	}
	if len(sr.Added) > 0 {
		fmt.Fprintf(w, "Added on %s (%d, %s):\n", rightLabel, len(sr.Added), formatFileSize(sr.AddedBytes))
		for _, path := range sr.Added {
			fmt.Fprintf(w, "\tadded:      %s\n", path)
		}
		fmt.Fprintln(w)
	}
	if len(sr.Deleted) > 0 {
		fmt.Fprintf(w, "Missing on %s (%d, %s):\n", rightLabel, len(sr.Deleted), formatFileSize(sr.DeletedBytes))
		for _, path := range sr.Deleted {
			fmt.Fprintf(w, "\tdeleted:    %s\n", path)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Summary: %d modified (%s), %d added (%s), %d deleted (%s)\n",
		len(sr.Modified), formatFileSize(sr.ModifiedBytes),
		len(sr.Added), formatFileSize(sr.AddedBytes),
		len(sr.Deleted), formatFileSize(sr.DeletedBytes))
}

// outputDiffJSON emits the standard diff JSON envelope used by `dcfh diff`
// and `dcfh snapshot status`. Keeps the two paths from drifting on field
// names, ordering, or summary shape.
func outputDiffJSON(left, right string, sr *dcfh.StatusResult) {
	outputJSON(map[string]any{
		"left":     left,
		"right":    right,
		"modified": sr.Modified,
		"added":    sr.Added,
		"deleted":  sr.Deleted,
		"summary": StatusSummary{
			ModifiedCount: len(sr.Modified),
			AddedCount:    len(sr.Added),
			DeletedCount:  len(sr.Deleted),
			ModifiedBytes: sr.ModifiedBytes,
			AddedBytes:    sr.AddedBytes,
			DeletedBytes:  sr.DeletedBytes,
			HasChanges:    sr.HasChanges(),
		},
	})
}

// statusFraming derives the status-style framing values (working-dir relative
// to repo root, "since" timestamp from main.idx). Used by `dcfh status` and
// by the byte-equivalent (main, fs-scan) branch of `dcfh diff`.
func statusFraming(ctx context.Context, repo dcfh.Repo, repoRoot string) (relCwd, sinceStr string, info *dcfh.RepoInfo, err error) {
	cwd, _ := os.Getwd()
	relCwd, _ = filepath.Rel(repoRoot, cwd)
	if relCwd == "." {
		relCwd = ""
	}
	info, err = repo.Info(ctx)
	if err != nil {
		return "", "", nil, err
	}
	if !info.IndexTimestamp.IsZero() {
		sinceStr = info.IndexTimestamp.Format(time.RFC3339)
	} else if fi, statErr := os.Stat(info.IndexFile); statErr == nil {
		sinceStr = fi.ModTime().UTC().Format(time.RFC3339)
	}
	return relCwd, sinceStr, info, nil
}

// formatFileSize formats a file size in bytes to a human-readable string
func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
