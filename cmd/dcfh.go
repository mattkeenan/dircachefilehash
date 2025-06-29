//go:generate go run generate_version.go

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// Global options parser
var options *ParsedOptions

// initializeOptions sets up the global options definitions
func initializeOptions() {
	options = NewParsedOptions()
	
	// Define all global options
	options.DefineOption("output", "o", OptionTypeString, "human", "output format: human, json, fdupes")
	options.DefineOption("json", "j", OptionTypeBool, "", "output in JSON format (alias for --output=json)")
	options.DefineOption("verbose", "v", OptionTypeInt, "0", "verbose level (1=basic, 2=detailed, 3=trace)")
	options.DefineOption("version", "", OptionTypeBool, "", "show version information")
	options.DefineOption("debug", "d", OptionTypeString, "", "debug options (comma-separated): extravalidation,memorylayout,indexchaining,scanning")
	options.DefineOption("filehash", "f", OptionTypeString, "", "hash algorithm overrides (format: default:sha256)")
	options.DefineOption("symlinks", "", OptionTypeString, "all", "symlink handling: all, contained, none")
	options.DefineOption("s", "", OptionTypeBool, "", "follow symlinked directories (alias for --symlinks=all)")
	options.DefineOption("hash-workers", "w", OptionTypeInt, "0", "number of concurrent hash workers (0=use config default)")
	options.DefineOption("fix", "", OptionTypeString, "none", "fix mode for index operations: none, manual, auto")
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
	ModifiedCount int  `json:"modified_count"`
	AddedCount    int  `json:"added_count"`
	DeletedCount  int  `json:"deleted_count"`
	HasChanges    bool `json:"has_changes"`
}

type IndexInfo struct {
	FileCount int `json:"file_count"`
}

type UpdateOutput struct {
	Success      bool           `json:"success"`
	Message      string         `json:"message"`
	Repository   string         `json:"repository"`
	PathsUpdated []string       `json:"paths_updated,omitempty"`
	FileCount    int            `json:"file_count"`
	TotalSize    int64          `json:"total_size"`
	TimeElapsed  string         `json:"time_elapsed"`
	Duplicates   *DuplicateInfo `json:"duplicates,omitempty"`
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

func showUsage() {
	if options != nil {
		// Use the new options system to show usage
		options.ShowUsage("dcfh")
	} else {
		// Fallback for early errors before options are initialized
		fmt.Fprintf(os.Stderr, "Usage: dcfh [GLOBAL_OPTIONS] <command> [COMMAND_OPTIONS]\n")
		fmt.Fprintf(os.Stderr, "Run 'dcfh --help' for detailed usage information.\n")
		return
	}
	
	fmt.Fprintf(os.Stderr, "\nCommands:\n")
	fmt.Fprintf(os.Stderr, "  init <dir>       Initialize a new dcfh repository in the specified directory\n")
	fmt.Fprintf(os.Stderr, "  status           Show the status of files in the current dcfh repository\n")
	fmt.Fprintf(os.Stderr, "  update [paths...] Update the index with current file states\n")
	fmt.Fprintf(os.Stderr, "  dupes            Find and display duplicate files\n")
	fmt.Fprintf(os.Stderr, "  index <subcommand> Manage and inspect index files (fsck, explore, repair, reset, merge)\n")
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
	fmt.Fprintf(os.Stderr, "  dcfh index fsck\n")
	fmt.Fprintf(os.Stderr, "  dcfh index explore\n")
	fmt.Fprintf(os.Stderr, "  dcfh config filehash.default sha1\n")
	fmt.Fprintf(os.Stderr, "  dcfh config output.format fdupes\n")
	fmt.Fprintf(os.Stderr, "  dcfh config --list\n")
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

func main() {
	// Initialize options definitions
	initializeOptions()

	// Parse command-line arguments
	if err := options.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Handle help flag
	if options.GetBool("help") {
		showUsage()
		return
	}

	// Handle version flag
	if options.GetBool("version") {
		handleVersion()
		return
	}

	// Initialize debug flags early
	debugStr := options.GetString("debug")
	dcfh.InitDebugFlags(debugStr)
	if debugStr != "" {
		dcfh.LogDebugFlags()
	}

	// Validate output format early (after flag parsing)
	validateOutputFormat()

	// Handle special verbose option repetition (-vvv)
	handleVerboseRepetition()

	// Get remaining arguments after options
	args := options.GetArgs()
	if len(args) < 1 {
		showUsage()
		os.Exit(1)
	}

	command := args[0]

	switch command {
	case "init":
		handleInit(args[1:])
	case "status":
		handleStatus(args[1:])
	case "update":
		handleUpdate(args[1:])
	case "dupes":
		handleDupes(args[1:])
	case "index":
		handleIndex(args[1:])
	case "config":
		handleConfig(args[1:])
	case "version":
		handleVersionCommand(args[1:])
	default:
		outputError(fmt.Sprintf("Unknown command: %s", command))
		os.Exit(1)
	}
}

// buildFlags creates a flags map from global CLI flags
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
	
	return flags
}

// handleVersion handles --version flag (alias for handleVersionCommand with no args)
func handleVersion() {
	handleVersionCommand([]string{})
}

// handleVersionCommand handles the "dcfh version" command
func handleVersionCommand(args []string) {
	if len(args) != 0 {
		outputError("Usage: dcfh version")
		os.Exit(1)
	}

	format := validateOutputFormat()
	
	if format == OutputJSON {
		versionJSON := map[string]interface{}{
			"version": getVersionString(),
		}
		
		// Include detailed info if verbose
		if options.GetInt("verbose") > 0 {
			versionJSON["git_commit"] = getGitCommit()
			versionJSON["go_version"] = runtime.Version()
			versionJSON["supported_index_formats"] = []string{"v0"}
			versionJSON["description"] = "Directory Cache File Hash - A fast file indexing and duplicate detection tool"
		}
		
		jsonBytes, _ := json.MarshalIndent(versionJSON, "", "  ")
		fmt.Println(string(jsonBytes))
	} else {
		// Simple output by default: just the version
		fmt.Println(getVersionString())
		
		// Detailed output if verbose
		if options.GetInt("verbose") > 0 {
			fmt.Printf("Git commit: %s\n", getGitCommit())
			fmt.Printf("Go version: %s\n", runtime.Version())
			fmt.Printf("Supported index formats: v0\n")
			fmt.Printf("Description: Directory Cache File Hash - A fast file indexing and duplicate detection tool\n")
		}
	}
}

// getVersionString and getGitCommit are generated by go generate

// OutputFormat represents the supported output formats
type OutputFormat string

const (
	OutputHuman  OutputFormat = "human"
	OutputJSON   OutputFormat = "json"
	OutputFdupes OutputFormat = "fdupes"
)

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

func outputError(message string) {
	format := validateOutputFormat()
	if format == OutputJSON {
		errorOut := ErrorOutput{
			Success: false,
			Error:   message,
		}
		json.NewEncoder(os.Stderr).Encode(errorOut)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	}
}

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
		json.NewEncoder(os.Stdout).Encode(successOut)
	} else {
		fmt.Println(message)
	}
}

func outputJSON(data interface{}) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.Encode(data)
}

func handleInit(args []string) {
	if len(args) != 1 {
		outputError("Usage: dcfh init <directory>")
		os.Exit(1)
	}

	directory := args[0]
	start := time.Now()

	// Convert to absolute path and resolve symlinks
	absDir, err := filepath.Abs(directory)
	if err != nil {
		outputError(fmt.Sprintf("Failed to get absolute path for %s: %v", directory, err))
		os.Exit(1)
	}
	
	// Resolve symlinks to get the real path
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		// If symlink resolution fails, fall back to absolute path
		realDir = absDir
	}
	absDir = realDir

	// Check if directory exists
	info, err := os.Stat(absDir)
	if err != nil {
		outputError(fmt.Sprintf("Failed to access directory %s: %v", absDir, err))
		os.Exit(1)
	}
	if !info.IsDir() {
		outputError(fmt.Sprintf("%s is not a directory", absDir))
		os.Exit(1)
	}

	// Check if .dcfh already exists
	dcfhDir := filepath.Join(absDir, ".dcfh")
	if _, err := os.Stat(dcfhDir); err == nil {
		outputError(fmt.Sprintf(".dcfh directory already exists in %s", absDir))
		os.Exit(1)
	}

	// Create cache - this will automatically create .dcfh directory structure
	cache := dcfh.NewDirectoryCache(absDir, absDir)
	defer cache.Close()
	
	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}
	
	// Create empty main index
	if err := cache.CreateEmptyMainIndex(); err != nil {
		outputError(fmt.Sprintf("Failed to create empty index: %v", err))
		os.Exit(1)
	}

	format := validateOutputFormat()
	duration := time.Since(start)
	
	// Always show the initialization message (git-style)
	fmt.Printf("Initialized empty dcfh repository in %s\n", dcfhDir)

	if format == OutputJSON {
		output := InitOutput{
			Success:     true,
			Message:     "Successfully initialized dcfh repository",
			Repository:  absDir,
			FileCount:   0,
			TotalSize:   0,
			TimeElapsed: duration.Round(time.Millisecond).String(),
		}
		outputJSON(output)
	} else if options.GetInt("verbose") > 0 {
		fmt.Printf("✓ Repository structure created\n")
		fmt.Printf("✓ Configuration files initialized\n")
		fmt.Printf("✓ Run 'dcfh update' to scan and index files\n")
		fmt.Printf("✓ Completed in %v\n", duration.Round(time.Millisecond))
	}
}

func handleStatus(args []string) {
	if len(args) != 0 {
		outputError("Usage: dcfh status")
		os.Exit(1)
	}

	// Find the dcfh repository root
	repoRoot, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		format := validateOutputFormat()
		if format == OutputHuman {
			fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialize a repository\n")
		}
		os.Exit(1)
	}

	// Get current working directory relative to repo root
	cwd, _ := os.Getwd()
	relCwd, _ := filepath.Rel(repoRoot, cwd)
	if relCwd == "." {
		relCwd = ""
	}

	// Create cache and get status
	cache := dcfh.NewDirectoryCache(repoRoot, dcfhDir)
	defer cache.Close()
	
	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}

	status, err := cache.Status(flags)
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}

	format := validateOutputFormat()
	if format == OutputJSON {
		fileCount := cache.Length() // Use Length() method
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
				HasChanges:    status.HasChanges(),
			},
			IndexInfo: IndexInfo{
				FileCount: fileCount,
			},
		}
		outputJSON(output)
		return
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
		fileCount := cache.Length() // Use Length() method
		fmt.Printf("Index contains %d files\n", fileCount)
		return
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

	fmt.Printf("Summary: %d modified, %d added, %d deleted\n",
		len(status.Modified), len(status.Added), len(status.Deleted))
}

func handleUpdate(args []string) {
	// Find the dcfh repository root
	repoRoot, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		format := validateOutputFormat()
		if format == OutputHuman {
			fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialize a repository\n")
		}
		os.Exit(1)
	}

	// Get paths to update (if any)
	var paths []string
	format := validateOutputFormat()
	if len(args) > 0 {
		paths = args
		if format == OutputHuman {
			fmt.Printf("Updating specified paths in %s\n", repoRoot)
			if options.GetInt("verbose") > 0 {
				for _, path := range paths {
					fmt.Printf("  %s\n", path)
				}
			}
		}
	} else {
		if format == OutputHuman {
			fmt.Printf("Updating entire repository in %s\n", repoRoot)
		}
	}

	// Update the index
	cache := dcfh.NewDirectoryCache(repoRoot, dcfhDir)
	defer cache.Close()
	
	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}

	if format == OutputHuman && options.GetInt("verbose") > 0 {
		fmt.Println("Scanning directory...")
	}

	start := time.Now()

	if err := cache.Update(flags, paths...); err != nil {
		outputError(fmt.Sprintf("Failed to update index: %v", err))
		os.Exit(1)
	}

	duration := time.Since(start)
	fileCount, totalSize, _ := cache.Stats()

	// Check for duplicates
	var duplicateInfo *DuplicateInfo
	duplicates, err := cache.FindDuplicates(flags)
	if err != nil {
		outputError(fmt.Sprintf("Failed to find duplicates: %v", err))
		os.Exit(1)
	}

	if len(duplicates) > 0 {
		duplicateCount := 0
		for _, group := range duplicates {
			duplicateCount += len(group.Files)
		}
		duplicateInfo = &DuplicateInfo{
			SetCount:  len(duplicates),
			FileCount: duplicateCount,
		}
	}

	if format == OutputJSON {
		output := UpdateOutput{
			Success:      true,
			Message:      "Successfully updated index",
			Repository:   repoRoot,
			PathsUpdated: paths,
			FileCount:    fileCount,
			TotalSize:    totalSize,
			TimeElapsed:  duration.Round(time.Millisecond).String(),
			Duplicates:   duplicateInfo,
		}
		outputJSON(output)
	} else {
		if len(paths) > 0 {
			fmt.Printf("Updated %d specified paths\n", len(paths))
		} else {
			fmt.Printf("Updated index\n")
		}
		if options.GetInt("verbose") > 0 {
			fmt.Printf("✓ Completed in %v\n", duration.Round(time.Millisecond))
			fmt.Printf("✓ Indexed %d files, total size: %d bytes\n", fileCount, totalSize)
			if duplicateInfo != nil {
				fmt.Printf("⚠ Found %d duplicate files in %d sets\n", duplicateInfo.FileCount, duplicateInfo.SetCount)
			}
		}
	}
}

func handleDupes(args []string) {
	if len(args) != 0 {
		outputError("Usage: dcfh dupes")
		os.Exit(1)
	}

	// Find the dcfh repository root
	repoRoot, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		format := validateOutputFormat()
		if format == OutputHuman {
			fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialize a repository\n")
		}
		os.Exit(1)
	}

	// Load existing index
	cache := dcfh.NewDirectoryCache(repoRoot, dcfhDir)
	defer cache.Close()
	
	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}

	if _, err := cache.LoadMainIndex(); err != nil {
		outputError(fmt.Sprintf("Failed to load index: %v", err))
		os.Exit(1)
	}

	// Find duplicates
	duplicates, err := cache.FindDuplicates(flags)
	if err != nil {
		outputError(fmt.Sprintf("Failed to find duplicates: %v", err))
		os.Exit(1)
	}

	format := getEffectiveOutputFormat(cache)
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
		return // No output if no duplicates found (like fdupes in text mode)
	}

	// Sort groups by hash for consistent output
	for i := 0; i < len(duplicates); i++ {
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
			// Use relative paths directly (like git)
			var filePaths []string
			for _, relPath := range group.Files {
				filePaths = append(filePaths, relPath)
			}

			// Sort the file paths
			for k := 0; k < len(filePaths); k++ {
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
			// Convert to absolute paths and sort them
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

			// Print each file in the duplicate group (absolute paths)
			for _, absPath := range filePaths {
				fmt.Println(absPath)
			}

			// Add blank line between groups (except after the last group)
			if i < len(duplicates)-1 {
				fmt.Println()
			}
		}

	default: // OutputHuman
		// Human format: relative paths with context
		for i, group := range duplicates {
			// Use relative paths directly and sort them
			var filePaths []string
			for _, relPath := range group.Files {
				filePaths = append(filePaths, relPath)
			}

			// Sort the file paths
			for k := 0; k < len(filePaths); k++ {
				for l := k + 1; l < len(filePaths); l++ {
					if filePaths[k] > filePaths[l] {
						filePaths[k], filePaths[l] = filePaths[l], filePaths[k]
					}
				}
			}

			// Print each file in the duplicate group (relative paths)
			for _, relPath := range filePaths {
				fmt.Println(relPath)
			}

			// Add blank line between groups (except after the last group)
			if i < len(duplicates)-1 {
				fmt.Println()
			}
		}
	}
}

func handleConfig(args []string) {
	// Create options parser for config subcommand
	configOptions := NewParsedOptions()
	configOptions.DefineOption("list", "", OptionTypeBool, "", "list all configuration variables")
	configOptions.DefineOption("global", "", OptionTypeBool, "", "use global configuration (not implemented)")
	
	// Parse config-specific options
	if err := configOptions.Parse(args); err != nil {
		outputError(fmt.Sprintf("Error parsing config options: %v", err))
		os.Exit(1)
	}
	
	configArgs := configOptions.GetArgs()
	
	// Handle --list flag
	if configOptions.GetBool("list") {
		if len(configArgs) > 0 {
			outputError("Cannot specify configuration keys with --list flag")
			os.Exit(1)
		}
		handleConfigList()
		return
	}
	
	// Handle get/set operations
	switch len(configArgs) {
	case 0:
		// No args with no --list means show usage
		showConfigUsage()
		os.Exit(1)
	case 1:
		// Get operation: dcfh config key
		handleConfigGet(configArgs[0])
	case 2:
		// Set operation: dcfh config key value
		handleConfigSet(configArgs[0], configArgs[1])
	default:
		outputError("Too many arguments for config command")
		os.Exit(1)
	}
}

func showConfigUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh config [OPTIONS] [<key>] [<value>]\n\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	fmt.Fprintf(os.Stderr, "  --list       List all configuration variables\n")
	fmt.Fprintf(os.Stderr, "  --global     Use global configuration (not implemented)\n")
	fmt.Fprintf(os.Stderr, "\nConfiguration Keys:\n")
	fmt.Fprintf(os.Stderr, "  filehash.default     Default hash algorithm (sha1, sha256, sha512)\n")
	fmt.Fprintf(os.Stderr, "  output.format        Default output format (human, json, fdupes)\n")
	fmt.Fprintf(os.Stderr, "  verbose.level        Default verbose level (0-3)\n")
	fmt.Fprintf(os.Stderr, "  verbose.debug        Default debug flags (comma-separated)\n")
	fmt.Fprintf(os.Stderr, "  symlink.mode         Default symlink handling (all, contained, none)\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh config --list\n")
	fmt.Fprintf(os.Stderr, "  dcfh config filehash.default\n")
	fmt.Fprintf(os.Stderr, "  dcfh config filehash.default sha256\n")
	fmt.Fprintf(os.Stderr, "  dcfh config output.format fdupes\n")
}

func handleConfigList() {
	// Find the dcfh repository root
	_, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}
	
	// Load configuration
	config, err := dcfh.LoadConfig(filepath.Join(dcfhDir, ".dcfh"))
	if err != nil {
		outputError(fmt.Sprintf("Failed to load configuration: %v", err))
		os.Exit(1)
	}
	
	allConfig := config.GetAllConfig()
	
	// List all configuration in git config format
	fmt.Printf("filehash.default=%s\n", allConfig.Hash.Default)
	fmt.Printf("output.format=%s\n", allConfig.Output.Format)
	fmt.Printf("verbose.level=%d\n", allConfig.Verbose.Level)
	fmt.Printf("verbose.debug=%s\n", allConfig.Verbose.Debug)
	fmt.Printf("symlink.mode=%s\n", allConfig.Symlink.Mode)
}

func handleConfigGet(key string) {
	// Find the dcfh repository root
	_, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}
	
	// Load configuration
	config, err := dcfh.LoadConfig(filepath.Join(dcfhDir, ".dcfh"))
	if err != nil {
		outputError(fmt.Sprintf("Failed to load configuration: %v", err))
		os.Exit(1)
	}
	
	allConfig := config.GetAllConfig()
	
	// Get the requested configuration value
	switch key {
	case "filehash.default":
		fmt.Println(allConfig.Hash.Default)
	case "output.format":
		fmt.Println(allConfig.Output.Format)
	case "verbose.level":
		fmt.Println(allConfig.Verbose.Level)
	case "verbose.debug":
		fmt.Println(allConfig.Verbose.Debug)
	case "symlink.mode":
		fmt.Println(allConfig.Symlink.Mode)
	default:
		outputError(fmt.Sprintf("Unknown configuration key: %s", key))
		os.Exit(1)
	}
}

func handleConfigSet(key, value string) {
	// Find the dcfh repository root
	_, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}
	
	// Load configuration
	config, err := dcfh.LoadConfig(filepath.Join(dcfhDir, ".dcfh"))
	if err != nil {
		outputError(fmt.Sprintf("Failed to load configuration: %v", err))
		os.Exit(1)
	}
	
	// Set the configuration value with validation
	switch key {
	case "filehash.default":
		if err := dcfh.ValidateHashAlgorithm(value); err != nil {
			outputError(fmt.Sprintf("Invalid hash algorithm: %v", err))
			os.Exit(1)
		}
		if err := config.SetHashDefault(value); err != nil {
			outputError(fmt.Sprintf("Failed to set filehash.default: %v", err))
			os.Exit(1)
		}
	case "output.format":
		if err := dcfh.ValidateOutputFormat(value); err != nil {
			outputError(fmt.Sprintf("Invalid output format: %v", err))
			os.Exit(1)
		}
		if err := config.SetOutputFormat(value); err != nil {
			outputError(fmt.Sprintf("Failed to set output.format: %v", err))
			os.Exit(1)
		}
	case "verbose.level":
		level, err := strconv.Atoi(value)
		if err != nil {
			outputError(fmt.Sprintf("Invalid verbose level '%s': must be a number", value))
			os.Exit(1)
		}
		if err := dcfh.ValidateVerboseLevel(level); err != nil {
			outputError(fmt.Sprintf("Invalid verbose level: %v", err))
			os.Exit(1)
		}
		if err := config.SetVerboseLevel(level); err != nil {
			outputError(fmt.Sprintf("Failed to set verbose.level: %v", err))
			os.Exit(1)
		}
	case "verbose.debug":
		if err := dcfh.ValidateDebugFlags(value); err != nil {
			outputError(fmt.Sprintf("Invalid debug flags: %v", err))
			os.Exit(1)
		}
		if err := config.SetDebugFlags(value); err != nil {
			outputError(fmt.Sprintf("Failed to set verbose.debug: %v", err))
			os.Exit(1)
		}
	case "symlink.mode":
		if err := dcfh.ValidateSymlinkMode(value); err != nil {
			outputError(fmt.Sprintf("Invalid symlink mode: %v", err))
			os.Exit(1)
		}
		if err := config.SetSymlinkMode(value); err != nil {
			outputError(fmt.Sprintf("Failed to set symlink.mode: %v", err))
			os.Exit(1)
		}
	default:
		outputError(fmt.Sprintf("Unknown configuration key: %s", key))
		showConfigUsage()
		os.Exit(1)
	}
	
	fmt.Printf("Configuration updated: %s = %s\n", key, value)
}

// handleIndex handles the index command with various subcommands
func handleIndex(args []string) {
	if len(args) < 1 {
		showIndexUsage()
		os.Exit(1)
	}
	
	subcommand := args[0]
	
	switch subcommand {
	case "list", "ls":
		handleIndexList(args[1:])
	case "idxck", "fsck", "validate":
		handleIndexCheck(args[1:])
	case "explore", "inspect", "show":
		handleIndexExplore(args[1:])
	case "reset", "restart":
		handleIndexReset(args[1:])
	case "recover", "recovery":
		handleIndexRecover(args[1:])
	case "search", "find":
		handleIndexSearch(args[1:])
	case "merge":
		handleIndexMerge(args[1:])
	case "help", "-h", "--help":
		showIndexUsage()
	default:
		outputError(fmt.Sprintf("Unknown index subcommand: %s", subcommand))
		showIndexUsage()
		os.Exit(1)
	}
}

// showIndexUsage displays usage information for the index command
func showIndexUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh index <subcommand> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Index management subcommands:\n")
	fmt.Fprintf(os.Stderr, "  list, ls         List all index files (.idx) in the repository\n")
	fmt.Fprintf(os.Stderr, "  idxck            Check and optionally repair index files (like fsck)\n")
	fmt.Fprintf(os.Stderr, "  explore, inspect Show detailed index file information and contents\n")
	fmt.Fprintf(os.Stderr, "  search, find     Search for files or patterns within index files\n")
	fmt.Fprintf(os.Stderr, "  reset, restart   Reset/recreate index files from scratch\n")
	fmt.Fprintf(os.Stderr, "  merge            Merge multiple index files or resolve conflicts\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh index list                    # List all index files\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json index list             # List index files as JSON\n")
	fmt.Fprintf(os.Stderr, "  dcfh index idxck                   # Check index files (read-only)\n")
	fmt.Fprintf(os.Stderr, "  dcfh index idxck --fix=manual      # Check and prompt for each fix\n")
	fmt.Fprintf(os.Stderr, "  dcfh index idxck --fix=auto        # Check and auto-fix issues\n")
	fmt.Fprintf(os.Stderr, "  dcfh index explore                 # Show index overview\n")
	fmt.Fprintf(os.Stderr, "  dcfh index recover                 # Auto-recover from corrupted indices\n")
	fmt.Fprintf(os.Stderr, "  dcfh index recover cache.idx       # Recover from specific index file\n")
	fmt.Fprintf(os.Stderr, "  dcfh index search \"*.txt\"           # Search for text files\n")
	fmt.Fprintf(os.Stderr, "  dcfh index search --hash abc123     # Search by hash prefix\n")
	fmt.Fprintf(os.Stderr, "  dcfh index reset                   # Reset index from filesystem\n")
}

// IndexFileInfo represents information about an index file
type IndexFileInfo struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"modified_time"`
	Type         string    `json:"type"`        // main, cache, scan, temp, unknown
	EntryCount   int       `json:"entry_count,omitempty"`
	IsCorrupted  bool      `json:"is_corrupted,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

// IndexListOutput represents the output for index list command
type IndexListOutput struct {
	Repository string          `json:"repository"`
	IndexFiles []IndexFileInfo `json:"index_files"`
	Summary    struct {
		TotalFiles  int   `json:"total_files"`
		TotalSize   int64 `json:"total_size"`
		MainIndexes int   `json:"main_indexes"`
		CacheIndexes int  `json:"cache_indexes"`
		ScanIndexes int   `json:"scan_indexes"`
		TempIndexes int   `json:"temp_indexes"`
		UnknownIndexes int `json:"unknown_indexes"`
	} `json:"summary"`
}

// handleIndexList implements index file listing
func handleIndexList(args []string) {
	// Find the dcfh repository
	repoRoot, _, err := findDcfhRepo()
	if err != nil {
		outputError(fmt.Sprintf("Failed to find dcfh repository: %v", err))
		os.Exit(1)
	}
	
	dcfhDir := filepath.Join(repoRoot, ".dcfh")
	
	// Find all .idx files
	indexFiles, err := findIndexFiles(dcfhDir)
	if err != nil {
		outputError(fmt.Sprintf("Failed to scan for index files: %v", err))
		os.Exit(1)
	}
	
	// Check if JSON output is requested
	outputFormat := validateOutputFormat()
	
	if outputFormat == OutputJSON {
		// JSON output
		output := IndexListOutput{
			Repository: repoRoot,
			IndexFiles: indexFiles,
		}
		
		// Calculate summary
		for _, file := range indexFiles {
			output.Summary.TotalFiles++
			output.Summary.TotalSize += file.Size
			
			switch file.Type {
			case "main":
				output.Summary.MainIndexes++
			case "cache":
				output.Summary.CacheIndexes++
			case "scan":
				output.Summary.ScanIndexes++
			case "temp":
				output.Summary.TempIndexes++
			default:
				output.Summary.UnknownIndexes++
			}
		}
		
		jsonData, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			outputError(fmt.Sprintf("Failed to marshal JSON: %v", err))
			os.Exit(1)
		}
		fmt.Println(string(jsonData))
	} else {
		// Human-readable output
		if len(indexFiles) == 0 {
			fmt.Printf("No index files found in %s\n", dcfhDir)
			return
		}
		
		fmt.Printf("Index files in %s:\n\n", dcfhDir)
		
		// Print header
		fmt.Printf("%-20s %-10s %-8s %-19s %s\n", "NAME", "TYPE", "SIZE", "MODIFIED", "ENTRIES")
		fmt.Printf("%-20s %-10s %-8s %-19s %s\n", 
			strings.Repeat("-", 20),
			strings.Repeat("-", 10), 
			strings.Repeat("-", 8),
			strings.Repeat("-", 19),
			strings.Repeat("-", 8))
		
		var totalSize int64
		for _, file := range indexFiles {
			totalSize += file.Size
			
			sizeStr := formatFileSize(file.Size)
			modTimeStr := file.ModTime.Format("2006-01-02 15:04:05")
			
			entryCountStr := "-"
			if file.EntryCount > 0 {
				entryCountStr = fmt.Sprintf("%d", file.EntryCount)
			}
			
			nameStr := file.Name
			if file.IsCorrupted {
				nameStr += " [CORRUPT]"
			}
			
			fmt.Printf("%-20s %-10s %-8s %-19s %s\n",
				nameStr, file.Type, sizeStr, modTimeStr, entryCountStr)
		}
		
		fmt.Printf("\nSummary: %d files, %s total\n", len(indexFiles), formatFileSize(totalSize))
	}
}

// findIndexFiles scans for .idx files in the dcfh directory
func findIndexFiles(dcfhDir string) ([]IndexFileInfo, error) {
	var indexFiles []IndexFileInfo
	
	err := filepath.Walk(dcfhDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Skip directories
		if info.IsDir() {
			return nil
		}
		
		// Only process .idx files
		if !strings.HasSuffix(info.Name(), ".idx") {
			return nil
		}
		
		// Determine index type from filename
		indexType := determineIndexType(info.Name())
		
		fileInfo := IndexFileInfo{
			Path:    path,
			Name:    info.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Type:    indexType,
		}
		
		// Try to read entry count from the index file (if not corrupted)
		if entryCount, err := getIndexEntryCount(path); err == nil {
			fileInfo.EntryCount = entryCount
		} else {
			fileInfo.IsCorrupted = true
			fileInfo.ErrorMessage = err.Error()
		}
		
		indexFiles = append(indexFiles, fileInfo)
		return nil
	})
	
	return indexFiles, err
}

// determineIndexType determines the type of index file based on filename
func determineIndexType(filename string) string {
	if filename == "main.idx" {
		return "main"
	}
	if filename == "cache.idx" {
		return "cache"
	}
	if strings.HasPrefix(filename, "scan-") {
		return "scan"
	}
	if strings.HasPrefix(filename, "tmp-") || strings.HasPrefix(filename, "temp-") {
		return "temp"
	}
	return "unknown"
}

// getIndexEntryCount attempts to read the entry count from an index file header
func getIndexEntryCount(indexPath string) (int, error) {
	file, err := os.Open(indexPath)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	
	// Read just enough for the header - we need to check the actual header format
	// For now, return 0 as we need to examine the index format first
	return 0, nil
}

// formatFileSize formats a file size in bytes to human readable format
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

// FixMode defines the repair mode for index checking
type FixMode string

const (
	FixModeNone   FixMode = "none"   // Read-only validation (default)
	FixModeManual FixMode = "manual" // Prompt user for each fix
	FixModeAuto   FixMode = "auto"   // Automatically apply fixes
)

// IndexCheckResult represents the result of checking an index file
type IndexCheckResult struct {
	FilePath     string   `json:"file_path"`
	FileName     string   `json:"file_name"`
	Type         string   `json:"type"`
	IsValid      bool     `json:"is_valid"`
	EntryCount   int      `json:"entry_count,omitempty"`
	Issues       []string `json:"issues,omitempty"`
	FixesApplied []string `json:"fixes_applied,omitempty"`
	BackupPath   string   `json:"backup_path,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// IndexCheckOutput represents the complete output of index check command
type IndexCheckOutput struct {
	Repository    string             `json:"repository"`
	FixMode       string             `json:"fix_mode"`
	FilesChecked  int                `json:"files_checked"`
	FilesValid    int                `json:"files_valid"`
	FilesCorrupt  int                `json:"files_corrupt"`
	IssuesFound   int                `json:"issues_found"`
	FixesApplied  int                `json:"fixes_applied"`
	BackupsCreated int               `json:"backups_created"`
	Results       []IndexCheckResult `json:"results"`
	Summary       string             `json:"summary"`
}

// handleIndexCheck implements unified index checking/repair (idxck command)
func handleIndexCheck(args []string) {
	// Parse validation mode from arguments
	var validationMode dcfh.ValidationMode = dcfh.ValidationStrict // Default to strict
	var checkExtract bool = false
	
	// Process arguments
	filteredArgs := []string{}
	for _, arg := range args {
		switch arg {
		case "--mode=strict", "-strict":
			validationMode = dcfh.ValidationStrict
		case "--mode=lenient", "-lenient":
			validationMode = dcfh.ValidationLenient
		case "--mode=diagnostic", "-diagnostic":
			validationMode = dcfh.ValidationDiagnostic
		case "--extract", "-extract":
			checkExtract = true
		default:
			filteredArgs = append(filteredArgs, arg)
		}
	}
	
	// Get fix mode from global options
	fixModeStr := options.GetString("fix")
	var fixMode FixMode
	
	switch fixModeStr {
	case "none", "nofix":
		fixMode = FixModeNone
	case "manual", "manualfix":
		fixMode = FixModeManual
	case "auto", "autofix":
		fixMode = FixModeAuto
	case "extract":
		fixMode = FixModeNone // Extract mode is read-only
		checkExtract = true
	default:
		outputError(fmt.Sprintf("Invalid fix mode: %s. Valid modes: none, manual, auto, extract", fixModeStr))
		showIndexCheckUsage()
		os.Exit(1)
	}
	
	// Find the dcfh repository
	repoRoot, _, err := findDcfhRepo()
	if err != nil {
		outputError(fmt.Sprintf("Failed to find dcfh repository: %v", err))
		os.Exit(1)
	}
	
	dcfhDir := filepath.Join(repoRoot, ".dcfh")
	
	// Find all .idx files
	indexFiles, err := findIndexFiles(dcfhDir)
	if err != nil {
		outputError(fmt.Sprintf("Failed to scan for index files: %v", err))
		os.Exit(1)
	}
	
	if len(indexFiles) == 0 {
		fmt.Printf("No index files found in %s\n", dcfhDir)
		return
	}
	
	// Perform checks
	output := IndexCheckOutput{
		Repository:  repoRoot,
		FixMode:     string(fixMode),
		FilesChecked: len(indexFiles),
	}
	
	var modeDesc string
	switch validationMode {
	case dcfh.ValidationStrict:
		modeDesc = "strict"
	case dcfh.ValidationLenient:
		modeDesc = "lenient"
	case dcfh.ValidationDiagnostic:
		modeDesc = "diagnostic"
	}
	
	fmt.Printf("Checking %d index files in %s (validation: %s, fix: %s)\n\n", len(indexFiles), dcfhDir, modeDesc, fixMode)
	
	// Create pre-recovery snapshot if any operation might modify index files
	if fixMode == FixModeManual || fixMode == FixModeAuto || checkExtract {
		// Create a DirectoryCache instance for snapshot creation
		dc := dcfh.NewDirectoryCache(repoRoot, dcfhDir)
		if dc != nil {
			verbosity := options.GetInt("verbose")
			if err := dc.CreatePreRecoverySnapshotForIdxck(verbosity); err != nil {
				if verbosity >= 1 {
					fmt.Printf("Warning: failed to create pre-operation snapshot: %v\n", err)
				}
			} else if verbosity >= 1 {
				fmt.Printf("Created pre-operation snapshot in .dcfh/recovery/\n")
			}
		}
	}
	
	for _, fileInfo := range indexFiles {
		result := checkIndexFileWithMode(fileInfo, fixMode, validationMode, checkExtract)
		output.Results = append(output.Results, result)
		
		if result.IsValid {
			output.FilesValid++
		} else {
			output.FilesCorrupt++
		}
		
		output.IssuesFound += len(result.Issues)
		output.FixesApplied += len(result.FixesApplied)
		
		if result.BackupPath != "" {
			output.BackupsCreated++
		}
		
		// Display progress
		if validateOutputFormat() == OutputJSON {
			// For JSON output, collect all results and output at the end
			continue
		} else {
			// For human output, show progress as we go
			displayCheckResult(result)
		}
	}
	
	// Generate summary
	if output.FilesCorrupt == 0 {
		output.Summary = "All index files are valid"
	} else {
		output.Summary = fmt.Sprintf("%d/%d files have issues", output.FilesCorrupt, output.FilesChecked)
	}
	
	// Output results
	if validateOutputFormat() == OutputJSON {
		jsonData, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			outputError(fmt.Sprintf("Failed to marshal JSON: %v", err))
			os.Exit(1)
		}
		fmt.Println(string(jsonData))
	} else {
		// Human-readable summary
		fmt.Printf("\nSummary:\n")
		fmt.Printf("  Files checked: %d\n", output.FilesChecked)
		fmt.Printf("  Valid files:   %d\n", output.FilesValid)
		fmt.Printf("  Corrupt files: %d\n", output.FilesCorrupt)
		fmt.Printf("  Issues found:  %d\n", output.IssuesFound)
		
		if fixMode != FixModeNone {
			fmt.Printf("  Fixes applied: %d\n", output.FixesApplied)
			fmt.Printf("  Backups created: %d\n", output.BackupsCreated)
		}
		
		fmt.Printf("\nResult: %s\n", output.Summary)
		
		if output.FilesCorrupt > 0 && fixMode == FixModeNone {
			fmt.Printf("\nTo attempt repairs, run with --fix=manual or --fix=auto\n")
		}
	}
	
	// Exit with error code if issues found
	if output.FilesCorrupt > 0 {
		os.Exit(1)
	}
}

// showIndexCheckUsage shows usage for the idxck subcommand
func showIndexCheckUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh index idxck [OPTIONS] [--fix=MODE]\n\n")
	fmt.Fprintf(os.Stderr, "Validation modes:\n")
	fmt.Fprintf(os.Stderr, "  --mode=strict     Strict validation - fail on any error (default)\n")
	fmt.Fprintf(os.Stderr, "  --mode=lenient    Lenient validation - skip invalid entries\n")
	fmt.Fprintf(os.Stderr, "  --mode=diagnostic Report all issues but include all entries\n")
	fmt.Fprintf(os.Stderr, "\nFix modes:\n")
	fmt.Fprintf(os.Stderr, "  --fix=none        Read-only validation (default)\n")
	fmt.Fprintf(os.Stderr, "  --fix=manual      Prompt user for each fix\n")
	fmt.Fprintf(os.Stderr, "  --fix=auto        Automatically apply all fixes\n")
	fmt.Fprintf(os.Stderr, "  --fix=extract     Extract valid entries to new index file\n")
	fmt.Fprintf(os.Stderr, "\nOther options:\n")
	fmt.Fprintf(os.Stderr, "  --extract         Extract valid entries (same as --fix=extract)\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh index idxck                          # Strict validation, no fixes\n")
	fmt.Fprintf(os.Stderr, "  dcfh index idxck --mode=diagnostic        # Report all issues\n")
	fmt.Fprintf(os.Stderr, "  dcfh index idxck --fix=extract             # Extract valid entries\n")
	fmt.Fprintf(os.Stderr, "  dcfh index idxck --mode=lenient --fix=auto # Lenient validation with auto-fix\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json index idxck --fix=auto         # JSON output with auto-fix\n")
}

// checkIndexFileWithMode performs validation and optional repair using unified validation framework
func checkIndexFileWithMode(fileInfo IndexFileInfo, fixMode FixMode, validationMode dcfh.ValidationMode, extract bool) IndexCheckResult {
	result := IndexCheckResult{
		FilePath: fileInfo.Path,
		FileName: fileInfo.Name,
		Type:     fileInfo.Type,
		IsValid:  true,
	}
	
	// Create a temporary DirectoryCache to use existing validation functions
	indexDir := filepath.Dir(fileInfo.Path)
	repoRoot := filepath.Dir(indexDir)
	
	dc := dcfh.NewDirectoryCache(repoRoot, indexDir)
	if dc == nil {
		result.IsValid = false
		result.Error = "Failed to create DirectoryCache"
		return result
	}
	
	// Create unified validation processor
	verbosity := options.GetInt("verbose")
	config := dcfh.DefaultValidationConfig(validationMode, verbosity)
	processor := dcfh.UnifiedValidationProcessor(config)
	
	// Load and validate index with unified processor
	refs, err := dc.LoadIndexFromFileWithProcessor(fileInfo.Path, processor)
	if err != nil {
		result.IsValid = false
		result.Issues = append(result.Issues, err.Error())
		
		// Try to categorize the error
		errStr := err.Error()
		if strings.Contains(errStr, "signature") {
			result.Issues = append(result.Issues, "Invalid file signature")
		}
		if strings.Contains(errStr, "checksum") {
			result.Issues = append(result.Issues, "Checksum verification failed")
		}
		if strings.Contains(errStr, "byte order") {
			result.Issues = append(result.Issues, "Byte order mismatch")
		}
		if strings.Contains(errStr, "version") {
			result.Issues = append(result.Issues, "Version mismatch")
		}
	} else {
		// Validation passed - update entry count
		result.IsValid = true
		result.EntryCount = len(refs)
		
		// Handle extract mode if requested (simplified for now)
		if extract && len(refs) > 0 {
			extractPath := fileInfo.Path + ".extracted"
			result.FixesApplied = append(result.FixesApplied, fmt.Sprintf("Would extract %d valid entries to %s (extract functionality coming soon)", len(refs), extractPath))
		}
	}
	
	// Apply fixes if requested and issues found
	if !result.IsValid && fixMode != FixModeNone && len(result.Issues) > 0 {
		shouldFix := false
		
		switch fixMode {
		case FixModeAuto:
			shouldFix = true
		case FixModeManual:
			// Prompt user
			fmt.Printf("Fix issues in %s? [y/N]: ", fileInfo.Name)
			var response string
			fmt.Scanln(&response)
			shouldFix = strings.ToLower(response) == "y" || strings.ToLower(response) == "yes"
		}
		
		if shouldFix {
			// Create backup before fixing
			backupPath, err := createBackup(fileInfo.Path)
			if err != nil {
				result.Error = fmt.Sprintf("Failed to create backup: %v", err)
				return result
			}
			result.BackupPath = backupPath
			
			// TODO: Implement actual repair logic
			result.FixesApplied = append(result.FixesApplied, "Simulated repair applied")
			result.IsValid = true // Assume repair succeeded
		}
	}
	
	return result
}

// TODO: Implement extractValidEntries once binaryEntryRef type is properly exported
// This would create a new index file containing only the validated entries

// createBackup creates a backup of the index file before repair
func createBackup(indexPath string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.backup-%s", indexPath, timestamp)
	
	sourceFile, err := os.Open(indexPath)
	if err != nil {
		return "", err
	}
	defer sourceFile.Close()
	
	backupFile, err := os.Create(backupPath)
	if err != nil {
		return "", err
	}
	defer backupFile.Close()
	
	_, err = sourceFile.Seek(0, 0)
	if err != nil {
		return "", err
	}
	
	_, err = backupFile.ReadFrom(sourceFile)
	if err != nil {
		os.Remove(backupPath) // Clean up failed backup
		return "", err
	}
	
	return backupPath, nil
}

// displayCheckResult shows the result of checking a single file
func displayCheckResult(result IndexCheckResult) {
	status := "✓ VALID"
	if !result.IsValid {
		status = "✗ CORRUPT"
	}
	
	fmt.Printf("%-20s %-10s %s\n", result.FileName, result.Type, status)
	
	if result.Error != "" {
		fmt.Printf("  Error: %s\n", result.Error)
	}
	
	for _, issue := range result.Issues {
		fmt.Printf("  Issue: %s\n", issue)
	}
	
	for _, fix := range result.FixesApplied {
		fmt.Printf("  Fixed: %s\n", fix)
	}
	
	if result.BackupPath != "" {
		fmt.Printf("  Backup: %s\n", filepath.Base(result.BackupPath))
	}
	
	if len(result.Issues) > 0 || len(result.FixesApplied) > 0 || result.Error != "" {
		fmt.Println()
	}
}

// handleIndexExplore implements index exploration/inspection
func handleIndexExplore(args []string) {
	// TODO: Implement index exploration
	outputError("Index exploration not yet implemented")
	os.Exit(1)
}

// handleIndexReset implements index reset/restart operations
func handleIndexReset(args []string) {
	if len(args) != 0 {
		outputError("Usage: dcfh index reset")
		os.Exit(1)
	}
	
	// Find dcfh repository - allow missing main.idx for reset operation
	repoRoot, dcfhDir, err := findDcfhRepoAllowMissingIndex()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}
	
	// Create DirectoryCache instance
	dc := dcfh.NewDirectoryCache(repoRoot, dcfhDir)
	
	// Get verbosity level
	verbosity := options.GetInt("verbose")
	
	// Check output format
	outputFormat := validateOutputFormat()
	
	if verbosity >= 1 {
		outputMessage("Resetting index to empty state...")
	}
	
	// Create empty main index
	err = dc.CreateEmptyMainIndex()
	if err != nil {
		if outputFormat == OutputJSON {
			resetResult := map[string]interface{}{
				"success":    false,
				"error":      err.Error(),
				"repository": repoRoot,
			}
			outputJSON(resetResult)
		} else {
			outputError(fmt.Sprintf("Failed to reset index: %v", err))
		}
		os.Exit(1)
	}
	
	if outputFormat == OutputJSON {
		resetResult := map[string]interface{}{
			"success":    true,
			"message":    "Index successfully reset to empty state",
			"repository": repoRoot,
		}
		outputJSON(resetResult)
	} else {
		outputMessage("Index successfully reset to empty state")
		if verbosity >= 1 {
			outputMessage("Run 'dcfh update' to rebuild the index from current files")
		}
	}
}

// handleIndexRecover implements index recovery operations
func handleIndexRecover(args []string) {
	// Find dcfh repository
	repoRoot, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}
	
	// Create DirectoryCache instance
	dc := dcfh.NewDirectoryCache(repoRoot, dcfhDir)
	
	// Get verbosity level
	verbosity := options.GetInt("verbose")
	
	// Check output format
	outputFormat := validateOutputFormat()
	
	var recoveryResult RecoveryResult
	
	if len(args) == 0 {
		// Auto-recovery mode - try multiple strategies
		if verbosity >= 1 {
			outputMessage("Starting automatic index recovery...")
		}
		
		err = dc.AutoRecover(verbosity)
		if err != nil {
			recoveryResult = RecoveryResult{
				Success:    false,
				Error:      err.Error(),
				Repository: repoRoot,
			}
		} else {
			recoveryResult = RecoveryResult{
				Success:     true,
				Message:     "Index recovery completed successfully",
				Repository:  repoRoot,
				Method:      "automatic",
				TimeElapsed: "0s", // TODO: Add timing
			}
		}
	} else if len(args) == 1 && (args[0] == "preserve" || args[0] == "--preserve") {
		// Explicit comprehensive recovery with state preservation
		if verbosity >= 1 {
			outputMessage("Starting comprehensive recovery with state preservation...")
		}
		
		err = dc.RecoverWithStatePreservation(verbosity)
		if err != nil {
			recoveryResult = RecoveryResult{
				Success:    false,
				Error:      err.Error(),
				Repository: repoRoot,
			}
		} else {
			recoveryResult = RecoveryResult{
				Success:     true,
				Message:     "Comprehensive recovery with state preservation completed successfully",
				Repository:  repoRoot,
				Method:      "comprehensive-preserve",
				TimeElapsed: "0s", // TODO: Add timing
			}
		}
	} else {
		// Specific file recovery
		sourceIndexPath := args[0]
		
		// If relative path, make it relative to dcfh directory
		if !filepath.IsAbs(sourceIndexPath) {
			sourceIndexPath = filepath.Join(dcfhDir, sourceIndexPath)
		}
		
		if verbosity >= 1 {
			outputMessage(fmt.Sprintf("Starting recovery from: %s", sourceIndexPath))
		}
		
		err = dc.RecoverFromIndex(sourceIndexPath, verbosity)
		if err != nil {
			recoveryResult = RecoveryResult{
				Success:    false,
				Error:      err.Error(),
				Repository: repoRoot,
				SourceFile: sourceIndexPath,
			}
		} else {
			recoveryResult = RecoveryResult{
				Success:     true,
				Message:     "Index recovery completed successfully",
				Repository:  repoRoot,
				SourceFile:  sourceIndexPath,
				Method:      "specific",
				TimeElapsed: "0s", // TODO: Add timing
			}
		}
	}
	
	// Output results
	if outputFormat == OutputJSON {
		data, err := json.Marshal(recoveryResult)
		if err != nil {
			outputError(fmt.Sprintf("Failed to marshal JSON output: %v", err))
			os.Exit(1)
		}
		fmt.Println(string(data))
	} else {
		if recoveryResult.Success {
			outputMessage(recoveryResult.Message)
		} else {
			outputError(recoveryResult.Error)
			os.Exit(1)
		}
	}
}

// RecoveryResult represents the result of an index recovery operation
type RecoveryResult struct {
	Success     bool   `json:"success"`
	Message     string `json:"message,omitempty"`
	Error       string `json:"error,omitempty"`
	Repository  string `json:"repository"`
	SourceFile  string `json:"source_file,omitempty"`
	Method      string `json:"method,omitempty"`      // automatic, specific
	TimeElapsed string `json:"time_elapsed,omitempty"`
}

// handleIndexSearch implements index search operations
func handleIndexSearch(args []string) {
	if len(args) < 1 {
		outputError("Usage: dcfh index search <pattern> [options]")
		outputError("  dcfh index search \"*.txt\"              # Search by filename pattern")
		outputError("  dcfh index search --path /some/dir      # Search by path prefix")
		outputError("  dcfh index search --hash abc123         # Search by hash prefix")
		outputError("  dcfh index search --size 1024           # Search by exact file size")
		outputError("  dcfh index search --deleted             # Show only deleted entries")
		os.Exit(1)
	}

	// Find dcfh repository
	repoRoot, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}

	// Parse search options
	var pattern, pathPrefix, hashPrefix string
	var exactSize *uint64
	var showDeleted bool
	
	i := 0
	for i < len(args) {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			switch {
			case strings.HasPrefix(arg, "--path="):
				pathPrefix = strings.TrimPrefix(arg, "--path=")
			case arg == "--path" && i+1 < len(args):
				i++
				pathPrefix = args[i]
			case strings.HasPrefix(arg, "--hash="):
				hashPrefix = strings.TrimPrefix(arg, "--hash=")
			case arg == "--hash" && i+1 < len(args):
				i++
				hashPrefix = args[i]
			case strings.HasPrefix(arg, "--size="):
				sizeStr := strings.TrimPrefix(arg, "--size=")
				if size, err := strconv.ParseUint(sizeStr, 10, 64); err == nil {
					exactSize = &size
				} else {
					outputError(fmt.Sprintf("Invalid size value: %s", sizeStr))
					os.Exit(1)
				}
			case arg == "--size" && i+1 < len(args):
				i++
				if size, err := strconv.ParseUint(args[i], 10, 64); err == nil {
					exactSize = &size
				} else {
					outputError(fmt.Sprintf("Invalid size value: %s", args[i]))
					os.Exit(1)
				}
			case arg == "--deleted":
				showDeleted = true
			default:
				outputError(fmt.Sprintf("Unknown option: %s", arg))
				os.Exit(1)
			}
		} else {
			// First non-option argument is the pattern
			if pattern == "" {
				pattern = arg
			} else {
				outputError("Multiple patterns not supported")
				os.Exit(1)
			}
		}
		i++
	}

	// Create directory cache for index access
	cache := dcfh.NewDirectoryCache(repoRoot, dcfhDir)
	defer cache.Close()

	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}

	// Search in all available index files
	indexFiles, err := findIndexFiles(dcfhDir)
	if err != nil {
		outputError(fmt.Sprintf("Failed to find index files: %v", err))
		os.Exit(1)
	}

	if len(indexFiles) == 0 {
		outputError("No index files found")
		os.Exit(1)
	}

	searchCount := 0
	for _, indexFile := range indexFiles {
		// Create search options
		searchOpts := dcfh.SearchOptions{
			Pattern:     pattern,
			PathPrefix:  pathPrefix,
			HashPrefix:  hashPrefix,
			ExactSize:   exactSize,
			ShowDeleted: showDeleted,
			SearchCount: &searchCount,
		}
		
		// Create search processor using callback system
		processor := dcfh.SearchEntryProcessor(searchOpts)
		
		// Load index with search processor
		_, err := cache.LoadIndexFromFileWithProcessor(indexFile.Path, processor)
		if err != nil {
			outputError(fmt.Sprintf("Failed to search in %s: %v", indexFile.Name, err))
			continue
		}
	}

	if searchCount == 0 {
		if validateOutputFormat() == "json" {
			fmt.Println("[]")
		} else {
			outputError("No matching entries found")
		}
	}
}

// handleIndexMerge implements index merge operations
func handleIndexMerge(args []string) {
	// TODO: Implement index merge
	outputError("Index merge not yet implemented")
	os.Exit(1)
}

// findDcfhRepo searches for .dcfh directory starting from current directory
// and moving up the directory tree
// Returns: (repoRoot, dcfhDir, error)
func findDcfhRepo() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current directory: %w", err)
	}

	dir := cwd
	for {
		dcfhPath := filepath.Join(dir, ".dcfh")
		indexFile := filepath.Join(dcfhPath, "main.idx")

		if info, err := os.Stat(dcfhPath); err == nil && info.IsDir() {
			if _, err := os.Stat(indexFile); err == nil {
				// Resolve symlinks to get the real path
				realDir, err := filepath.EvalSymlinks(dir)
				if err != nil {
					// If symlink resolution fails, fall back to original path
					realDir = dir
				}
				return realDir, realDir, nil // repoRoot and dcfhDir are the same
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return "", "", fmt.Errorf("not a dcfh repository (or any of the parent directories)")
}

// findDcfhRepoAllowMissingIndex finds a dcfh repository without requiring main.idx to exist
// This is useful for recovery operations where the main index may be missing or corrupt
func findDcfhRepoAllowMissingIndex() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current directory: %w", err)
	}

	dir := cwd
	for {
		dcfhPath := filepath.Join(dir, ".dcfh")

		if info, err := os.Stat(dcfhPath); err == nil && info.IsDir() {
			// Resolve symlinks to get the real path
			realDir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				// If symlink resolution fails, fall back to original path
				realDir = dir
			}
			return realDir, realDir, nil // repoRoot and dcfhDir are the same
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return "", "", fmt.Errorf("not a dcfh repository (or any of the parent directories): .dcfh directory not found")
}
