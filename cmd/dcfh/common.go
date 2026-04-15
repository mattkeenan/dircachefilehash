package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
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
	ModifiedCount int   `json:"modified_count"`
	AddedCount    int   `json:"added_count"`
	DeletedCount  int   `json:"deleted_count"`
	ModifiedBytes int64 `json:"modified_bytes"`
	AddedBytes    int64 `json:"added_bytes"`
	DeletedBytes  int64 `json:"deleted_bytes"`
	HasChanges    bool  `json:"has_changes"`
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

// buildFlags builds a flags map for package calls from cobra flag values.
func buildFlags() map[string]string {
	flags := make(map[string]string)

	if flagVerbose > 0 {
		flags["v"] = fmt.Sprintf("%d", flagVerbose)
	}

	if flagFilehash != "" {
		flags["filehash"] = flagFilehash
	}

	flags["symlinks"] = flagSymlinks

	if flagHashWorkers > 0 {
		flags["hash_workers"] = fmt.Sprintf("%d", flagHashWorkers)
	}

	if flagIndexLockTimeout > 0 {
		flags["index_lock_timeout"] = fmt.Sprintf("%d", flagIndexLockTimeout)
	}

	return flags
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

// findDcfhRepo finds the .dcfh directory by walking up the directory tree.
// Results are cached after the first successful call (populated by PersistentPreRunE).
func findDcfhRepo() (string, string, error) {
	if cachedRepoRoot != "" {
		return cachedRepoRoot, cachedDcfhDir, nil
	}

	currentDir, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Walk up the directory tree looking for .dcfh
	for {
		dcfhPath := filepath.Join(currentDir, ".dcfh")
		if stat, err := os.Stat(dcfhPath); err == nil && stat.IsDir() {
			cachedRepoRoot = currentDir
			cachedDcfhDir = dcfhPath
			return currentDir, dcfhPath, nil
		}

		parent := filepath.Dir(currentDir)

		if parent == currentDir {
			break
		}
		currentDir = parent
	}

	return "", "", fmt.Errorf("not in a dcfh repository (no .dcfh directory found)")
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
