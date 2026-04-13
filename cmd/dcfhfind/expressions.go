package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"
)

// Test expressions

// NameTest matches filename against a pattern
type NameTest struct {
	Pattern       string
	CaseSensitive bool
}

func (t *NameTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	filename := filepath.Base(entry.Path)
	pattern := t.Pattern

	if !t.CaseSensitive {
		filename = strings.ToLower(filename)
		pattern = strings.ToLower(pattern)
	}

	matched, err := filepath.Match(pattern, filename)
	return matched, err
}

func (t *NameTest) String() string {
	if t.CaseSensitive {
		return fmt.Sprintf("--name %s", t.Pattern)
	}
	return fmt.Sprintf("--iname %s", t.Pattern)
}

// PathTest matches full path against a pattern
type PathTest struct {
	Pattern       string
	CaseSensitive bool
}

func (t *PathTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	path := entry.Path
	pattern := t.Pattern

	if !t.CaseSensitive {
		path = strings.ToLower(path)
		pattern = strings.ToLower(pattern)
	}

	matched, err := filepath.Match(pattern, path)
	return matched, err
}

func (t *PathTest) String() string {
	if t.CaseSensitive {
		return fmt.Sprintf("--path %s", t.Pattern)
	}
	return fmt.Sprintf("--ipath %s", t.Pattern)
}

// SizeTest compares file size
type SizeTest struct {
	Size int64
	Mode string // "=", "+", "-"
}

func (t *SizeTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	size := int64(entry.FileSize)

	switch t.Mode {
	case "+":
		return size > t.Size, nil
	case "-":
		return size < t.Size, nil
	case "=":
		return size == t.Size, nil
	default:
		return false, fmt.Errorf("invalid size mode: %s", t.Mode)
	}
}

func (t *SizeTest) String() string {
	return fmt.Sprintf("--size %s%d", t.Mode, t.Size)
}

// EmptyTest checks for zero-size files
type EmptyTest struct{}

func (t *EmptyTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	return entry.FileSize == 0, nil
}

func (t *EmptyTest) String() string {
	return "--empty"
}

// DeletedTest checks if entry is marked as deleted
type DeletedTest struct{}

func (t *DeletedTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	return entry.IsDeleted, nil
}

func (t *DeletedTest) String() string {
	return "--deleted"
}

// ValidTest checks if entry passes validation
type ValidTest struct{}

func (t *ValidTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	// Use the new validation function from dcfhfind_support.go
	valid, err := dircachefilehash.ValidateEntryInfo(entry, context.Repository)
	if err != nil {
		return false, err
	}
	return valid, nil
}

func (t *ValidTest) String() string {
	return "--valid"
}

// CorruptTest checks if entry fails validation
type CorruptTest struct{}

func (t *CorruptTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	// Use the new corruption detection function from dcfhfind_support.go
	corrupt, _ := dircachefilehash.DetectEntryCorruption(entry)
	return corrupt, nil
}

func (t *CorruptTest) String() string {
	return "--corrupt"
}

// HashTest matches exact hash value
type HashTest struct {
	Hash string
}

func (t *HashTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	entryHash := entry.HashStr
	return strings.EqualFold(entryHash, t.Hash), nil
}

func (t *HashTest) String() string {
	return fmt.Sprintf("--hash %s", t.Hash)
}

// HashPrefixTest matches hash prefix
type HashPrefixTest struct {
	Prefix string
}

func (t *HashPrefixTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	entryHash := entry.HashStr
	return strings.HasPrefix(strings.ToLower(entryHash), strings.ToLower(t.Prefix)), nil
}

func (t *HashPrefixTest) String() string {
	return fmt.Sprintf("--hash-prefix %s", t.Prefix)
}

// HashTypeTest matches hash algorithm type
type HashTypeTest struct {
	Type string
}

func (t *HashTypeTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	var typeName string

	switch entry.HashType {
	case 1: // HashTypeSHA1
		typeName = "SHA1"
	case 2: // HashTypeSHA256
		typeName = "SHA256"
	case 3: // HashTypeSHA512
		typeName = "SHA512"
	default:
		typeName = fmt.Sprintf("UNKNOWN(%d)", entry.HashType)
	}

	return strings.EqualFold(typeName, t.Type), nil
}

func (t *HashTypeTest) String() string {
	return fmt.Sprintf("--hash-type %s", t.Type)
}

// Action implementations

// PrintAction prints the path
type PrintAction struct{}

func (a *PrintAction) Execute(entry *dircachefilehash.EntryInfo, context *EvalContext) error {
	fmt.Println(entry.Path)
	return nil
}

func (a *PrintAction) String() string {
	return "--print"
}

// Print0Action prints null-terminated paths
type Print0Action struct{}

func (a *Print0Action) Execute(entry *dircachefilehash.EntryInfo, context *EvalContext) error {
	fmt.Printf("%s\000", entry.Path)
	return nil
}

func (a *Print0Action) String() string {
	return "--print0"
}

// LsAction provides detailed listing
type LsAction struct{}

func (a *LsAction) Execute(entry *dircachefilehash.EntryInfo, context *EvalContext) error {
	// Format: permissions links user group size mtime [index] path
	permissions := formatPermissions(entry.Mode)
	size := entry.FileSize
	mtime := formatTime(entry.MTimeWall)
	indexType := fmt.Sprintf("[%s]", context.IndexType)
	path := entry.Path

	// TODO: Get actual user/group names, for now use numeric
	uid := entry.UID
	gid := entry.GID

	fmt.Printf("%s %d %d %d %8d %s %s %s\n",
		permissions, 1, uid, gid, size, mtime, indexType, path)

	return nil
}

func (a *LsAction) String() string {
	return "--ls"
}

// PrintfAction formats output using format string
type PrintfAction struct {
	Format string
}

func (a *PrintfAction) Execute(entry *dircachefilehash.EntryInfo, context *EvalContext) error {
	output := a.formatString(entry, context)
	fmt.Print(output)
	return nil
}

func (a *PrintfAction) String() string {
	return fmt.Sprintf("--printf %s", a.Format)
}

func (a *PrintfAction) formatString(entry *dircachefilehash.EntryInfo, context *EvalContext) string {
	result := a.Format

	// Handle %% first to avoid interference
	result = strings.ReplaceAll(result, "%%", "\x00LITERAL_PERCENT\x00")

	// Replace format specifiers (single % like find)
	replacements := map[string]string{
		"%p": entry.Path,                         // Full path
		"%f": filepath.Base(entry.Path),          // Filename only
		"%h": filepath.Dir(entry.Path),           // Directory name
		"%s": fmt.Sprintf("%d", entry.FileSize),  // Size in bytes
		"%m": fmt.Sprintf("%o", entry.Mode&0777), // Permissions (octal)
		"%u": fmt.Sprintf("%d", entry.UID),       // UID
		"%g": fmt.Sprintf("%d", entry.GID),       // GID
		"%t": formatTime(entry.MTimeWall),        // Modification time
		"%c": formatTime(entry.CTimeWall),        // Change time
		"%H": entry.HashStr,                      // Hash value
		"%Y": formatHashType(entry.HashType),     // Hash type
		"%i": context.IndexType,                  // Index source
		"%I": context.IndexPath,                  // Full index path
		"%d": fmt.Sprintf("%d", entry.Dev),       // Device number
	}

	for pattern, replacement := range replacements {
		result = strings.ReplaceAll(result, pattern, replacement)
	}

	// Restore literal % symbols
	result = strings.ReplaceAll(result, "\x00LITERAL_PERCENT\x00", "%")

	// Handle escape sequences
	result = strings.ReplaceAll(result, "\\n", "\n")
	result = strings.ReplaceAll(result, "\\t", "\t")
	result = strings.ReplaceAll(result, "\\r", "\r")
	result = strings.ReplaceAll(result, "\\\\", "\\")

	return result
}

// ValidateAction validates the entry
type ValidateAction struct{}

func (a *ValidateAction) Execute(entry *dircachefilehash.EntryInfo, context *EvalContext) error {
	// Use comprehensive validation from dcfhfind_support.go
	valid, err := dircachefilehash.ValidateEntryInfo(entry, context.Repository)
	if err != nil {
		fmt.Printf("ERROR: %s - validation failed: %v\n", entry.Path, err)
		return nil
	}

	if valid {
		fmt.Printf("VALID: %s\n", entry.Path)
	} else {
		// Get detailed corruption information
		corrupt, issues := dircachefilehash.DetectEntryCorruption(entry)
		if corrupt {
			fmt.Printf("INVALID: %s\n", entry.Path)
			for _, issue := range issues {
				fmt.Printf("  Issue: %s\n", issue)
			}
		} else {
			fmt.Printf("INVALID: %s (failed basic validation)\n", entry.Path)
		}
	}
	return nil
}

func (a *ValidateAction) String() string {
	return "--validate"
}

// ChecksumAction verifies hash against file content
type ChecksumAction struct{}

func (a *ChecksumAction) Execute(entry *dircachefilehash.EntryInfo, context *EvalContext) error {
	// Use the new checksum verification function from dcfhfind_support.go
	matches, err := dircachefilehash.VerifyEntryChecksum(entry, context.Repository)
	if err != nil {
		if strings.Contains(err.Error(), "file does not exist") {
			fmt.Printf("MISSING: %s\n", entry.Path)
		} else {
			fmt.Printf("ERROR: %s - %v\n", entry.Path, err)
		}
		return nil
	}

	if matches {
		fmt.Printf("OK: %s\n", entry.Path)
	} else {
		// Get current hash for detailed output
		filePath := filepath.Join(context.Repository, entry.Path)
		algorithm, err := dircachefilehash.GetHashAlgorithmByType(entry.HashType)
		if err == nil {
			currentHash, err := dircachefilehash.HashFileToHexString(filePath, algorithm)
			if err == nil {
				fmt.Printf("MISMATCH: %s\n", entry.Path)
				fmt.Printf("  Stored:  %s\n", entry.HashStr)
				fmt.Printf("  Current: %s\n", currentHash)
				return nil
			}
		}
		fmt.Printf("MISMATCH: %s\n", entry.Path)
	}
	return nil
}

func (a *ChecksumAction) String() string {
	return "--checksum"
}

// FixAction applies fixes to entries (stub implementation)
type FixAction struct {
	Mode string // "auto", "manual", "none"
}

func (a *FixAction) Execute(entry *dircachefilehash.EntryInfo, context *EvalContext) error {
	// Stub implementation - just report what would be done
	switch a.Mode {
	case "auto":
		fmt.Printf("AUTO-FIX: %s (would apply automatic fixes)\n", entry.Path)
	case "manual":
		fmt.Printf("MANUAL-FIX: %s (would prompt for manual fixes)\n", entry.Path)
	case "none":
		fmt.Printf("NO-FIX: %s (validation only, no fixes applied)\n", entry.Path)
	}
	return nil
}

func (a *FixAction) String() string {
	return fmt.Sprintf("--fix %s", a.Mode)
}

// Helper functions

func formatPermissions(mode uint32) string {
	// Convert mode to ls-style permission string
	perm := "-"

	// File type
	switch mode & 0170000 {
	case 0040000:
		perm = "d" // directory
	case 0120000:
		perm = "l" // symlink
	case 0010000:
		perm = "p" // pipe
	case 0020000:
		perm = "c" // character device
	case 0060000:
		perm = "b" // block device
	case 0140000:
		perm = "s" // socket
	}

	// Owner permissions
	if mode&0400 != 0 {
		perm += "r"
	} else {
		perm += "-"
	}
	if mode&0200 != 0 {
		perm += "w"
	} else {
		perm += "-"
	}
	if mode&0100 != 0 {
		if mode&04000 != 0 { // setuid
			perm += "s"
		} else {
			perm += "x"
		}
	} else {
		if mode&04000 != 0 { // setuid
			perm += "S"
		} else {
			perm += "-"
		}
	}

	// Group permissions
	if mode&0040 != 0 {
		perm += "r"
	} else {
		perm += "-"
	}
	if mode&0020 != 0 {
		perm += "w"
	} else {
		perm += "-"
	}
	if mode&0010 != 0 {
		if mode&02000 != 0 { // setgid
			perm += "s"
		} else {
			perm += "x"
		}
	} else {
		if mode&02000 != 0 { // setgid
			perm += "S"
		} else {
			perm += "-"
		}
	}

	// Other permissions
	if mode&0004 != 0 {
		perm += "r"
	} else {
		perm += "-"
	}
	if mode&0002 != 0 {
		perm += "w"
	} else {
		perm += "-"
	}
	if mode&0001 != 0 {
		if mode&01000 != 0 { // sticky
			perm += "t"
		} else {
			perm += "x"
		}
	} else {
		if mode&01000 != 0 { // sticky
			perm += "T"
		} else {
			perm += "-"
		}
	}

	return perm
}

func formatTime(wallTime uint64) string {
	// TODO: Convert wallTime to proper time format
	// For now, use a placeholder
	if wallTime == 0 {
		return "Jan  1  1970"
	}

	// This is a simplified conversion - should use proper timeFromWall function
	t := time.Unix(int64(wallTime>>32), 0)
	return t.Format("Jan _2 15:04")
}

func formatHashType(hashType uint16) string {
	switch hashType {
	case 1:
		return "SHA1"
	case 2:
		return "SHA256"
	case 3:
		return "SHA512"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", hashType)
	}
}

// Operator expressions for complex expression parsing

// AndExpression represents logical AND between two expressions
type AndExpression struct {
	Left  Expression
	Right Expression
}

func (e *AndExpression) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	leftResult, err := e.Left.Evaluate(entry, context)
	if err != nil {
		return false, err
	}
	if !leftResult {
		return false, nil // Short-circuit evaluation
	}

	rightResult, err := e.Right.Evaluate(entry, context)
	if err != nil {
		return false, err
	}
	return rightResult, nil
}

func (e *AndExpression) String() string {
	return fmt.Sprintf("(%s --and %s)", e.Left.String(), e.Right.String())
}

// OrExpression represents logical OR between two expressions
type OrExpression struct {
	Left  Expression
	Right Expression
}

func (e *OrExpression) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	leftResult, err := e.Left.Evaluate(entry, context)
	if err != nil {
		return false, err
	}
	if leftResult {
		return true, nil // Short-circuit evaluation
	}

	rightResult, err := e.Right.Evaluate(entry, context)
	if err != nil {
		return false, err
	}
	return rightResult, nil
}

func (e *OrExpression) String() string {
	return fmt.Sprintf("(%s --or %s)", e.Left.String(), e.Right.String())
}

// NotExpression represents logical NOT of an expression
type NotExpression struct {
	Expr Expression
}

func (e *NotExpression) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	result, err := e.Expr.Evaluate(entry, context)
	if err != nil {
		return false, err
	}
	return !result, nil
}

func (e *NotExpression) String() string {
	return fmt.Sprintf("--not %s", e.Expr.String())
}

// Time-based test expressions

// MTimeTest matches files modified within a time range
type MTimeTest struct {
	Days int    // Number of days
	Mode string // "+", "-", or "="
}

func (t *MTimeTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	// Convert wall time to standard time
	mtime := dircachefilehash.TimeFromWall(entry.MTimeWall)
	now := time.Now()

	// Calculate age in days
	age := now.Sub(mtime)
	ageDays := int(age.Hours() / 24)

	switch t.Mode {
	case "+":
		return ageDays > t.Days, nil
	case "-":
		return ageDays < t.Days, nil
	case "=":
		return ageDays == t.Days, nil
	default:
		return false, fmt.Errorf("invalid mtime mode: %s", t.Mode)
	}
}

func (t *MTimeTest) String() string {
	return fmt.Sprintf("--mtime %s%d", t.Mode, t.Days)
}

// MMinTest matches files modified within a time range (minutes)
type MMinTest struct {
	Minutes int    // Number of minutes
	Mode    string // "+", "-", or "="
}

func (t *MMinTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	// Convert wall time to standard time
	mtime := dircachefilehash.TimeFromWall(entry.MTimeWall)
	now := time.Now()

	// Calculate age in minutes
	age := now.Sub(mtime)
	ageMinutes := int(age.Minutes())

	switch t.Mode {
	case "+":
		return ageMinutes > t.Minutes, nil
	case "-":
		return ageMinutes < t.Minutes, nil
	case "=":
		return ageMinutes == t.Minutes, nil
	default:
		return false, fmt.Errorf("invalid mmin mode: %s", t.Mode)
	}
}

func (t *MMinTest) String() string {
	return fmt.Sprintf("--mmin %s%d", t.Mode, t.Minutes)
}

// CTimeTest matches files with status change time within a range
type CTimeTest struct {
	Days int    // Number of days
	Mode string // "+", "-", or "="
}

func (t *CTimeTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	// Convert wall time to standard time
	ctime := dircachefilehash.TimeFromWall(entry.CTimeWall)
	now := time.Now()

	// Calculate age in days
	age := now.Sub(ctime)
	ageDays := int(age.Hours() / 24)

	switch t.Mode {
	case "+":
		return ageDays > t.Days, nil
	case "-":
		return ageDays < t.Days, nil
	case "=":
		return ageDays == t.Days, nil
	default:
		return false, fmt.Errorf("invalid ctime mode: %s", t.Mode)
	}
}

func (t *CTimeTest) String() string {
	return fmt.Sprintf("--ctime %s%d", t.Mode, t.Days)
}

// CMinTest matches files with status change time within a range (minutes)
type CMinTest struct {
	Minutes int    // Number of minutes
	Mode    string // "+", "-", or "="
}

func (t *CMinTest) Evaluate(entry *dircachefilehash.EntryInfo, context *EvalContext) (bool, error) {
	// Convert wall time to standard time
	ctime := dircachefilehash.TimeFromWall(entry.CTimeWall)
	now := time.Now()

	// Calculate age in minutes
	age := now.Sub(ctime)
	ageMinutes := int(age.Minutes())

	switch t.Mode {
	case "+":
		return ageMinutes > t.Minutes, nil
	case "-":
		return ageMinutes < t.Minutes, nil
	case "=":
		return ageMinutes == t.Minutes, nil
	default:
		return false, fmt.Errorf("invalid cmin mode: %s", t.Mode)
	}
}

func (t *CMinTest) String() string {
	return fmt.Sprintf("--cmin %s%d", t.Mode, t.Minutes)
}
