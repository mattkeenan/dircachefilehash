package dircachefilehash

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// FilterExpr is a predicate node in a dcfhfind-style expression tree.
// Implementations cover leaf tests (NameTest, SizeTest, …) and logical
// operators (AndExpression, OrExpression, NotExpression).
type FilterExpr interface {
	Evaluate(entry *EntryInfo, ctx *FilterContext) (bool, error)
	String() string
}

// FilterAction is an action node executed on every matching entry (PrintAction,
// LsAction, PrintfAction, ValidateAction, ChecksumAction, FixAction).
type FilterAction interface {
	Execute(entry *EntryInfo, ctx *FilterContext) error
	String() string
}

// FilterContext is the per-entry evaluation context threaded through
// FilterExpr.Evaluate and FilterAction.Execute.
type FilterContext struct {
	IndexPath    string
	IndexType    string
	Repository   string
	EntryPath    string
	RelativePath string
}

// NameTest matches filename against a glob pattern.
type NameTest struct {
	Pattern       string
	CaseSensitive bool
}

func (t *NameTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	filename := filepath.Base(entry.Path)
	pattern := t.Pattern
	if !t.CaseSensitive {
		filename = strings.ToLower(filename)
		pattern = strings.ToLower(pattern)
	}
	return filepath.Match(pattern, filename)
}

func (t *NameTest) String() string {
	if t.CaseSensitive {
		return fmt.Sprintf("--name %s", t.Pattern)
	}
	return fmt.Sprintf("--iname %s", t.Pattern)
}

// PathTest matches full path against a glob pattern.
type PathTest struct {
	Pattern       string
	CaseSensitive bool
}

func (t *PathTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	path := entry.Path
	pattern := t.Pattern
	if !t.CaseSensitive {
		path = strings.ToLower(path)
		pattern = strings.ToLower(pattern)
	}
	return filepath.Match(pattern, path)
}

func (t *PathTest) String() string {
	if t.CaseSensitive {
		return fmt.Sprintf("--path %s", t.Pattern)
	}
	return fmt.Sprintf("--ipath %s", t.Pattern)
}

// SizeTest compares file size.
type SizeTest struct {
	Size int64
	Mode string // "=", "+", "-"
}

func (t *SizeTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
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

// EmptyTest matches zero-size files.
type EmptyTest struct{}

func (t *EmptyTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	return entry.FileSize == 0, nil
}

func (t *EmptyTest) String() string { return "--empty" }

// DeletedTest matches entries flagged as deleted.
type DeletedTest struct{}

func (t *DeletedTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	return entry.IsDeleted, nil
}

func (t *DeletedTest) String() string { return "--deleted" }

// ValidTest matches entries that pass ValidateEntryInfo.
type ValidTest struct{}

func (t *ValidTest) Evaluate(entry *EntryInfo, ctx *FilterContext) (bool, error) {
	valid, err := ValidateEntryInfo(entry, ctx.Repository)
	if err != nil {
		return false, err
	}
	return valid, nil
}

func (t *ValidTest) String() string { return "--valid" }

// CorruptTest matches entries that DetectEntryCorruption flags.
type CorruptTest struct{}

func (t *CorruptTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	corrupt, _ := DetectEntryCorruption(entry)
	return corrupt, nil
}

func (t *CorruptTest) String() string { return "--corrupt" }

// HashTest matches an exact hash value (case-insensitive).
type HashTest struct {
	Hash string
}

func (t *HashTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	return strings.EqualFold(entry.HashStr, t.Hash), nil
}

func (t *HashTest) String() string { return fmt.Sprintf("--hash %s", t.Hash) }

// HashPrefixTest matches a hash prefix (case-insensitive).
type HashPrefixTest struct {
	Prefix string
}

func (t *HashPrefixTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	return strings.HasPrefix(strings.ToLower(entry.HashStr), strings.ToLower(t.Prefix)), nil
}

func (t *HashPrefixTest) String() string { return fmt.Sprintf("--hash-prefix %s", t.Prefix) }

// HashTypeTest matches the hash algorithm name (SHA1/SHA256/SHA512).
type HashTypeTest struct {
	Type string
}

func (t *HashTypeTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	var name string
	switch entry.HashType {
	case 1:
		name = "SHA1"
	case 2:
		name = "SHA256"
	case 3:
		name = "SHA512"
	default:
		name = fmt.Sprintf("UNKNOWN(%d)", entry.HashType)
	}
	return strings.EqualFold(name, t.Type), nil
}

func (t *HashTypeTest) String() string { return fmt.Sprintf("--hash-type %s", t.Type) }

// MTimeTest / MMinTest / CTimeTest / CMinTest — age-based tests.
type MTimeTest struct {
	Days int
	Mode string
}

func (t *MTimeTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	age := time.Since(TimeFromWall(entry.MTimeWall))
	return compareAge(int(age.Hours()/24), t.Days, t.Mode, "mtime")
}

func (t *MTimeTest) String() string { return fmt.Sprintf("--mtime %s%d", t.Mode, t.Days) }

type MMinTest struct {
	Minutes int
	Mode    string
}

func (t *MMinTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	age := time.Since(TimeFromWall(entry.MTimeWall))
	return compareAge(int(age.Minutes()), t.Minutes, t.Mode, "mmin")
}

func (t *MMinTest) String() string { return fmt.Sprintf("--mmin %s%d", t.Mode, t.Minutes) }

type CTimeTest struct {
	Days int
	Mode string
}

func (t *CTimeTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	age := time.Since(TimeFromWall(entry.CTimeWall))
	return compareAge(int(age.Hours()/24), t.Days, t.Mode, "ctime")
}

func (t *CTimeTest) String() string { return fmt.Sprintf("--ctime %s%d", t.Mode, t.Days) }

type CMinTest struct {
	Minutes int
	Mode    string
}

func (t *CMinTest) Evaluate(entry *EntryInfo, _ *FilterContext) (bool, error) {
	age := time.Since(TimeFromWall(entry.CTimeWall))
	return compareAge(int(age.Minutes()), t.Minutes, t.Mode, "cmin")
}

func (t *CMinTest) String() string { return fmt.Sprintf("--cmin %s%d", t.Mode, t.Minutes) }

func compareAge(actual, want int, mode, kind string) (bool, error) {
	switch mode {
	case "+":
		return actual > want, nil
	case "-":
		return actual < want, nil
	case "=":
		return actual == want, nil
	default:
		return false, fmt.Errorf("invalid %s mode: %s", kind, mode)
	}
}

// AndExpression, OrExpression, NotExpression — logical operators.
type AndExpression struct{ Left, Right FilterExpr }

func (e *AndExpression) Evaluate(entry *EntryInfo, ctx *FilterContext) (bool, error) {
	ok, err := e.Left.Evaluate(entry, ctx)
	if err != nil || !ok {
		return false, err
	}
	return e.Right.Evaluate(entry, ctx)
}

func (e *AndExpression) String() string {
	return fmt.Sprintf("(%s --and %s)", e.Left.String(), e.Right.String())
}

type OrExpression struct{ Left, Right FilterExpr }

func (e *OrExpression) Evaluate(entry *EntryInfo, ctx *FilterContext) (bool, error) {
	ok, err := e.Left.Evaluate(entry, ctx)
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	return e.Right.Evaluate(entry, ctx)
}

func (e *OrExpression) String() string {
	return fmt.Sprintf("(%s --or %s)", e.Left.String(), e.Right.String())
}

type NotExpression struct{ Expr FilterExpr }

func (e *NotExpression) Evaluate(entry *EntryInfo, ctx *FilterContext) (bool, error) {
	ok, err := e.Expr.Evaluate(entry, ctx)
	if err != nil {
		return false, err
	}
	return !ok, nil
}

func (e *NotExpression) String() string { return fmt.Sprintf("--not %s", e.Expr.String()) }

// Action implementations.

// PrintAction prints the entry's path with a trailing newline.
type PrintAction struct{}

func (a *PrintAction) Execute(entry *EntryInfo, _ *FilterContext) error {
	_, err := fmt.Println(entry.Path)
	return err
}

func (a *PrintAction) String() string { return "--print" }

// Print0Action prints the entry's path null-terminated.
type Print0Action struct{}

func (a *Print0Action) Execute(entry *EntryInfo, _ *FilterContext) error {
	_, err := fmt.Printf("%s\x00", entry.Path)
	return err
}

func (a *Print0Action) String() string { return "--print0" }

// LsAction prints a detailed ls-style listing.
type LsAction struct{}

func (a *LsAction) Execute(entry *EntryInfo, ctx *FilterContext) error {
	_, err := fmt.Printf("%s %d %d %d %8d %s [%s] %s\n",
		FormatPermissions(entry.Mode), 1, entry.UID, entry.GID, entry.FileSize,
		FormatFilterTime(entry.MTimeWall), ctx.IndexType, entry.Path)
	return err
}

func (a *LsAction) String() string { return "--ls" }

// PrintfAction formats each entry using a find(1)-style format string.
type PrintfAction struct {
	Format string
}

func (a *PrintfAction) Execute(entry *EntryInfo, ctx *FilterContext) error {
	_, err := fmt.Print(a.format(entry, ctx))
	return err
}

func (a *PrintfAction) String() string { return fmt.Sprintf("--printf %s", a.Format) }

func (a *PrintfAction) format(entry *EntryInfo, ctx *FilterContext) string {
	result := strings.ReplaceAll(a.Format, "%%", "\x00LITERAL_PERCENT\x00")

	replacements := map[string]string{
		"%p": entry.Path,
		"%f": filepath.Base(entry.Path),
		"%h": filepath.Dir(entry.Path),
		"%s": fmt.Sprintf("%d", entry.FileSize),
		"%m": fmt.Sprintf("%o", entry.Mode&0o777),
		"%u": fmt.Sprintf("%d", entry.UID),
		"%g": fmt.Sprintf("%d", entry.GID),
		"%t": FormatFilterTime(entry.MTimeWall),
		"%c": FormatFilterTime(entry.CTimeWall),
		"%H": entry.HashStr,
		"%Y": FormatHashTypeName(entry.HashType),
		"%i": ctx.IndexType,
		"%I": ctx.IndexPath,
		"%d": fmt.Sprintf("%d", entry.Dev),
	}
	for pat, rep := range replacements {
		result = strings.ReplaceAll(result, pat, rep)
	}
	result = strings.ReplaceAll(result, "\x00LITERAL_PERCENT\x00", "%")
	result = strings.ReplaceAll(result, "\\n", "\n")
	result = strings.ReplaceAll(result, "\\t", "\t")
	result = strings.ReplaceAll(result, "\\r", "\r")
	result = strings.ReplaceAll(result, "\\\\", "\\")
	return result
}

// ValidateAction runs full validation and prints VALID/INVALID lines.
type ValidateAction struct{}

func (a *ValidateAction) Execute(entry *EntryInfo, ctx *FilterContext) error {
	valid, err := ValidateEntryInfo(entry, ctx.Repository)
	if err != nil {
		fmt.Printf("ERROR: %s - validation failed: %v\n", entry.Path, err)
		return nil
	}
	if valid {
		fmt.Printf("VALID: %s\n", entry.Path)
		return nil
	}
	corrupt, issues := DetectEntryCorruption(entry)
	if corrupt {
		fmt.Printf("INVALID: %s\n", entry.Path)
		for _, issue := range issues {
			fmt.Printf("  Issue: %s\n", issue)
		}
	} else {
		fmt.Printf("INVALID: %s (failed basic validation)\n", entry.Path)
	}
	return nil
}

func (a *ValidateAction) String() string { return "--validate" }

// ChecksumAction re-hashes the underlying file and compares against the stored hash.
type ChecksumAction struct{}

func (a *ChecksumAction) Execute(entry *EntryInfo, ctx *FilterContext) error {
	matches, err := VerifyEntryChecksum(entry, ctx.Repository)
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
		return nil
	}
	filePath := filepath.Join(ctx.Repository, entry.Path)
	algorithm, algErr := GetHashAlgorithmByType(entry.HashType)
	if algErr == nil {
		if current, hashErr := HashFileToHexString(filePath, algorithm); hashErr == nil {
			fmt.Printf("MISMATCH: %s\n", entry.Path)
			fmt.Printf("  Stored:  %s\n", entry.HashStr)
			fmt.Printf("  Current: %s\n", current)
			return nil
		}
	}
	fmt.Printf("MISMATCH: %s\n", entry.Path)
	return nil
}

func (a *ChecksumAction) String() string { return "--checksum" }

// FixAction is a placeholder action; see dcfhfix for actual repair logic.
type FixAction struct {
	Mode string // "auto", "manual", "none"
}

func (a *FixAction) Execute(entry *EntryInfo, _ *FilterContext) error {
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

func (a *FixAction) String() string { return fmt.Sprintf("--fix %s", a.Mode) }

// FormatPermissions converts a Unix file mode to an ls-style permission string.
func FormatPermissions(mode uint32) string {
	perm := "-"
	switch mode & 0o170000 {
	case 0o040000:
		perm = "d"
	case 0o120000:
		perm = "l"
	case 0o010000:
		perm = "p"
	case 0o020000:
		perm = "c"
	case 0o060000:
		perm = "b"
	case 0o140000:
		perm = "s"
	}

	triplet := func(r, w, x uint32, special uint32, sChar, sUpperChar byte) string {
		out := make([]byte, 0, 3)
		if mode&r != 0 {
			out = append(out, 'r')
		} else {
			out = append(out, '-')
		}
		if mode&w != 0 {
			out = append(out, 'w')
		} else {
			out = append(out, '-')
		}
		switch {
		case mode&x != 0 && mode&special != 0:
			out = append(out, sChar)
		case mode&x != 0:
			out = append(out, 'x')
		case mode&special != 0:
			out = append(out, sUpperChar)
		default:
			out = append(out, '-')
		}
		return string(out)
	}
	perm += triplet(0o400, 0o200, 0o100, 0o4000, 's', 'S')
	perm += triplet(0o040, 0o020, 0o010, 0o2000, 's', 'S')
	perm += triplet(0o004, 0o002, 0o001, 0o1000, 't', 'T')
	return perm
}

// FormatFilterTime renders a wall-time value in ls-style "Jan _2 15:04" format.
func FormatFilterTime(wallTime uint64) string {
	if wallTime == 0 {
		return "Jan  1  1970"
	}
	return time.Unix(int64(wallTime>>32), 0).Format("Jan _2 15:04")
}

// FormatHashTypeName returns the canonical name for a hash type code.
func FormatHashTypeName(hashType uint16) string {
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
