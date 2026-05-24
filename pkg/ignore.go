package dircachefilehash

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// IgnoreManager handles ignore patterns for dcfh. Patterns are
// gitignore(5)-style — the same syntax accepted by `--name` / `--path`
// CLI flags — so a single pattern works in either surface unchanged.
//
// The raw source lines are kept alongside the parsed Patterns so the
// wire/RPC layer can ship them to a remote walker (gitignore.Pattern
// itself doesn't expose its source string).
type IgnoreManager struct {
	ignorePath string
	rawLines   []string
	patterns   []gitignore.Pattern
	matcher    gitignore.Matcher
	loaded     bool

	// suppressFile makes LoadIgnorePatterns a no-op so the .dcfh/ignore
	// file has no effect for this run. Set by --no-ignore-file before any
	// load happens; flipping it after load does not retroactively clear
	// already-parsed patterns (call Reload for that).
	suppressFile bool
}

// NewIgnoreManager creates a new ignore manager.
// metaDir is the .dcfh metadata directory path (e.g. /path/.dcfh or /path/foo.dcfh).
func NewIgnoreManager(metaDir string) *IgnoreManager {
	return &IgnoreManager{
		ignorePath: filepath.Join(metaDir, "ignore"),
		patterns:   make([]gitignore.Pattern, 0),
		loaded:     false,
	}
}

// SetSuppressFile toggles whether LoadIgnorePatterns reads .dcfh/ignore.
// Must be called before the first load (or paired with Reload) to take
// effect — once loaded=true, the parsed patterns stay live regardless.
func (im *IgnoreManager) SetSuppressFile(suppress bool) {
	im.suppressFile = suppress
}

// CompileIgnorePattern parses one gitignore-style pattern line into a
// matcher. Empty lines and comments (`#…`) return (nil, nil). The error
// return is reserved: today's gitignore parser accepts every input
// string (silently producing a no-op matcher for nonsense), but the
// shape lets future validation surface failures without churn.
//
// This is the single entry point for both .dcfh/ignore lines and the
// CLI `--name` / `--path` family — one definition of "what counts as a
// valid dcfh pattern".
func CompileIgnorePattern(line string) (gitignore.Pattern, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, nil
	}
	return gitignore.ParsePattern(line, nil), nil
}

// LoadIgnorePatterns loads ignore patterns from the ignore file.
//
// Lines that look like RE2 regex (a leftover from the pre-gitignore
// dcfh format) emit a one-line stderr warning so users can spot the
// migration; the line is still parsed as gitignore and may behave
// differently than it used to.
func (im *IgnoreManager) LoadIgnorePatterns() error {
	if im.loaded {
		return nil
	}
	if im.suppressFile {
		im.loaded = true
		return nil
	}

	if _, err := os.Stat(im.ignorePath); os.IsNotExist(err) {
		if err := im.CreateEmptyIgnoreFile(); err != nil {
			return fmt.Errorf("failed to create ignore file: %w", err)
		}
		im.loaded = true
		return nil
	}

	file, err := os.Open(im.ignorePath)
	if err != nil {
		return fmt.Errorf("failed to open ignore file: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()
		if looksLikeRE2(raw) {
			fmt.Fprintf(os.Stderr,
				"warning: %s line %d: pattern %q looks like RE2 regex; "+
					"dcfh now uses gitignore syntax — see CHANGELOG.\n",
				im.ignorePath, lineNum, strings.TrimSpace(raw))
		}
		pat, err := CompileIgnorePattern(raw)
		if err != nil {
			return fmt.Errorf("invalid pattern at line %d: %s - %w", lineNum, raw, err)
		}
		if pat == nil {
			continue
		}
		im.patterns = append(im.patterns, pat)
		im.rawLines = append(im.rawLines, strings.TrimSpace(raw))
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading ignore file: %w", err)
	}

	im.matcher = gitignore.NewMatcher(im.patterns)
	im.loaded = true
	return nil
}

// looksLikeRE2 heuristically detects ignore-file lines that were valid
// in the previous RE2-regex syntax but probably aren't what the user
// wants under gitignore. Narrow on purpose: only the strongest tells
// (anchors and escaped dots) so we avoid false positives on legitimate
// gitignore patterns.
func looksLikeRE2(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	return strings.Contains(trimmed, `\.`) ||
		strings.HasPrefix(trimmed, "^") ||
		strings.HasSuffix(trimmed, "$")
}

// ShouldIgnore checks if a path should be ignored based on patterns.
// relativePath is forward-slash relative to the repository root.
func (im *IgnoreManager) ShouldIgnore(relativePath string) bool {
	if !im.loaded {
		if err := im.LoadIgnorePatterns(); err != nil {
			return false
		}
	}
	if im.matcher == nil || len(im.patterns) == 0 {
		return false
	}
	segments := splitForGitignore(relativePath)
	if len(segments) == 0 {
		return false
	}
	return im.matcher.Match(segments, false)
}

// splitForGitignore turns a relative path into the []string segment
// form gitignore expects. Empty segments (leading "/") are dropped so
// the matcher sees clean components.
func splitForGitignore(relativePath string) []string {
	rel := filepath.ToSlash(relativePath)
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return nil
	}
	return strings.Split(rel, "/")
}

// CreateEmptyIgnoreFile creates an empty ignore file with helpful comments
func (im *IgnoreManager) CreateEmptyIgnoreFile() error {
	if err := os.MkdirAll(filepath.Dir(im.ignorePath), 0755); err != nil { //nolint:gosec // G301: .dcfh/ dir, non-secret
		return err
	}

	file, err := os.Create(im.ignorePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	_, err = file.WriteString(`# dcfh ignore patterns
#
# Each non-empty, non-comment line is a gitignore(5) pattern. The same
# syntax is accepted by the --name / --path CLI flags, so a pattern
# works identically in either place.
#
# Note: the repository's own .dcfh/ directory is automatically skipped
# during scanning (hardcoded, not via ignore patterns). You do not need
# to add a pattern for it here.
#
# Examples:
# .DS_Store          # ignore .DS_Store anywhere
# *.tmp              # ignore *.tmp anywhere
# /build             # ignore the build/ directory at the repo root only
# node_modules/      # ignore node_modules directories
# **/*.log           # ignore .log files at any depth
`)

	return err
}

// AddPattern adds a new ignore pattern (gitignore syntax).
func (im *IgnoreManager) AddPattern(patternStr string) error {
	pat, err := CompileIgnorePattern(patternStr)
	if err != nil {
		return fmt.Errorf("invalid pattern: %s - %w", patternStr, err)
	}
	if pat == nil {
		return fmt.Errorf("empty pattern")
	}
	im.patterns = append(im.patterns, pat)
	im.rawLines = append(im.rawLines, strings.TrimSpace(patternStr))
	im.matcher = gitignore.NewMatcher(im.patterns)
	return nil
}

// RawLines returns the source pattern strings in the order they were
// loaded/added. Used by the wire layer to ship patterns to a remote
// walker without losing fidelity.
func (im *IgnoreManager) RawLines() []string {
	if !im.loaded {
		_ = im.LoadIgnorePatterns()
	}
	return im.rawLines
}

// SaveIgnorePatterns saves the current pattern lines to the ignore file.
//
// We can only write back the original raw lines, but Pattern doesn't
// expose its source string. SaveIgnorePatterns is therefore a no-op for
// patterns added via AddPattern in-memory only — callers that need
// round-trip support should edit the file directly.
func (im *IgnoreManager) SaveIgnorePatterns() error {
	return fmt.Errorf("SaveIgnorePatterns is not supported on the gitignore-backed IgnoreManager; edit %s directly", im.ignorePath)
}

// GetPatterns returns the loaded gitignore patterns.
func (im *IgnoreManager) GetPatterns() []gitignore.Pattern {
	if !im.loaded {
		_ = im.LoadIgnorePatterns()
	}
	return im.patterns
}

// IsLoaded returns true if patterns have been loaded
func (im *IgnoreManager) IsLoaded() bool {
	return im.loaded
}

// Reload forces a reload of ignore patterns from file
func (im *IgnoreManager) Reload() error {
	im.patterns = im.patterns[:0]
	im.rawLines = im.rawLines[:0]
	im.matcher = nil
	im.loaded = false
	return im.LoadIgnorePatterns()
}

// ValidatePattern checks if a pattern string is a usable gitignore line.
// Empty/comment lines are rejected. Otherwise the pattern is accepted
// (gitignore parsing has no compile-time failure mode).
func (im *IgnoreManager) ValidatePattern(patternStr string) error {
	pat, err := CompileIgnorePattern(patternStr)
	if err != nil {
		return err
	}
	if pat == nil {
		return fmt.Errorf("empty pattern")
	}
	return nil
}

// HasPatterns returns true if there are any ignore patterns loaded
func (im *IgnoreManager) HasPatterns() bool {
	if !im.loaded {
		_ = im.LoadIgnorePatterns()
	}
	return len(im.patterns) > 0
}

// FilterIgnoredPaths filters a slice of paths, removing ignored ones
func (im *IgnoreManager) FilterIgnoredPaths(paths []string) []string {
	if !im.HasPatterns() {
		return paths
	}

	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if !im.ShouldIgnore(path) {
			filtered = append(filtered, path)
		}
	}
	return filtered
}

// GetIgnoreFilePath returns the path to the ignore file
func (im *IgnoreManager) GetIgnoreFilePath() string {
	return im.ignorePath
}
