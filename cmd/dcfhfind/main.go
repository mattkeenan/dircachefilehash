//go:generate go run generate_version.go

package main

import (
	"context"
	"fmt"
	"os"
	"slices"
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
	repoRoot, metaDir, err := discoverRepository(args.RepoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dcfhfind: %v\n", err)
		os.Exit(1)
	}
	args.RepoPath = repoRoot

	// Execute the find operation via the Repo abstraction
	if err := executeFind(context.Background(), metaDir, args); err != nil {
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
	fmt.Printf("  --name PATTERN    Match basename (gitignore syntax — same as .dcfh/ignore)\n")
	fmt.Printf("  --path PATTERN    Match full path (gitignore syntax)\n")
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

// Expression / Action / EvalContext / IndexFile are re-exported from pkg
// via aliases.go; see pkg/filter.go and pkg/filter_run.go for the canonical
// definitions.

// IndexFile mirrors pkg.IndexRef so existing tests keep working.
type IndexFile = dircachefilehash.IndexRef

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
	return slices.Contains(tests, token)
}

func (p *ExpressionParser) isGlobalOption(token string) bool {
	globals := []string{"--repo", "--maxdepth", "--warn", "--nowarn", "--tz"}
	return slices.Contains(globals, token)
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
	case "--tz":
		// --tz is shared with the cmd/dcfh flat-flag dialect: it sets
		// the IANA zone for parsing bare --start-date / --end-date
		// values. Stored as a global so subsequent expression tokens
		// can resolve it without needing to traverse forward.
		if p.pos >= len(p.tokens) {
			return nil, fmt.Errorf("--tz requires an argument")
		}
		value := p.next()
		p.globalArgs["--tz"] = value
	}

	return nil, nil // Global options don't produce expressions
}

func (p *ExpressionParser) parseBasicExpression() (Expression, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	token := p.next()
	if expr, handled, err := p.parseTestToken(token); handled {
		return expr, err
	}
	if handled, err := p.parseActionToken(token); handled {
		return nil, err
	}
	return nil, fmt.Errorf("unknown expression: %s", token)
}

// requireArg consumes the next token, returning it or a descriptive
// error if the token stream is exhausted.
func (p *ExpressionParser) requireArg(flag, what string) (string, error) {
	if p.pos >= len(p.tokens) {
		return "", fmt.Errorf("%s requires %s", flag, what)
	}
	return p.next(), nil
}

// parseTestToken handles the --name/--size/--hash/... family. The
// bool return indicates "this token is a test"; caller falls through
// to action parsing when false.
func (p *ExpressionParser) parseTestToken(token string) (Expression, bool, error) {
	// Tests taking no argument.
	switch token {
	case "--empty":
		return &EmptyTest{}, true, nil
	case "--deleted":
		return &DeletedTest{}, true, nil
	case "--valid":
		return &ValidTest{}, true, nil
	case "--corrupt":
		return &CorruptTest{}, true, nil
	}

	// Inclusive size and absolute-date predicates need access to parser
	// state (zone for date parsing) or a different constructor shape
	// than argTestTable supports, so they're inlined here.
	switch token {
	case "--min-size":
		arg, err := p.requireArg(token, "a size bound (N[K|M|G|T])")
		if err != nil {
			return nil, true, err
		}
		n, err := dircachefilehash.ParseSizeBound(arg)
		if err != nil {
			return nil, true, fmt.Errorf("--min-size: %w", err)
		}
		return &dircachefilehash.MinSizeTest{Min: n}, true, nil
	case "--max-size":
		arg, err := p.requireArg(token, "a size bound (N[K|M|G|T])")
		if err != nil {
			return nil, true, err
		}
		n, err := dircachefilehash.ParseSizeBound(arg)
		if err != nil {
			return nil, true, fmt.Errorf("--max-size: %w", err)
		}
		return &dircachefilehash.MaxSizeTest{Max: n}, true, nil
	case "--start-date":
		arg, err := p.requireArg(token, "a partial ISO-8601 date")
		if err != nil {
			return nil, true, err
		}
		zone, err := dircachefilehash.ResolveZone(p.globalArgs["--tz"])
		if err != nil {
			return nil, true, err
		}
		t, err := dircachefilehash.ParsePartialDateTime(arg, zone)
		if err != nil {
			return nil, true, fmt.Errorf("--start-date: %w", err)
		}
		return &dircachefilehash.MTimeRangeTest{Start: t}, true, nil
	case "--end-date":
		arg, err := p.requireArg(token, "a partial ISO-8601 date")
		if err != nil {
			return nil, true, err
		}
		zone, err := dircachefilehash.ResolveZone(p.globalArgs["--tz"])
		if err != nil {
			return nil, true, err
		}
		t, err := dircachefilehash.ParsePartialDateTime(arg, zone)
		if err != nil {
			return nil, true, fmt.Errorf("--end-date: %w", err)
		}
		return &dircachefilehash.MTimeRangeTest{End: t}, true, nil
	}

	spec, ok := argTestTable[token]
	if !ok {
		return nil, false, nil
	}
	arg, err := p.requireArg(token, spec.what)
	if err != nil {
		return nil, true, err
	}
	expr, err := spec.build(arg)
	return expr, true, err
}

// argTestSpec is the per-flag data for a test that takes one
// argument: the human description of what the arg should be, and a
// constructor that turns the arg into an Expression.
type argTestSpec struct {
	what  string
	build func(string) (Expression, error)
}

// argTestTable dispatches each --flag to its argument description
// and Expression constructor. Package-level so the map and its
// closures are allocated exactly once, not per parse call.
var argTestTable = map[string]argTestSpec{
	"--name":        {"a pattern", func(a string) (Expression, error) { return dircachefilehash.NewNameTest(a, true) }},
	"--iname":       {"a pattern", func(a string) (Expression, error) { return dircachefilehash.NewNameTest(a, false) }},
	"--path":        {"a pattern", func(a string) (Expression, error) { return dircachefilehash.NewPathTest(a, true) }},
	"--ipath":       {"a pattern", func(a string) (Expression, error) { return dircachefilehash.NewPathTest(a, false) }},
	"--size":        {"a size specification", parseSizeTest},
	"--hash":        {"a hash value", func(a string) (Expression, error) { return &HashTest{Hash: a}, nil }},
	"--mtime":       {"a time specification", func(a string) (Expression, error) { return parseTimeTest(a, "mtime") }},
	"--mmin":        {"a time specification", func(a string) (Expression, error) { return parseTimeTest(a, "mmin") }},
	"--ctime":       {"a time specification", func(a string) (Expression, error) { return parseTimeTest(a, "ctime") }},
	"--cmin":        {"a time specification", func(a string) (Expression, error) { return parseTimeTest(a, "cmin") }},
	"--hash-prefix": {"a prefix", func(a string) (Expression, error) { return &HashPrefixTest{Prefix: a}, nil }},
	"--hash-type":   {"a type", func(a string) (Expression, error) { return &HashTypeTest{Type: a}, nil }},
}

// parseActionToken handles --print/--ls/--printf/etc. Actions don't
// produce expressions; the bool indicates "this token was an action".
func (p *ExpressionParser) parseActionToken(token string) (bool, error) {
	switch token {
	case "--print":
		p.actions = append(p.actions, &PrintAction{})
		return true, nil
	case "--print0":
		p.actions = append(p.actions, &Print0Action{})
		return true, nil
	case "--ls":
		p.actions = append(p.actions, &LsAction{})
		return true, nil
	case "--validate":
		p.actions = append(p.actions, &ValidateAction{})
		return true, nil
	case "--checksum":
		p.actions = append(p.actions, &ChecksumAction{})
		return true, nil
	case "--printf":
		format, err := p.requireArg("--printf", "a format string")
		if err != nil {
			return true, err
		}
		p.actions = append(p.actions, &PrintfAction{Format: format})
		return true, nil
	case "--fix":
		mode, err := p.requireArg("--fix", "an argument (auto|manual|none)")
		if err != nil {
			return true, err
		}
		if mode != "auto" && mode != "manual" && mode != "none" {
			return true, fmt.Errorf("--fix argument must be auto, manual, or none")
		}
		p.actions = append(p.actions, &FixAction{Mode: mode})
		return true, nil
	}
	return false, nil
}

func parseSizeTest(sizeSpec string) (Expression, error) {
	if len(sizeSpec) == 0 {
		return nil, fmt.Errorf("empty size specification")
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
		return nil, fmt.Errorf("size specification missing numeric value")
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
		return nil, fmt.Errorf("size specification missing numeric value")
	}

	// Parse the numeric part
	var size int64
	var err error

	// Handle decimal numbers for units
	if strings.Contains(numStr, ".") {
		var floatSize float64
		floatSize, err = strconv.ParseFloat(numStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid size number: %s", numStr)
		}
		size = int64(floatSize * float64(multiplier))
	} else {
		var intSize int64
		intSize, err = strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid size number: %s", numStr)
		}
		size = intSize * multiplier
	}

	if size < 0 {
		return nil, fmt.Errorf("size cannot be negative")
	}

	return &SizeTest{Size: size, Mode: mode}, nil
}

func parseTimeTest(timeSpec string, timeType string) (Expression, error) {
	value, mode, err := dircachefilehash.ParseAgeSpec(timeSpec)
	if err != nil {
		return nil, err
	}
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

func discoverRepository(repoPath string) (string, string, error) {
	if repoPath == "" {
		repoPath = "."
	}
	return dircachefilehash.DiscoverRepository(repoPath)
}

// resolveStartingPoints is a thin wrapper over pkg.ResolveIndexSelectors
// retained so existing tests and call sites keep working.
func resolveStartingPoints(startingPoints []string, metaDir string) ([]IndexFile, error) {
	refs, err := dircachefilehash.ResolveIndexSelectors(metaDir, startingPoints)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no accessible index files found")
	}
	return refs, nil
}

// executeFind delegates to repo.Filter, collapsing the implicit-AND list of
// expressions into a single root predicate.
func executeFind(ctx context.Context, metaDir string, args *Arguments) error {
	repo, err := dircachefilehash.OpenRepo(ctx, metaDir)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}
	defer func() { _ = repo.Close() }()

	var root Expression
	for _, e := range args.Expressions {
		if root == nil {
			root = e
			continue
		}
		root = &AndExpression{Left: root, Right: e}
	}

	req := dircachefilehash.FilterRequest{
		IndexSelectors: args.StartingPoints,
		Repository:     args.RepoPath,
		Expression:     root,
		Actions:        args.Actions,
		Warn:           args.GlobalOptions.Warn,
	}
	_, err = repo.Filter(ctx, req)
	return err
}
