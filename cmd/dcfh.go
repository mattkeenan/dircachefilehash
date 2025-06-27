//go:generate go run generate_version.go

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// Global flags
var (
	output    = flag.String("output", "human", "output format: human, json, fdupes")
	jsonFlag  = flag.Bool("json", false, "output in JSON format (alias for --output=json)")
	verbose   = flag.Int("verbose", 0, "verbose level (1=basic, 2=detailed, 3=trace); can be repeated -v, -vv, -vvv")
	version   = flag.Bool("version", false, "show version information")
	debug     = flag.String("debug", "", "debug options (comma-separated): extravalidation,memorylayout,indexchaining,scanning")
	filehash  = flag.String("filehash", "", "hash algorithm overrides (format: default:sha256)")
	symlinks  = flag.String("symlinks", "all", "symlink handling: all (follow all), contained (follow only if target within rootDir), none (don't follow)")
	symlinkShort = flag.Bool("s", false, "follow symlinked directories (alias for --symlinks=all)")
)

// Command-specific flag sets
var (
	initFlags   = flag.NewFlagSet("init", flag.ExitOnError)
	statusFlags = flag.NewFlagSet("status", flag.ExitOnError)
	updateFlags = flag.NewFlagSet("update", flag.ExitOnError)
	dupesFlags  = flag.NewFlagSet("dupes", flag.ExitOnError)
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
	fmt.Fprintf(os.Stderr, "Usage: dcfh [GLOBAL_OPTIONS] <command> [COMMAND_OPTIONS]\n\n")
	fmt.Fprintf(os.Stderr, "Global Options:\n")
	fmt.Fprintf(os.Stderr, "  --output=FORMAT    Output format: human (default), json, fdupes\n")
	fmt.Fprintf(os.Stderr, "  --json             Output in JSON format (alias for --output=json)\n")
	fmt.Fprintf(os.Stderr, "  -v, --verbose=LEVEL    Verbose level (1=basic, 2=detailed, 3=trace); can be repeated -v, -vv, -vvv\n")
	fmt.Fprintf(os.Stderr, "  --debug=OPTIONS        Debug options: extravalidation,memorylayout,indexchaining,scanning\n")
	fmt.Fprintf(os.Stderr, "  --filehash=OPTION      Hash algorithm override (format: default:sha256)\n")
	fmt.Fprintf(os.Stderr, "  -s, --symlinks=MODE    Symlink handling: all (default), contained, none\n")
	fmt.Fprintf(os.Stderr, "  --version              Show version information\n")
	fmt.Fprintf(os.Stderr, "  --help                 Show this help message\n")
	fmt.Fprintf(os.Stderr, "\nCommands:\n")
	fmt.Fprintf(os.Stderr, "  init <dir>       Initialize a new dcfh repository in the specified directory\n")
	fmt.Fprintf(os.Stderr, "  status           Show the status of files in the current dcfh repository\n")
	fmt.Fprintf(os.Stderr, "  update [paths...] Update the index with current file states\n")
	fmt.Fprintf(os.Stderr, "  dupes            Find and display duplicate files\n")
	fmt.Fprintf(os.Stderr, "  config           Get and set repository configuration options\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh init .\n")
	fmt.Fprintf(os.Stderr, "  dcfh init /home/user/documents\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json status\n")
	fmt.Fprintf(os.Stderr, "  dcfh --output=json status\n")
	fmt.Fprintf(os.Stderr, "  dcfh update\n")
	fmt.Fprintf(os.Stderr, "  dcfh update file.txt dir/\n")
	fmt.Fprintf(os.Stderr, "  dcfh --filehash=default:sha1 update\n")
	fmt.Fprintf(os.Stderr, "  dcfh --json dupes\n")
	fmt.Fprintf(os.Stderr, "  dcfh --output=fdupes dupes\n")
	fmt.Fprintf(os.Stderr, "  dcfh config filehash.default sha1\n")
	fmt.Fprintf(os.Stderr, "  dcfh config output.format fdupes\n")
	fmt.Fprintf(os.Stderr, "  dcfh config --list\n")
}

// parseVerboseFlags handles GNU-style verbose flags (-v, -vv, -vvv)
func parseVerboseFlags() {
	// Count -v flags and convert to verbose level
	verboseCount := 0
	
	// Process args to count -v occurrences and remove them
	var newArgs []string
	for i := 0; i < len(os.Args); i++ {
		arg := os.Args[i]
		
		if arg == "-v" {
			verboseCount++
		} else if strings.HasPrefix(arg, "-v") && len(arg) > 2 {
			// Handle -vv, -vvv, etc.
			for j := 1; j < len(arg); j++ {
				if arg[j] == 'v' {
					verboseCount++
				} else {
					// Not a pure -v repetition, keep as is
					newArgs = append(newArgs, arg)
					break
				}
			}
			// If we counted all v's, don't add to newArgs
			if verboseCount > 0 && strings.Trim(arg[1:], "v") == "" {
				continue
			}
		} else {
			newArgs = append(newArgs, arg)
		}
	}
	
	// Update os.Args to remove processed -v flags
	os.Args = newArgs
	
	// Set the verbose level if any -v flags were found
	if verboseCount > 0 {
		*verbose = verboseCount
	}
}

func main() {
	// Set up global flags
	flag.Usage = showUsage

	// Parse global flags first, but handle -v repetition manually
	parseVerboseFlags()
	flag.Parse()

	// Initialize debug flags early
	dcfh.InitDebugFlags(*debug)
	if *debug != "" {
		dcfh.LogDebugFlags()
	}

	// Validate output format early (after flag parsing)
	validateOutputFormat()

	// Handle version flag
	if *version {
		handleVersion()
		return
	}

	// Get remaining arguments after global flags
	args := flag.Args()
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
	case "config":
		handleConfig(args[1:])
	default:
		outputError(fmt.Sprintf("Unknown command: %s", command))
		os.Exit(1)
	}
}

// buildFlags creates a flags map from global CLI flags
func buildFlags() map[string]string {
	flags := make(map[string]string)
	
	// Use verbose level directly
	if *verbose > 0 {
		flags["v"] = fmt.Sprintf("%d", *verbose)
		// Set global verbose level for trace logging
		dcfh.SetVerboseLevel(*verbose)
	}
	
	// Set debug flags  
	if *debug != "" {
		dcfh.SetDebugFlags(*debug)
	}
	
	// Set hash algorithm override
	if *filehash != "" {
		flags["filehash"] = *filehash
	}
	
	// Set symlink handling mode
	symlinkMode := *symlinks
	if *symlinkShort {
		// -s flag overrides --symlinks setting
		symlinkMode = "all"
	}
	flags["symlinks"] = symlinkMode
	
	return flags
}

func handleVersion() {
	if *output == "json" || *jsonFlag {
		versionJSON := map[string]interface{}{
			"version":    getVersionString(),
			"git_commit": getGitCommit(),
			"go_version": runtime.Version(),
			"supported_index_formats": []string{"v0"},
			"description": "Directory Cache File Hash - A fast file indexing and duplicate detection tool",
		}
		jsonBytes, _ := json.MarshalIndent(versionJSON, "", "  ")
		fmt.Println(string(jsonBytes))
	} else {
		fmt.Printf("dcfh version %s\n", getVersionString())
		fmt.Printf("Git commit: %s\n", getGitCommit())
		fmt.Printf("Go version: %s\n", runtime.Version())
		fmt.Printf("Supported index formats: v0\n")
		fmt.Println("Directory Cache File Hash - A fast file indexing and duplicate detection tool")
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
	if *jsonFlag {
		if *output != "human" {
			fmt.Fprintf(os.Stderr, "Error: cannot use both --json and --output flags together\n")
			os.Exit(1)
		}
		return OutputJSON
	}
	
	switch *output {
	case "human":
		return OutputHuman
	case "json":
		return OutputJSON
	case "fdupes":
		return OutputFdupes
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid output format '%s'. Supported formats: human, json, fdupes\n", *output)
		os.Exit(1)
		return OutputHuman // unreachable
	}
}

// getEffectiveOutputFormat determines the output format based on config and CLI flags
func getEffectiveOutputFormat(cache *dcfh.DirectoryCache) OutputFormat {
	// CLI flags take precedence
	if *jsonFlag {
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

	// Create cache - this will automatically create .dcfh directory and index
	cache := dcfh.NewDirectoryCache(absDir, absDir)
	defer cache.Close()
	
	// Apply configuration overrides
	flags := buildFlags()
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		outputError(fmt.Sprintf("Failed to apply configuration overrides: %v", err))
		os.Exit(1)
	}

	format := validateOutputFormat()
	if format == OutputHuman && *verbose > 0 {
		fmt.Printf("Initialized empty dcfh repository in %s\n", dcfhDir)
		fmt.Println("Scanning directory and creating initial index...")
	}

	if err := cache.Update(flags); err != nil {
		outputError(fmt.Sprintf("Failed to create initial index: %v", err))
		os.Exit(1)
	}

	duration := time.Since(start)
	fileCount, totalSize, _ := cache.Stats()

	if format == OutputJSON {
		output := InitOutput{
			Success:     true,
			Message:     "Successfully initialized dcfh repository",
			Repository:  absDir,
			FileCount:   fileCount,
			TotalSize:   totalSize,
			TimeElapsed: duration.Round(time.Millisecond).String(),
		}
		outputJSON(output)
	} else {
		fmt.Printf("Initialized empty dcfh repository in %s\n", dcfhDir)
		if *verbose > 0 {
			fmt.Printf("✓ Successfully indexed %d files, total size: %d bytes\n", fileCount, totalSize)
			fmt.Printf("✓ Completed in %v\n", duration.Round(time.Millisecond))
		}
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
			if *verbose > 0 {
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

	if format == OutputHuman && *verbose > 0 {
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
		if *verbose > 0 {
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
	// Parse flags for config command
	configFlags := flag.NewFlagSet("config", flag.ExitOnError)
	listFlag := configFlags.Bool("list", false, "list all configuration variables")
	_ = configFlags.Bool("global", false, "use global configuration (not implemented)")
	configFlags.Parse(args)
	
	configArgs := configFlags.Args()
	
	// Handle --list flag
	if *listFlag {
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
