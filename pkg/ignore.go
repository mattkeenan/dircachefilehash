package dircachefilehash

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// IgnoreManager handles ignore patterns for dcfh
type IgnoreManager struct {
	ignorePath string
	patterns   []*regexp.Regexp
	loaded     bool
}

// NewIgnoreManager creates a new ignore manager
func NewIgnoreManager(dcfhDir string) *IgnoreManager {
	return &IgnoreManager{
		ignorePath: filepath.Join(dcfhDir, ".dcfh", "ignore"),
		patterns:   make([]*regexp.Regexp, 0),
		loaded:     false,
	}
}

// LoadIgnorePatterns loads ignore patterns from the ignore file
func (im *IgnoreManager) LoadIgnorePatterns() error {
	if im.loaded {
		return nil // Already loaded
	}

	// Check if ignore file exists
	if _, err := os.Stat(im.ignorePath); os.IsNotExist(err) {
		// Create empty ignore file
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
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Compile regex pattern
		pattern, err := regexp.Compile(line)
		if err != nil {
			return fmt.Errorf("invalid regex pattern at line %d: %s - %w", lineNum, line, err)
		}

		im.patterns = append(im.patterns, pattern)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading ignore file: %w", err)
	}

	im.loaded = true
	return nil
}

// ShouldIgnore checks if a path should be ignored based on patterns
func (im *IgnoreManager) ShouldIgnore(relativePath string) bool {
	if !im.loaded {
		// Silently load patterns if not loaded yet
		if err := im.LoadIgnorePatterns(); err != nil {
			return false // Don't ignore on error
		}
	}

	// Normalise path separators to forward slashes for consistent pattern matching
	normalisedPath := filepath.ToSlash(relativePath)

	for _, pattern := range im.patterns {
		if pattern.MatchString(normalisedPath) {
			return true
		}
	}

	return false
}

// CreateEmptyIgnoreFile creates an empty ignore file with helpful comments
func (im *IgnoreManager) CreateEmptyIgnoreFile() error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(im.ignorePath), 0755); err != nil {
		return err
	}

	file, err := os.Create(im.ignorePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write helpful header comments
	_, err = file.WriteString(`# dcfh ignore patterns
# 
# This file contains regular expression patterns for files and directories
# that should be ignored by dcfh indexing operations.
#
# Each line should contain a valid Go regular expression.
# Lines starting with # are comments and are ignored.
# Empty lines are also ignored.
#
# Examples:
# \.git/.*              # Ignore .git directory and all contents
# \.DS_Store$           # Ignore .DS_Store files
# .*\.tmp$              # Ignore all .tmp files
# node_modules/.*       # Ignore node_modules directory
# \.dcfh/.*             # Ignore .dcfh directory (automatically added)

# Automatically ignore .dcfh directory
\.dcfh/.*
`)

	return err
}

// AddPattern adds a new ignore pattern
func (im *IgnoreManager) AddPattern(patternStr string) error {
	pattern, err := regexp.Compile(patternStr)
	if err != nil {
		return fmt.Errorf("invalid regex pattern: %s - %w", patternStr, err)
	}

	im.patterns = append(im.patterns, pattern)
	return nil
}

// SaveIgnorePatterns saves current patterns to the ignore file
func (im *IgnoreManager) SaveIgnorePatterns() error {
	file, err := os.Create(im.ignorePath)
	if err != nil {
		return fmt.Errorf("failed to create ignore file: %w", err)
	}
	defer file.Close()

	// Write header
	_, err = file.WriteString(`# dcfh ignore patterns
# 
# This file contains regular expression patterns for files and directories
# that should be ignored by dcfh indexing operations.
#

`)
	if err != nil {
		return err
	}

	// Write patterns
	for _, pattern := range im.patterns {
		if _, err := file.WriteString(pattern.String() + "\n"); err != nil {
			return err
		}
	}

	return nil
}

// GetPatterns returns all loaded patterns
func (im *IgnoreManager) GetPatterns() []*regexp.Regexp {
	if !im.loaded {
		im.LoadIgnorePatterns() // Load if not already loaded
	}
	return im.patterns
}

// IsLoaded returns true if patterns have been loaded
func (im *IgnoreManager) IsLoaded() bool {
	return im.loaded
}

// Reload forces a reload of ignore patterns from file
func (im *IgnoreManager) Reload() error {
	im.patterns = make([]*regexp.Regexp, 0)
	im.loaded = false
	return im.LoadIgnorePatterns()
}

// ValidatePattern checks if a pattern string is a valid regex
func (im *IgnoreManager) ValidatePattern(patternStr string) error {
	_, err := regexp.Compile(patternStr)
	return err
}

// HasPatterns returns true if there are any ignore patterns loaded
func (im *IgnoreManager) HasPatterns() bool {
	if !im.loaded {
		im.LoadIgnorePatterns() // Load if not already loaded
	}
	return len(im.patterns) > 0
}

// FilterIgnoredPaths filters a slice of paths, removing ignored ones
func (im *IgnoreManager) FilterIgnoredPaths(paths []string) []string {
	if !im.HasPatterns() {
		return paths // No patterns, return all paths
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
