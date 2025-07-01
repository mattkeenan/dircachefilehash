//go:generate go run generate_version.go

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"
)

func main() {
	if len(os.Args) < 2 {
		showUsage()
		os.Exit(1)
	}

	// Handle help and version early
	if os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help" {
		showHelp()
		return
	}

	if os.Args[1] == "--version" {
		fmt.Printf("dcfhfind %s\n", getVersionString())
		return
	}

	// Parse command line arguments
	args, err := parseArguments(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "dcfhfind: %v\n", err)
		os.Exit(1)
	}

	// Discover repository if needed
	repo, err := discoverRepository(args.RepoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dcfhfind: %v\n", err)
		os.Exit(1)
	}
	args.RepoPath = repo

	// Resolve starting points to actual index files
	indexFiles, err := resolveStartingPoints(args.StartingPoints, args.RepoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dcfhfind: %v\n", err)
		os.Exit(1)
	}

	// Execute the find operation
	err = executeFind(indexFiles, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dcfhfind: %v\n", err)
		os.Exit(1)
	}
}

func showUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfhfind [starting-points...] [expressions]\n")
	fmt.Fprintf(os.Stderr, "Try 'dcfhfind --help' for more information.\n")
}

func showHelp() {
	fmt.Printf("dcfhfind - find-style interface for dcfh repositories\n\n")
	fmt.Printf("Usage: dcfhfind [starting-points...] [expressions]\n\n")
	
	fmt.Printf("STARTING POINTS:\n")
	fmt.Printf("  main              Search main index (.dcfh/main.idx)\n")
	fmt.Printf("  cache             Search cache index (.dcfh/cache.idx)\n")
	fmt.Printf("  scan              Search all scan indices (.dcfh/scan-*.idx)\n")
	fmt.Printf("  scan-PID-TID      Search specific scan index\n")
	fmt.Printf("  all               Search all indices (main + cache + scan)\n")
	fmt.Printf("  /path/to/file.idx Direct file path\n")
	fmt.Printf("  .dcfh/*.idx       Shell patterns\n\n")
	
	fmt.Printf("TESTS:\n")
	fmt.Printf("  --name PATTERN    Match filename (glob)\n")
	fmt.Printf("  --path PATTERN    Match full path (glob)\n")
	fmt.Printf("  --iname PATTERN   Case-insensitive name match\n")
	fmt.Printf("  --ipath PATTERN   Case-insensitive path match\n")
	fmt.Printf("  --size [+-]N[cwbkMG]  Size comparison\n")
	fmt.Printf("  --empty           Zero size files\n")
	fmt.Printf("  --mtime [+-]N     Modified N*24 hours ago\n")
	fmt.Printf("  --mmin [+-]N      Modified N minutes ago\n")
	fmt.Printf("  --ctime [+-]N     Changed N*24 hours ago\n")
	fmt.Printf("  --cmin [+-]N      Changed N minutes ago\n")
	fmt.Printf("  --hash HASH       Exact hash match\n")
	fmt.Printf("  --hash-prefix PREFIX  Hash starts with prefix\n")
	fmt.Printf("  --hash-type TYPE  Hash algorithm (SHA1, SHA256, SHA512)\n")
	fmt.Printf("  --deleted         Entry marked as deleted\n")
	fmt.Printf("  --valid           Entry passes validation\n")
	fmt.Printf("  --corrupt         Entry fails validation\n")
	fmt.Printf("  --missing         File doesn't exist on disk\n")
	fmt.Printf("  --type TYPE       File type (f,d,l,p,s,c,b)\n")
	fmt.Printf("  --perm MODE       Exact permissions\n")
	fmt.Printf("  --perm -MODE      All bits set\n")
	fmt.Printf("  --perm /MODE      Any bits set\n\n")
	
	fmt.Printf("ACTIONS:\n")
	fmt.Printf("  --print           Print path (default)\n")
	fmt.Printf("  --print0          Print null-terminated paths\n")
	fmt.Printf("  --ls              Detailed listing\n")
	fmt.Printf("  --printf FORMAT   Custom format output\n")
	fmt.Printf("  --validate        Validate entry\n")
	fmt.Printf("  --checksum        Verify hash against file\n")
	fmt.Printf("  --fix {auto|manual|none}  Apply fixes (required argument)\n\n")
	
	fmt.Printf("OPERATORS:\n")
	fmt.Printf("  --and             Logical AND (implicit)\n")
	fmt.Printf("  --or              Logical OR\n")
	fmt.Printf("  --not, !          Logical NOT\n")
	fmt.Printf("  \\( ... \\)          Grouping\n\n")
	
	fmt.Printf("GLOBAL OPTIONS:\n")
	fmt.Printf("  --repo DIR        Repository root directory\n")
	fmt.Printf("  --maxdepth N      Maximum search depth\n")
	fmt.Printf("  --warn            Enable warnings\n")
	fmt.Printf("  --nowarn          Suppress warnings\n\n")
	
	fmt.Printf("PRINTF FORMAT SPECIFIERS:\n")
	fmt.Printf("  %%p - Full path          %%s - Size in bytes\n")
	fmt.Printf("  %%f - Filename only      %%m - Permissions (octal)\n")
	fmt.Printf("  %%h - Directory name     %%u - UID\n")
	fmt.Printf("  %%t - Modification time  %%g - GID\n")
	fmt.Printf("  %%c - Change time        %%H - Hash value\n")
	fmt.Printf("  %%i - Index source       %%Y - Hash type\n")
	fmt.Printf("  %%d - Device number      %%%% - Literal %%\n\n")
	
	fmt.Printf("EXAMPLES:\n")
	fmt.Printf("  dcfhfind main --name \"*.go\"                    # Find Go files\n")
	fmt.Printf("  dcfhfind all --size +100M --ls                # Large files\n")
	fmt.Printf("  dcfhfind scan --corrupt --print               # Corrupted entries\n")
	fmt.Printf("  dcfhfind cache --deleted --printf \"%%p\\n\"       # Deleted files\n")
	fmt.Printf("  dcfhfind all --fix none --validate            # Validate all\n\n")
}

// Arguments represents parsed command line arguments
type Arguments struct {
	StartingPoints []string
	Expressions    []Expression
	Actions        []Action
	GlobalOptions  GlobalOptions
	RepoPath       string
}

// GlobalOptions represents global dcfhfind options
type GlobalOptions struct {
	MaxDepth int
	MinDepth int
	Warn     bool
	RepoDir  string
}

// Expression represents a test or operator in the find expression
type Expression interface {
	Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error)
	String() string
}

// Action represents an action to perform on matching entries
type Action interface {
	Execute(entry *dircachefilehash.EntryInfo, context *EvalContext) error
	String() string
}

// EvalContext provides context for expression evaluation
type EvalContext struct {
	IndexPath    string
	IndexType    string
	Repository   string
	Options      GlobalOptions
	EntryPath    string
	RelativePath string
}

// IndexFile represents a resolved index file to search
type IndexFile struct {
	Path      string
	Type      string // "main", "cache", "scan", "file"
	ScanID    string // for scan files: "PID-TID"
}

func parseArguments(args []string) (*Arguments, error) {
	result := &Arguments{
		StartingPoints: []string{},
		Expressions:    []Expression{},
		Actions:        []Action{},
		GlobalOptions:  GlobalOptions{Warn: true},
	}

	i := 0
	
	// Parse starting points (everything before first -- option)
	for i < len(args) && !strings.HasPrefix(args[i], "--") && args[i] != "!" && args[i] != "(" {
		result.StartingPoints = append(result.StartingPoints, args[i])
		i++
	}

	// If no starting points specified, default to "all"
	if len(result.StartingPoints) == 0 {
		result.StartingPoints = []string{"all"}
	}

	// Parse expressions and actions
	for i < len(args) {
		arg := args[i]
		
		switch arg {
		case "--repo":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--repo requires an argument")
			}
			result.GlobalOptions.RepoDir = args[i+1]
			result.RepoPath = args[i+1]
			i += 2
		case "--maxdepth":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--maxdepth requires an argument")
			}
			// TODO: Parse integer
			i += 2
		case "--warn":
			result.GlobalOptions.Warn = true
			i++
		case "--nowarn":
			result.GlobalOptions.Warn = false
			i++
		case "--fix":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--fix requires an argument (auto|manual|none)")
			}
			fixMode := args[i+1]
			if fixMode != "auto" && fixMode != "manual" && fixMode != "none" {
				return nil, fmt.Errorf("--fix argument must be auto, manual, or none")
			}
			action := &FixAction{Mode: fixMode}
			result.Actions = append(result.Actions, action)
			i += 2
		default:
			// Try to parse as expression or action
			expr, consumed, err := parseExpression(args[i:])
			if err != nil {
				return nil, fmt.Errorf("unknown option or invalid expression: %s", arg)
			}
			
			if action, ok := expr.(Action); ok {
				result.Actions = append(result.Actions, action)
			} else if expression, ok := expr.(Expression); ok {
				result.Expressions = append(result.Expressions, expression)
			} else {
				return nil, fmt.Errorf("unknown expression type returned for: %s", arg)
			}
			i += consumed
		}
	}

	// If no actions specified, default to --print
	if len(result.Actions) == 0 {
		result.Actions = append(result.Actions, &PrintAction{})
	}

	return result, nil
}

func parseExpression(args []string) (interface{}, int, error) {
	if len(args) == 0 {
		return nil, 0, fmt.Errorf("empty expression")
	}

	arg := args[0]
	
	switch arg {
	case "--name":
		if len(args) < 2 {
			return nil, 0, fmt.Errorf("--name requires a pattern")
		}
		return &NameTest{Pattern: args[1], CaseSensitive: true}, 2, nil
	case "--iname":
		if len(args) < 2 {
			return nil, 0, fmt.Errorf("--iname requires a pattern")
		}
		return &NameTest{Pattern: args[1], CaseSensitive: false}, 2, nil
	case "--path":
		if len(args) < 2 {
			return nil, 0, fmt.Errorf("--path requires a pattern")
		}
		return &PathTest{Pattern: args[1], CaseSensitive: true}, 2, nil
	case "--ipath":
		if len(args) < 2 {
			return nil, 0, fmt.Errorf("--ipath requires a pattern")
		}
		return &PathTest{Pattern: args[1], CaseSensitive: false}, 2, nil
	case "--size":
		if len(args) < 2 {
			return nil, 0, fmt.Errorf("--size requires a size specification")
		}
		return parseSizeTest(args[1])
	case "--empty":
		return &EmptyTest{}, 1, nil
	case "--deleted":
		return &DeletedTest{}, 1, nil
	case "--valid":
		return &ValidTest{}, 1, nil
	case "--corrupt":
		return &CorruptTest{}, 1, nil
	case "--hash":
		if len(args) < 2 {
			return nil, 0, fmt.Errorf("--hash requires a hash value")
		}
		return &HashTest{Hash: args[1]}, 2, nil
	case "--hash-prefix":
		if len(args) < 2 {
			return nil, 0, fmt.Errorf("--hash-prefix requires a prefix")
		}
		return &HashPrefixTest{Prefix: args[1]}, 2, nil
	case "--hash-type":
		if len(args) < 2 {
			return nil, 0, fmt.Errorf("--hash-type requires a type")
		}
		return &HashTypeTest{Type: args[1]}, 2, nil
	case "--print":
		return &PrintAction{}, 1, nil
	case "--print0":
		return &Print0Action{}, 1, nil
	case "--ls":
		return &LsAction{}, 1, nil
	case "--printf":
		if len(args) < 2 {
			return nil, 0, fmt.Errorf("--printf requires a format string")
		}
		return &PrintfAction{Format: args[1]}, 2, nil
	case "--validate":
		return &ValidateAction{}, 1, nil
	case "--checksum":
		return &ChecksumAction{}, 1, nil
	default:
		return nil, 0, fmt.Errorf("unknown expression: %s", arg)
	}
}

func parseSizeTest(sizeSpec string) (Expression, int, error) {
	// TODO: Implement size parsing (+100M, -1k, etc)
	return &SizeTest{Size: 0, Mode: "="}, 2, nil
}

func discoverRepository(repoPath string) (string, error) {
	if repoPath == "" {
		repoPath = "."
	}
	return dircachefilehash.FindRepositoryRootFrom(repoPath)
}

func resolveStartingPoints(startingPoints []string, repoPath string) ([]IndexFile, error) {
	var indexFiles []IndexFile
	dcfhDir := filepath.Join(repoPath, ".dcfh")

	for _, point := range startingPoints {
		switch point {
		case "main":
			indexFiles = append(indexFiles, IndexFile{
				Path: filepath.Join(dcfhDir, "main.idx"),
				Type: "main",
			})
		case "cache":
			indexFiles = append(indexFiles, IndexFile{
				Path: filepath.Join(dcfhDir, "cache.idx"),
				Type: "cache",
			})
		case "scan":
			// Find all scan files
			scanFiles, err := filepath.Glob(filepath.Join(dcfhDir, "scan-*.idx"))
			if err != nil {
				return nil, fmt.Errorf("error finding scan files: %w", err)
			}
			for _, scanFile := range scanFiles {
				basename := filepath.Base(scanFile)
				// Extract scan ID from filename: scan-PID-TID.idx
				if strings.HasPrefix(basename, "scan-") && strings.HasSuffix(basename, ".idx") {
					scanID := basename[5 : len(basename)-4] // Remove "scan-" and ".idx"
					indexFiles = append(indexFiles, IndexFile{
						Path:   scanFile,
						Type:   "scan",
						ScanID: scanID,
					})
				}
			}
		case "all":
			// Recursively resolve main, cache, and scan
			allPoints := []string{"main", "cache", "scan"}
			for _, subPoint := range allPoints {
				subFiles, err := resolveStartingPoints([]string{subPoint}, repoPath)
				if err != nil {
					continue // Ignore missing indices
				}
				indexFiles = append(indexFiles, subFiles...)
			}
		default:
			// Check if it's a specific scan file pattern
			if strings.HasPrefix(point, "scan-") && (strings.Contains(point, "-") || strings.HasSuffix(point, ".idx")) {
				var indexPath string
				if strings.HasSuffix(point, ".idx") {
					indexPath = filepath.Join(dcfhDir, point)
				} else {
					indexPath = filepath.Join(dcfhDir, point+".idx")
				}
				
				// Extract scan ID
				basename := filepath.Base(indexPath)
				if strings.HasPrefix(basename, "scan-") && strings.HasSuffix(basename, ".idx") {
					scanID := basename[5 : len(basename)-4]
					indexFiles = append(indexFiles, IndexFile{
						Path:   indexPath,
						Type:   "scan",
						ScanID: scanID,
					})
				}
			} else {
				// Treat as direct file path
				indexFiles = append(indexFiles, IndexFile{
					Path: point,
					Type: "file",
				})
			}
		}
	}

	// Remove duplicates and check file existence
	var result []IndexFile
	seen := make(map[string]bool)
	
	for _, indexFile := range indexFiles {
		if seen[indexFile.Path] {
			continue
		}
		seen[indexFile.Path] = true

		if _, err := os.Stat(indexFile.Path); os.IsNotExist(err) {
			// Silently skip missing files (like find does)
			continue
		}

		result = append(result, indexFile)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no accessible index files found")
	}

	return result, nil
}

func executeFind(indexFiles []IndexFile, args *Arguments) error {
	for _, indexFile := range indexFiles {
		err := processIndexFile(indexFile, args)
		if err != nil {
			if args.GlobalOptions.Warn {
				fmt.Fprintf(os.Stderr, "dcfhfind: warning: %s: %v\n", indexFile.Path, err)
			}
			continue
		}
	}
	return nil
}

func processIndexFile(indexFile IndexFile, args *Arguments) error {
	// Use the new IterateIndexFile function
	return dircachefilehash.IterateIndexFile(indexFile.Path, func(entry *dircachefilehash.EntryInfo, indexType string) bool {
		context := &EvalContext{
			IndexPath:    indexFile.Path,
			IndexType:    indexType,
			Repository:   args.RepoPath,
			Options:      args.GlobalOptions,
			EntryPath:    entry.Path,
			RelativePath: entry.Path,
		}

		// Evaluate all expressions (implicit AND)
		match := true
		for _, expr := range args.Expressions {
			result, err := expr.Evaluate(entry, context)
			if err != nil {
				if args.GlobalOptions.Warn {
					fmt.Fprintf(os.Stderr, "dcfhfind: warning: %s: %v\n", entry.Path, err)
				}
				match = false
				break
			}
			if !result {
				match = false
				break
			}
		}

		// Execute actions on matching entries
		if match {
			for _, action := range args.Actions {
				err := action.Execute(entry, context)
				if err != nil {
					if args.GlobalOptions.Warn {
						fmt.Fprintf(os.Stderr, "dcfhfind: warning: action failed for %s: %v\n", entry.Path, err)
					}
				}
			}
		}
		
		return true // Continue iteration
	})
}