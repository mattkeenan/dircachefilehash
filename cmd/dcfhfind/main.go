//go:generate go run generate_version.go

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	fmt.Printf("  --checksum        Verify hash against file (WARNING: slow on many/large files)\n")
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
	fmt.Printf("  %%d - Device number      %%%% - Literal %%\n")
	fmt.Printf("  Escape sequences: \\n (newline), \\t (tab), \\r (carriage return)\n\n")
	
	fmt.Printf("PERFORMANCE NOTES:\n")
	fmt.Printf("  The --checksum action reads file contents to compute hashes, which can be\n")
	fmt.Printf("  very slow when processing many files or large files. Consider using --valid\n")
	fmt.Printf("  for faster validation that doesn't require reading file contents.\n\n")
	
	fmt.Printf("EXAMPLES:\n")
	fmt.Printf("  dcfhfind main --name \"*.go\"                    # Find Go files\n")
	fmt.Printf("  dcfhfind all --size +100M --ls                # Large files\n")
	fmt.Printf("  dcfhfind scan --corrupt --print               # Corrupted entries\n")
	fmt.Printf("  dcfhfind cache --deleted --printf \"%%p\\n\"       # Deleted files\n")
	fmt.Printf("  dcfhfind all --valid --print                  # Fast validation check\n")
	fmt.Printf("  dcfhfind main --name \"*.txt\" --checksum       # Slow but thorough hash check\n\n")
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

	// Parse expressions and actions using complex expression parser
	remainingArgs := args[i:]
	expressions, actions, globalArgs, err := parseComplexExpressions(remainingArgs)
	if err != nil {
		return nil, err
	}
	
	// Apply global arguments
	for option, value := range globalArgs {
		switch option {
		case "--repo":
			result.GlobalOptions.RepoDir = value
			result.RepoPath = value
		case "--maxdepth":
			// TODO: Parse integer
		case "--warn":
			result.GlobalOptions.Warn = true
		case "--nowarn":
			result.GlobalOptions.Warn = false
		}
	}
	
	result.Expressions = expressions
	result.Actions = actions

	// If no actions specified, default to --print
	if len(result.Actions) == 0 {
		result.Actions = append(result.Actions, &PrintAction{})
	}

	return result, nil
}

// parseComplexExpressions parses expressions with operator support (--and, --or, --not, grouping)
func parseComplexExpressions(args []string) ([]Expression, []Action, map[string]string, error) {
	parser := &ExpressionParser{
		tokens:     args,
		pos:        0,
		globalArgs: make(map[string]string),
		actions:    []Action{},
	}
	
	expressions, err := parser.parseExpressionList()
	if err != nil {
		return nil, nil, nil, err
	}
	
	// If we have multiple expressions, combine them with implicit AND
	var finalExpression Expression
	if len(expressions) == 0 {
		finalExpression = nil
	} else if len(expressions) == 1 {
		finalExpression = expressions[0]
	} else {
		// Combine multiple expressions with AND
		finalExpression = expressions[0]
		for i := 1; i < len(expressions); i++ {
			finalExpression = &AndExpression{
				Left:  finalExpression,
				Right: expressions[i],
			}
		}
	}
	
	var result []Expression
	if finalExpression != nil {
		result = []Expression{finalExpression}
	}
	
	return result, parser.actions, parser.globalArgs, nil
}

// ExpressionParser handles complex expression parsing with operators
type ExpressionParser struct {
	tokens     []string
	pos        int
	globalArgs map[string]string
	actions    []Action
}

func (p *ExpressionParser) peek() string {
	if p.pos >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.pos]
}

func (p *ExpressionParser) next() string {
	if p.pos >= len(p.tokens) {
		return ""
	}
	token := p.tokens[p.pos]
	p.pos++
	return token
}

func (p *ExpressionParser) parseExpressionList() ([]Expression, error) {
	var expressions []Expression
	
	for p.pos < len(p.tokens) {
		expr, err := p.parseOrExpression()
		if err != nil {
			return nil, err
		}
		if expr != nil {
			expressions = append(expressions, expr)
		}
	}
	
	return expressions, nil
}

func (p *ExpressionParser) parseOrExpression() (Expression, error) {
	left, err := p.parseAndExpression()
	if err != nil {
		return nil, err
	}
	
	for p.peek() == "--or" {
		p.next() // consume --or
		right, err := p.parseAndExpression()
		if err != nil {
			return nil, err
		}
		left = &OrExpression{Left: left, Right: right}
	}
	
	return left, nil
}

func (p *ExpressionParser) parseAndExpression() (Expression, error) {
	left, err := p.parseNotExpression()
	if err != nil {
		return nil, err
	}
	
	for p.peek() == "--and" || (p.peek() != "" && p.peek() != "--or" && p.peek() != ")" && p.isTestExpression(p.peek())) {
		if p.peek() == "--and" {
			p.next() // consume --and
		}
		// implicit AND for adjacent expressions
		right, err := p.parseNotExpression()
		if err != nil {
			return nil, err
		}
		if right != nil {
			left = &AndExpression{Left: left, Right: right}
		}
	}
	
	return left, nil
}

func (p *ExpressionParser) parseNotExpression() (Expression, error) {
	if p.peek() == "--not" || p.peek() == "!" {
		p.next() // consume --not or !
		expr, err := p.parsePrimaryExpression()
		if err != nil {
			return nil, err
		}
		return &NotExpression{Expr: expr}, nil
	}
	
	return p.parsePrimaryExpression()
}

func (p *ExpressionParser) parsePrimaryExpression() (Expression, error) {
	token := p.peek()
	if token == "" {
		return nil, nil
	}
	
	if token == "(" {
		p.next() // consume (
		expr, err := p.parseOrExpression()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, fmt.Errorf("expected ')' but found '%s'", p.peek())
		}
		p.next() // consume )
		return expr, nil
	}
	
	// Handle global options
	if p.isGlobalOption(token) {
		return p.parseGlobalOption()
	}
	
	// Parse basic expression or action
	return p.parseBasicExpression()
}

func (p *ExpressionParser) isTestExpression(token string) bool {
	tests := []string{
		"--name", "--iname", "--path", "--ipath", "--size", "--empty", "--deleted",
		"--valid", "--corrupt", "--hash", "--hash-prefix", "--hash-type", 
		"--mtime", "--mmin", "--ctime", "--cmin", "--not", "!", "(",
	}
	for _, test := range tests {
		if token == test {
			return true
		}
	}
	return false
}

func (p *ExpressionParser) isGlobalOption(token string) bool {
	globals := []string{"--repo", "--maxdepth", "--warn", "--nowarn"}
	for _, global := range globals {
		if token == global {
			return true
		}
	}
	return false
}

func (p *ExpressionParser) parseGlobalOption() (Expression, error) {
	token := p.next()
	
	switch token {
	case "--repo":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--repo requires an argument")
		}
		value := p.next()
		p.globalArgs["--repo"] = value
	case "--maxdepth":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--maxdepth requires an argument")
		}
		value := p.next()
		p.globalArgs["--maxdepth"] = value
	case "--warn":
		p.globalArgs["--warn"] = "true"
	case "--nowarn":
		p.globalArgs["--nowarn"] = "true"
	}
	
	return nil, nil // Global options don't produce expressions
}

func (p *ExpressionParser) parseBasicExpression() (Expression, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	
	token := p.next()
	
	switch token {
	case "--name":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--name requires a pattern")
		}
		pattern := p.next()
		return &NameTest{Pattern: pattern, CaseSensitive: true}, nil
		
	case "--iname":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--iname requires a pattern")
		}
		pattern := p.next()
		return &NameTest{Pattern: pattern, CaseSensitive: false}, nil
		
	case "--path":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--path requires a pattern")
		}
		pattern := p.next()
		return &PathTest{Pattern: pattern, CaseSensitive: true}, nil
		
	case "--ipath":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--ipath requires a pattern")
		}
		pattern := p.next()
		return &PathTest{Pattern: pattern, CaseSensitive: false}, nil
		
	case "--size":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--size requires a size specification")
		}
		sizeSpec := p.next()
		expr, _, err := parseSizeTest(sizeSpec)
		return expr, err
		
	case "--empty":
		return &EmptyTest{}, nil
	case "--deleted":
		return &DeletedTest{}, nil
	case "--valid":
		return &ValidTest{}, nil
	case "--corrupt":
		return &CorruptTest{}, nil
		
	case "--hash":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--hash requires a hash value")
		}
		hash := p.next()
		return &HashTest{Hash: hash}, nil
		
	case "--mtime":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--mtime requires a time specification")
		}
		timeSpec := p.next()
		return parseTimeTest(timeSpec, "mtime")
		
	case "--mmin":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--mmin requires a time specification")
		}
		timeSpec := p.next()
		return parseTimeTest(timeSpec, "mmin")
		
	case "--ctime":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--ctime requires a time specification")
		}
		timeSpec := p.next()
		return parseTimeTest(timeSpec, "ctime")
		
	case "--cmin":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--cmin requires a time specification")
		}
		timeSpec := p.next()
		return parseTimeTest(timeSpec, "cmin")
		
	case "--hash-prefix":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--hash-prefix requires a prefix")
		}
		prefix := p.next()
		return &HashPrefixTest{Prefix: prefix}, nil
		
	case "--hash-type":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--hash-type requires a type")
		}
		hashType := p.next()
		return &HashTypeTest{Type: hashType}, nil
		
	// Actions
	case "--print":
		action := &PrintAction{}
		p.actions = append(p.actions, action)
		return nil, nil // Actions don't produce expressions
		
	case "--print0":
		action := &Print0Action{}
		p.actions = append(p.actions, action)
		return nil, nil
		
	case "--ls":
		action := &LsAction{}
		p.actions = append(p.actions, action)
		return nil, nil
		
	case "--printf":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--printf requires a format string")
		}
		format := p.next()
		action := &PrintfAction{Format: format}
		p.actions = append(p.actions, action)
		return nil, nil
		
	case "--validate":
		action := &ValidateAction{}
		p.actions = append(p.actions, action)
		return nil, nil
		
	case "--checksum":
		action := &ChecksumAction{}
		p.actions = append(p.actions, action)
		return nil, nil
		
	case "--fix":
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--fix requires an argument (auto|manual|none)")
		}
		fixMode := p.next()
		if fixMode != "auto" && fixMode != "manual" && fixMode != "none" {
			return nil, fmt.Errorf("--fix argument must be auto, manual, or none")
		}
		action := &FixAction{Mode: fixMode}
		p.actions = append(p.actions, action)
		return nil, nil
		
	default:
		return nil, fmt.Errorf("unknown expression: %s", token)
	}
}

func parseSizeTest(sizeSpec string) (Expression, int, error) {
	if len(sizeSpec) == 0 {
		return nil, 0, fmt.Errorf("empty size specification")
	}
	
	var mode string
	var sizeStr string
	
	// Parse prefix (+, -, or exact)
	switch sizeSpec[0] {
	case '+':
		mode = "+"
		sizeStr = sizeSpec[1:]
	case '-':
		mode = "-" 
		sizeStr = sizeSpec[1:]
	default:
		mode = "="
		sizeStr = sizeSpec
	}
	
	if len(sizeStr) == 0 {
		return nil, 0, fmt.Errorf("size specification missing numeric value")
	}
	
	// Parse unit suffix
	var multiplier int64 = 1
	var numStr string
	
	if len(sizeStr) > 0 {
		lastChar := sizeStr[len(sizeStr)-1]
		switch lastChar {
		case 'c':
			// bytes (default)
			multiplier = 1
			numStr = sizeStr[:len(sizeStr)-1]
		case 'w':
			// 2-byte words
			multiplier = 2
			numStr = sizeStr[:len(sizeStr)-1]
		case 'b':
			// 512-byte blocks
			multiplier = 512
			numStr = sizeStr[:len(sizeStr)-1]
		case 'k':
			// kilobytes
			multiplier = 1024
			numStr = sizeStr[:len(sizeStr)-1]
		case 'M':
			// megabytes
			multiplier = 1024 * 1024
			numStr = sizeStr[:len(sizeStr)-1]
		case 'G':
			// gigabytes
			multiplier = 1024 * 1024 * 1024
			numStr = sizeStr[:len(sizeStr)-1]
		default:
			// No unit, assume bytes
			numStr = sizeStr
		}
	}
	
	if len(numStr) == 0 {
		return nil, 0, fmt.Errorf("size specification missing numeric value")
	}
	
	// Parse the numeric part
	var size int64
	var err error
	
	// Handle decimal numbers for units
	if strings.Contains(numStr, ".") {
		var floatSize float64
		floatSize, err = strconv.ParseFloat(numStr, 64)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid size number: %s", numStr)
		}
		size = int64(floatSize * float64(multiplier))
	} else {
		var intSize int64
		intSize, err = strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid size number: %s", numStr)
		}
		size = intSize * multiplier
	}
	
	if size < 0 {
		return nil, 0, fmt.Errorf("size cannot be negative")
	}
	
	return &SizeTest{Size: size, Mode: mode}, 2, nil
}

func parseTimeTest(timeSpec string, timeType string) (Expression, error) {
	if len(timeSpec) == 0 {
		return nil, fmt.Errorf("empty time specification")
	}
	
	var mode string
	var timeStr string
	
	// Parse prefix (+, -, or exact)
	switch timeSpec[0] {
	case '+':
		mode = "+"
		timeStr = timeSpec[1:]
	case '-':
		mode = "-"
		timeStr = timeSpec[1:]
	default:
		mode = "="
		timeStr = timeSpec
	}
	
	if len(timeStr) == 0 {
		return nil, fmt.Errorf("time specification missing numeric value")
	}
	
	// Parse the numeric part
	value, err := strconv.Atoi(timeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid time number: %s", timeStr)
	}
	
	if value < 0 {
		return nil, fmt.Errorf("time value cannot be negative")
	}
	
	// Create appropriate test based on type
	switch timeType {
	case "mtime":
		return &MTimeTest{Days: value, Mode: mode}, nil
	case "mmin":
		return &MMinTest{Minutes: value, Mode: mode}, nil
	case "ctime":
		return &CTimeTest{Days: value, Mode: mode}, nil
	case "cmin":
		return &CMinTest{Minutes: value, Mode: mode}, nil
	default:
		return nil, fmt.Errorf("unknown time test type: %s", timeType)
	}
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