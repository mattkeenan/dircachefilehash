package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// Global options parser
var options *ParsedOptions

// initialiseOptions sets up the global options definitions
func initialiseOptions() {
	options = NewParsedOptions()

	// Define all global options
	options.DefineOption("output", "o", OptionTypeString, "human", "output format: human, json, fdupes")
	options.DefineOption("json", "j", OptionTypeBool, "", "output in JSON format (alias for --output=json)")
	options.DefineOption("verbose", "v", OptionTypeInt, "0", "verbose level (1=basic, 2=detailed, 3=trace)")
	options.DefineOption("version", "", OptionTypeBool, "", "show version information")
	options.DefineOption("debug", "D", OptionTypeString, "", "debug options (comma-separated): extravalidation,memorylayout,indexchaining,scanning")
	options.DefineOption("filehash", "f", OptionTypeString, "", "hash algorithm overrides (format: default:sha256)")
	options.DefineOption("symlinks", "", OptionTypeString, "none", "symlink handling: all, internal, external, none (can append ,strict)")
	options.DefineOption("s", "", OptionTypeBool, "", "follow symlinked directories (alias for --symlinks=all)")
	options.DefineOption("hash-workers", "w", OptionTypeInt, "0", "number of concurrent hash workers (0=use config default)")
	options.DefineOption("index-lock-timeout", "", OptionTypeInt, "0", "timeout in seconds for index memory locks (0=use config default)")
	options.DefineOption("dry-run", "", OptionTypeBool, "", "show what would be done without actually doing it")
	options.DefineOption("help", "h", OptionTypeBool, "", "show this help message")
}

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

// showUsage displays the main help message
func showUsage() {
	if options != nil {
		// Use the new options system to show usage
		options.ShowUsage("dcfh")
	} else {
		// Fallback for early errors before options are initialised
		fmt.Fprintf(os.Stderr, "Usage: dcfh [GLOBAL_OPTIONS] <command> [COMMAND_OPTIONS]\n")
		fmt.Fprintf(os.Stderr, "Run 'dcfh --help' for detailed usage information.\n")
		return
	}

	fmt.Fprintf(os.Stderr, "\nCommands:\n")
	fmt.Fprintf(os.Stderr, "  init <dir>       Initialise a new dcfh repository in the specified directory\n")
	fmt.Fprintf(os.Stderr, "  status           Show the status of files in the current dcfh repository\n")
	fmt.Fprintf(os.Stderr, "  update [paths...] Update the index with current file states\n")
	fmt.Fprintf(os.Stderr, "  dupes            Find and display duplicate files\n")
	fmt.Fprintf(os.Stderr, "  snapshot <subcommand> Create and manage index state snapshots (create, list, forget, remove, status)\n")
	fmt.Fprintf(os.Stderr, "  subrepo <subcommand>  Discover and manage nested repositories (find, add)\n")
	fmt.Fprintf(os.Stderr, "  config           Get and set repository configuration options\n")
	fmt.Fprintf(os.Stderr, "  version          Show version information\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh init .\n")
	fmt.Fprintf(os.Stderr, "  dcfh init /home/user/documents\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json status\n")
	fmt.Fprintf(os.Stderr, "  dcfh --output=json status\n")
	fmt.Fprintf(os.Stderr, "  dcfh -vv update\n")
	fmt.Fprintf(os.Stderr, "  dcfh --verbose=2 update\n")
	fmt.Fprintf(os.Stderr, "  dcfh update file.txt dir/\n")
	fmt.Fprintf(os.Stderr, "  dcfh --filehash=default:sha1 update\n")
	fmt.Fprintf(os.Stderr, "  dcfh --hash-workers=8 update\n")
	fmt.Fprintf(os.Stderr, "  dcfh -w 8 update\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json dupes\n")
	fmt.Fprintf(os.Stderr, "  dcfh --output=fdupes dupes\n")
	fmt.Fprintf(os.Stderr, "  dcfh snapshot create\n")
	fmt.Fprintf(os.Stderr, "  dcfh snapshot list\n")
	fmt.Fprintf(os.Stderr, "  dcfh config filehash.default sha1\n")
	fmt.Fprintf(os.Stderr, "  dcfh config output.format fdupes\n")
	fmt.Fprintf(os.Stderr, "  dcfh config --list\n")
	fmt.Fprintf(os.Stderr, "\nFor advanced index management and repository diagnostics, use 'dcfhtool'.\n")
}

// handleVerboseRepetition handles GNU-style verbose flags (-v, -vv, -vvv)
// This is called after options parsing to handle special case of repeated -v flags
func handleVerboseRepetition() {
	// The new options parser already handles -vvv properly
	// This function exists for future extensions if needed
	verboseLevel := options.GetInt("verbose")

	// Set global verbose level for trace logging
	if verboseLevel > 0 {
		dcfh.SetVerboseLevel(verboseLevel)
	}
}

// buildFlags builds a flags map for package calls
func buildFlags() map[string]string {
	flags := make(map[string]string)

	// Use verbose level directly
	verboseLevel := options.GetInt("verbose")
	if verboseLevel > 0 {
		flags["v"] = fmt.Sprintf("%d", verboseLevel)
	}

	// Set debug flags
	debugStr := options.GetString("debug")
	if debugStr != "" {
		dcfh.SetDebugFlags(debugStr)
	}

	// Set hash algorithm override
	filehashStr := options.GetString("filehash")
	if filehashStr != "" {
		flags["filehash"] = filehashStr
	}

	// Set symlink handling mode
	symlinkMode := options.GetString("symlinks")
	if options.GetBool("s") {
		// -s flag overrides --symlinks setting
		symlinkMode = "all"
	}
	flags["symlinks"] = symlinkMode

	// Set hash workers if specified
	hashWorkers := options.GetInt("hash-workers")
	if hashWorkers > 0 {
		flags["hash_workers"] = fmt.Sprintf("%d", hashWorkers)
	}

	// Set index lock timeout if specified
	indexLockTimeout := options.GetInt("index-lock-timeout")
	if indexLockTimeout > 0 {
		flags["index_lock_timeout"] = fmt.Sprintf("%d", indexLockTimeout)
	}

	return flags
}

// validateOutputFormat validates and returns the output format
func validateOutputFormat() OutputFormat {
	// Handle --json flag as alias for --output=json
	jsonFlag := options.GetBool("json")
	outputFormat := options.GetString("output")

	if jsonFlag {
		if outputFormat != "human" {
			fmt.Fprintf(os.Stderr, "Error: cannot use both --json and --output flags together\n")
			os.Exit(1)
		}
		return OutputJSON
	}

	switch outputFormat {
	case "human":
		return OutputHuman
	case "json":
		return OutputJSON
	case "fdupes":
		return OutputFdupes
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid output format '%s'. Supported formats: human, json, fdupes\n", outputFormat)
		os.Exit(1)
		return OutputHuman // unreachable
	}
}

// getEffectiveOutputFormat determines the output format based on config and CLI flags
func getEffectiveOutputFormat(cache *dcfh.DirectoryCache) OutputFormat {
	// CLI flags take precedence
	if options.GetBool("json") {
		return OutputJSON
	}

	// Check if output flag was explicitly set by examining the command line args
	wasOutputFlagSet := false
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--output=") || arg == "--output" {
			wasOutputFlagSet = true
			break
		}
	}

	if wasOutputFlagSet {
		return validateOutputFormat()
	}

	// Use config default
	if cache != nil {
		config := cache.GetConfig()
		if config != nil {
			outputConfig := config.GetOutputConfig()
			switch outputConfig.Format {
			case "json":
				return OutputJSON
			case "human":
				return OutputHuman
			case "fdupes":
				return OutputFdupes
			}
		}
	}

	// Final fallback
	return OutputHuman
}

// outputError outputs an error message in the appropriate format
func outputError(message string) {
	format := validateOutputFormat()
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
	format := validateOutputFormat()
	if format == OutputJSON {
		// For info messages in JSON mode, we could output to a different structure
		// For now, just output as a simple success message
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

// findDcfhRepo finds the .dcfh directory by walking up the directory tree
func findDcfhRepo() (string, string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Walk up the directory tree looking for .dcfh
	for {
		dcfhPath := fmt.Sprintf("%s/.dcfh", currentDir)
		if stat, err := os.Stat(dcfhPath); err == nil && stat.IsDir() {
			return currentDir, dcfhPath, nil
		}

		parent := fmt.Sprintf("%s/..", currentDir)
		parent, err = filepath.Abs(parent)
		if err != nil {
			return "", "", fmt.Errorf("failed to get absolute path of parent: %w", err)
		}

		// Check if we've reached the root
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
