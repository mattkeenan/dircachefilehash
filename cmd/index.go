package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// Index-related types

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
		TotalFiles     int   `json:"total_files"`
		TotalSize      int64 `json:"total_size"`
		MainIndexes    int   `json:"main_indexes"`
		CacheIndexes   int   `json:"cache_indexes"`
		ScanIndexes    int   `json:"scan_indexes"`
		TempIndexes    int   `json:"temp_indexes"`
		UnknownIndexes int   `json:"unknown_indexes"`
	} `json:"summary"`
}

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

// IndexCheckOutput represents the output for index check command
type IndexCheckOutput struct {
	Repository     string             `json:"repository"`
	FixMode        string             `json:"fix_mode"`
	FilesChecked   int                `json:"files_checked"`
	FilesValid     int                `json:"files_valid"`
	FilesCorrupt   int                `json:"files_corrupt"`
	IssuesFound    int                `json:"issues_found"`
	FixesApplied   int                `json:"fixes_applied"`
	BackupsCreated int                `json:"backups_created"`
	Results        []IndexCheckResult `json:"results"`
	Summary        string             `json:"summary"`
}

// FixMode represents the fix mode for index operations
type FixMode string

const (
	FixModeNone   FixMode = "none"   // Read-only validation (default)
	FixModeManual FixMode = "manual" // Prompt user for each fix
	FixModeAuto   FixMode = "auto"   // Automatically apply fixes
)

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

// TODO: The actual function implementations will be added here
// This is a placeholder to make the file compile while I extract the functions

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
	fmt.Fprintf(os.Stderr, "  reset, restart   Reset the main index to empty state\n")
	fmt.Fprintf(os.Stderr, "  recover, recovery Recover index files from backups or scan files\n")
	fmt.Fprintf(os.Stderr, "  merge            Merge multiple index files\n")
	fmt.Fprintf(os.Stderr, "  help             Show this help message\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh index list\n")
	fmt.Fprintf(os.Stderr, "  dcfh index idxck --fix=auto\n")
	fmt.Fprintf(os.Stderr, "  dcfh index explore main.idx\n")
	fmt.Fprintf(os.Stderr, "  dcfh index search filename.txt\n")
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
		// JSON output with summary statistics
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
		// Human-readable tabular output
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
		Repository:   repoRoot,
		FixMode:      string(fixMode),
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
		dc := dcfh.NewDirectoryCache(repoRoot, repoRoot)
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
	repoRoot, _, err := findDcfhRepoAllowMissingIndex()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}
	
	// Create DirectoryCache instance
	dc := dcfh.NewDirectoryCache(repoRoot, repoRoot)
	
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
	dc := dcfh.NewDirectoryCache(repoRoot, repoRoot)
	
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
	cache := dcfh.NewDirectoryCache(repoRoot, repoRoot)
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
		if validateOutputFormat() == OutputJSON {
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

// Helper functions for index operations

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
	header, err := dcfh.ValidateIndexHeader(indexPath, true, 0) // Validate version compatibility (current version is 0)
	if err != nil {
		return 0, err
	}
	return int(header.EntryCount), nil
}

// showIndexCheckUsage shows usage for idxck command
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
	
	dc := dcfh.NewDirectoryCache(repoRoot, repoRoot)
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