package dircachefilehash

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// FilterEntry is the read-only entry view that predicates and actions
// consult. The method set is a strict subset of BinaryEntryInterface, so
// every BinaryEntryInterface value (mmap-backed, scan, IO) satisfies
// FilterEntry directly. Two thin adapters cover the other producers:
// *EntryInfo (dcfhfind path) via entryInfoAdapter, and *binaryEntry
// (dupes hot loop) via binaryEntryAdapter — both are stack-allocated
// one-pointer wrappers, no heap traffic.
type FilterEntry interface {
	RelativePath() (string, error)
	FileSize() (int64, error)
	Mode() (uint32, error)
	UID() (uint32, error)
	GID() (uint32, error)
	Dev() (uint64, error)
	MTimeWall() (uint64, error)
	CTimeWall() (uint64, error)
	HashType() (uint16, error)
	HashString() (string, error)
	IsDeleted() (bool, error)
}

// FilterExpr is a predicate node in a dcfhfind-style expression tree.
// Implementations cover leaf tests (NameTest, SizeTest, …) and logical
// operators (AndExpression, OrExpression, NotExpression).
type FilterExpr interface {
	Evaluate(entry FilterEntry, ctx *FilterContext) (bool, error)
	String() string
}

// FilterAction is an action node executed on every matching entry (PrintAction,
// LsAction, PrintfAction, ValidateAction, ChecksumAction, FixAction).
type FilterAction interface {
	Execute(entry FilterEntry, ctx *FilterContext) error
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

// materialiseEntryInfo builds a transient *EntryInfo from a FilterEntry,
// for the dcfhfind-only helpers (ValidateEntryInfo, DetectEntryCorruption,
// VerifyEntryChecksum) that haven't been ported to FilterEntry. Dormant
// on the dupes / status / update hot paths.
func materialiseEntryInfo(e FilterEntry) (*EntryInfo, error) {
	var err error
	info := &EntryInfo{
		Path:      take(&err, e.RelativePath),
		IsDeleted: take(&err, e.IsDeleted),
		FileSize:  take(&err, e.FileSize),
		Mode:      take(&err, e.Mode),
		UID:       take(&err, e.UID),
		GID:       take(&err, e.GID),
		Dev:       take(&err, e.Dev),
		MTimeWall: take(&err, e.MTimeWall),
		CTimeWall: take(&err, e.CTimeWall),
		HashStr:   take(&err, e.HashString),
		HashType:  take(&err, e.HashType),
	}
	if err != nil {
		return nil, err
	}
	return info, nil
}

// take invokes get and threads any error through sticky. Once sticky is
// non-nil, subsequent calls return the zero value without invoking get.
func take[T any](sticky *error, get func() (T, error)) T {
	if *sticky != nil {
		var zero T
		return zero
	}
	v, err := get()
	if err != nil {
		*sticky = err
	}
	return v
}

// binaryEntryAdapter wraps a raw *binaryEntry to satisfy FilterEntry. Used
// by the dupes hot loop (skiplist.ForEach yields *binaryEntry, not
// BinaryEntryInterface) and by callers that already hold a *binaryEntry
// and don't want to detour through a heavier wrapper. *binaryEntry's own
// methods don't return errors; the adapter just lifts return shapes.
type binaryEntryAdapter struct{ e *binaryEntry }

// asFilterEntry wraps be for predicate evaluation. It is a free function (not a
// method) because binaryEntry is an alias for the out-of-package format.Entry.
func asFilterEntry(be *binaryEntry) FilterEntry { return binaryEntryAdapter{be} }

func (a binaryEntryAdapter) RelativePath() (string, error) { return a.e.RelativePath(), nil }
func (a binaryEntryAdapter) FileSize() (int64, error)      { return a.e.FileSize, nil }
func (a binaryEntryAdapter) Mode() (uint32, error)         { return a.e.Mode, nil }
func (a binaryEntryAdapter) UID() (uint32, error)          { return a.e.UID, nil }
func (a binaryEntryAdapter) GID() (uint32, error)          { return a.e.GID, nil }
func (a binaryEntryAdapter) Dev() (uint64, error)          { return a.e.Dev, nil }
func (a binaryEntryAdapter) MTimeWall() (uint64, error)    { return a.e.MTimeWall, nil }
func (a binaryEntryAdapter) CTimeWall() (uint64, error)    { return a.e.CTimeWall, nil }
func (a binaryEntryAdapter) HashType() (uint16, error)     { return a.e.HashType, nil }
func (a binaryEntryAdapter) HashString() (string, error)   { return a.e.HashString(), nil }
func (a binaryEntryAdapter) IsDeleted() (bool, error)      { return a.e.IsDeleted(), nil }

// NameTest matches the entry's basename against a gitignore-style
// pattern. The same syntax accepted by `.dcfh/ignore` works here too.
//
// Pat is the compiled matcher; Raw is preserved for String() and for
// shipping the pattern over the wire. CaseSensitive=false lower-cases
// both sides at compile/match time — an approximation that's adequate
// for `--iname` since gitignore character classes (`[A-Z]`) lose their
// case semantics under lower-casing but are rare in CLI use.
type NameTest struct {
	Pat           gitignore.Pattern
	Raw           string
	CaseSensitive bool
}

// NewNameTest compiles pattern as a gitignore matcher and returns a
// NameTest. Returns an error if the pattern is empty or a comment;
// today the gitignore parser accepts every other input string, but the
// signature reserves room for future validation.
func NewNameTest(pattern string, caseSensitive bool) (*NameTest, error) {
	src := pattern
	if !caseSensitive {
		src = strings.ToLower(src)
	}
	pat, err := CompileIgnorePattern(src)
	if err != nil {
		return nil, err
	}
	if pat == nil {
		return nil, fmt.Errorf("empty pattern")
	}
	return &NameTest{Pat: pat, Raw: pattern, CaseSensitive: caseSensitive}, nil
}

// MustNewNameTest panics on compile error. For tests and other places
// where the pattern is a constant.
func MustNewNameTest(pattern string, caseSensitive bool) *NameTest {
	t, err := NewNameTest(pattern, caseSensitive)
	if err != nil {
		panic(err)
	}
	return t
}

func (t *NameTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	path, err := entry.RelativePath()
	if err != nil {
		return false, err
	}
	base := filepath.Base(path)
	if !t.CaseSensitive {
		base = strings.ToLower(base)
	}
	return t.Pat.Match([]string{base}, false) == gitignore.Exclude, nil
}

func (t *NameTest) String() string {
	if t.CaseSensitive {
		return fmt.Sprintf("--name %s", t.Raw)
	}
	return fmt.Sprintf("--iname %s", t.Raw)
}

// PathTest matches the entry's full forward-slash path against a
// gitignore-style pattern. Patterns containing `/` are anchored
// according to gitignore rules (leading `/` = repo root); patterns
// without `/` match any path component.
type PathTest struct {
	Pat           gitignore.Pattern
	Raw           string
	CaseSensitive bool
}

// NewPathTest compiles pattern as a gitignore matcher and returns a
// PathTest.
func NewPathTest(pattern string, caseSensitive bool) (*PathTest, error) {
	src := pattern
	if !caseSensitive {
		src = strings.ToLower(src)
	}
	pat, err := CompileIgnorePattern(src)
	if err != nil {
		return nil, err
	}
	if pat == nil {
		return nil, fmt.Errorf("empty pattern")
	}
	return &PathTest{Pat: pat, Raw: pattern, CaseSensitive: caseSensitive}, nil
}

// MustNewPathTest panics on compile error.
func MustNewPathTest(pattern string, caseSensitive bool) *PathTest {
	t, err := NewPathTest(pattern, caseSensitive)
	if err != nil {
		panic(err)
	}
	return t
}

func (t *PathTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	path, err := entry.RelativePath()
	if err != nil {
		return false, err
	}
	if !t.CaseSensitive {
		path = strings.ToLower(path)
	}
	segs := splitForGitignore(path)
	if len(segs) == 0 {
		return false, nil
	}
	return t.Pat.Match(segs, false) == gitignore.Exclude, nil
}

func (t *PathTest) String() string {
	if t.CaseSensitive {
		return fmt.Sprintf("--path %s", t.Raw)
	}
	return fmt.Sprintf("--ipath %s", t.Raw)
}

// SizeTest compares file size.
type SizeTest struct {
	Size int64
	Mode string // "=", "+", "-"
}

func (t *SizeTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	fs, err := entry.FileSize()
	if err != nil {
		return false, err
	}
	// fs is a validated non-negative size (os.FileInfo.Size() or a legacy
	// uint64 < 2^63; the recovery validator rejects negatives upstream), so the
	// signed comparison below carries no sign-flip hazard. SizeTest.Size is int64.
	switch t.Mode {
	case "+":
		return fs > t.Size, nil
	case "-":
		return fs < t.Size, nil
	case "=":
		return fs == t.Size, nil
	default:
		return false, fmt.Errorf("invalid size mode: %s", t.Mode)
	}
}

func (t *SizeTest) String() string {
	return fmt.Sprintf("--size %s%d", t.Mode, t.Size)
}

// MinSizeTest matches files with FileSize >= Min (inclusive).
// Companion to SizeTest — the latter is strict find-style (>, <, =), this
// is the inclusive form used by the flat --min-size flag.
type MinSizeTest struct {
	Min int64
}

func (t *MinSizeTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	fs, err := entry.FileSize()
	if err != nil {
		return false, err
	}
	// fs is a validated non-negative size (see SizeTest.Evaluate).
	return fs >= t.Min, nil
}

func (t *MinSizeTest) String() string { return fmt.Sprintf("--min-size %d", t.Min) }

// MaxSizeTest matches files with FileSize <= Max (inclusive).
type MaxSizeTest struct {
	Max int64
}

func (t *MaxSizeTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	fs, err := entry.FileSize()
	if err != nil {
		return false, err
	}
	// fs is a validated non-negative size (see SizeTest.Evaluate).
	return fs <= t.Max, nil
}

func (t *MaxSizeTest) String() string { return fmt.Sprintf("--max-size %d", t.Max) }

// MTimeRangeTest matches files whose mtime falls in [Start, End). A zero
// Start means "no lower bound"; a zero End means "no upper bound". Used
// by the flat --start-date / --end-date flags; complements MTimeTest's
// days-relative semantics.
type MTimeRangeTest struct {
	Start time.Time
	End   time.Time
}

func (t *MTimeRangeTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	mt, err := entry.MTimeWall()
	if err != nil {
		return false, err
	}
	when := TimeFromWall(mt)
	if !t.Start.IsZero() && when.Before(t.Start) {
		return false, nil
	}
	if !t.End.IsZero() && !when.Before(t.End) {
		return false, nil
	}
	return true, nil
}

func (t *MTimeRangeTest) String() string {
	switch {
	case t.Start.IsZero():
		return fmt.Sprintf("--end-date %s", t.End.Format(time.RFC3339))
	case t.End.IsZero():
		return fmt.Sprintf("--start-date %s", t.Start.Format(time.RFC3339))
	default:
		return fmt.Sprintf("--start-date %s --end-date %s",
			t.Start.Format(time.RFC3339), t.End.Format(time.RFC3339))
	}
}

// EmptyTest matches zero-size files.
type EmptyTest struct{}

func (t *EmptyTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	fs, err := entry.FileSize()
	if err != nil {
		return false, err
	}
	return fs == 0, nil
}

func (t *EmptyTest) String() string { return "--empty" }

// DeletedTest matches entries flagged as deleted.
type DeletedTest struct{}

func (t *DeletedTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	return entry.IsDeleted()
}

func (t *DeletedTest) String() string { return "--deleted" }

// ValidTest matches entries that pass ValidateEntryInfo.
type ValidTest struct{}

func (t *ValidTest) Evaluate(entry FilterEntry, ctx *FilterContext) (bool, error) {
	info, err := materialiseEntryInfo(entry)
	if err != nil {
		return false, err
	}
	return ValidateEntryInfo(info, ctx.Repository)
}

func (t *ValidTest) String() string { return "--valid" }

// CorruptTest matches entries that DetectEntryCorruption flags.
type CorruptTest struct{}

func (t *CorruptTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	info, err := materialiseEntryInfo(entry)
	if err != nil {
		return false, err
	}
	corrupt, _ := DetectEntryCorruption(info)
	return corrupt, nil
}

func (t *CorruptTest) String() string { return "--corrupt" }

// HashTest matches an exact hash value (case-insensitive).
type HashTest struct {
	Hash string
}

func (t *HashTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	hs, err := entry.HashString()
	if err != nil {
		return false, err
	}
	return strings.EqualFold(hs, t.Hash), nil
}

func (t *HashTest) String() string { return fmt.Sprintf("--hash %s", t.Hash) }

// HashPrefixTest matches a hash prefix (case-insensitive).
type HashPrefixTest struct {
	Prefix string
}

func (t *HashPrefixTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	hs, err := entry.HashString()
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.ToLower(hs), strings.ToLower(t.Prefix)), nil
}

func (t *HashPrefixTest) String() string { return fmt.Sprintf("--hash-prefix %s", t.Prefix) }

// HashTypeTest matches the hash algorithm name (SHA1/SHA256/SHA512).
type HashTypeTest struct {
	Type string
}

func (t *HashTypeTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	ht, err := entry.HashType()
	if err != nil {
		return false, err
	}
	var name string
	switch ht {
	case 1:
		name = "SHA1"
	case 2:
		name = "SHA256"
	case 3:
		name = "SHA512"
	default:
		name = fmt.Sprintf("UNKNOWN(%d)", ht)
	}
	return strings.EqualFold(name, t.Type), nil
}

func (t *HashTypeTest) String() string { return fmt.Sprintf("--hash-type %s", t.Type) }

// MTimeTest / MMinTest / CTimeTest / CMinTest — age-based tests.
type MTimeTest struct {
	Days int
	Mode string
}

func (t *MTimeTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	mt, err := entry.MTimeWall()
	if err != nil {
		return false, err
	}
	age := time.Since(TimeFromWall(mt))
	return compareAge(int(age.Hours()/24), t.Days, t.Mode, "mtime")
}

func (t *MTimeTest) String() string { return fmt.Sprintf("--mtime %s%d", t.Mode, t.Days) }

type MMinTest struct {
	Minutes int
	Mode    string
}

func (t *MMinTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	mt, err := entry.MTimeWall()
	if err != nil {
		return false, err
	}
	age := time.Since(TimeFromWall(mt))
	return compareAge(int(age.Minutes()), t.Minutes, t.Mode, "mmin")
}

func (t *MMinTest) String() string { return fmt.Sprintf("--mmin %s%d", t.Mode, t.Minutes) }

type CTimeTest struct {
	Days int
	Mode string
}

func (t *CTimeTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	ct, err := entry.CTimeWall()
	if err != nil {
		return false, err
	}
	age := time.Since(TimeFromWall(ct))
	return compareAge(int(age.Hours()/24), t.Days, t.Mode, "ctime")
}

func (t *CTimeTest) String() string { return fmt.Sprintf("--ctime %s%d", t.Mode, t.Days) }

type CMinTest struct {
	Minutes int
	Mode    string
}

func (t *CMinTest) Evaluate(entry FilterEntry, _ *FilterContext) (bool, error) {
	ct, err := entry.CTimeWall()
	if err != nil {
		return false, err
	}
	age := time.Since(TimeFromWall(ct))
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

func (e *AndExpression) Evaluate(entry FilterEntry, ctx *FilterContext) (bool, error) {
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

func (e *OrExpression) Evaluate(entry FilterEntry, ctx *FilterContext) (bool, error) {
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

func (e *NotExpression) Evaluate(entry FilterEntry, ctx *FilterContext) (bool, error) {
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

func (a *PrintAction) Execute(entry FilterEntry, _ *FilterContext) error {
	path, err := entry.RelativePath()
	if err != nil {
		return err
	}
	_, err = fmt.Println(path)
	return err
}

func (a *PrintAction) String() string { return "--print" }

// Print0Action prints the entry's path null-terminated.
type Print0Action struct{}

func (a *Print0Action) Execute(entry FilterEntry, _ *FilterContext) error {
	path, err := entry.RelativePath()
	if err != nil {
		return err
	}
	_, err = fmt.Printf("%s\x00", path)
	return err
}

func (a *Print0Action) String() string { return "--print0" }

// LsAction prints a detailed ls-style listing.
type LsAction struct{}

func (a *LsAction) Execute(entry FilterEntry, ctx *FilterContext) error {
	info, err := materialiseEntryInfo(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Printf("%s %d %d %d %8d %s [%s] %s\n",
		FormatPermissions(info.Mode), 1, info.UID, info.GID, info.FileSize,
		FormatFilterTime(info.MTimeWall), ctx.IndexType, info.Path)
	return err
}

func (a *LsAction) String() string { return "--ls" }

// PrintfAction formats each entry using a find(1)-style format string.
type PrintfAction struct {
	Format string
}

func (a *PrintfAction) Execute(entry FilterEntry, ctx *FilterContext) error {
	info, err := materialiseEntryInfo(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Print(a.format(info, ctx))
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

func (a *ValidateAction) Execute(entry FilterEntry, ctx *FilterContext) error {
	info, err := materialiseEntryInfo(entry)
	if err != nil {
		return err
	}
	valid, err := ValidateEntryInfo(info, ctx.Repository)
	if err != nil {
		fmt.Printf("ERROR: %s - validation failed: %v\n", info.Path, err)
		return nil
	}
	if valid {
		fmt.Printf("VALID: %s\n", info.Path)
		return nil
	}
	corrupt, issues := DetectEntryCorruption(info)
	if corrupt {
		fmt.Printf("INVALID: %s\n", info.Path)
		for _, issue := range issues {
			fmt.Printf("  Issue: %s\n", issue)
		}
	} else {
		fmt.Printf("INVALID: %s (failed basic validation)\n", info.Path)
	}
	return nil
}

func (a *ValidateAction) String() string { return "--validate" }

// ChecksumAction re-hashes the underlying file and compares against the stored hash.
type ChecksumAction struct{}

func (a *ChecksumAction) Execute(entry FilterEntry, ctx *FilterContext) error {
	info, err := materialiseEntryInfo(entry)
	if err != nil {
		return err
	}
	matches, err := VerifyEntryChecksum(info, ctx.Repository)
	if err != nil {
		if strings.Contains(err.Error(), "file does not exist") {
			fmt.Printf("MISSING: %s\n", info.Path)
		} else {
			fmt.Printf("ERROR: %s - %v\n", info.Path, err)
		}
		return nil
	}
	if matches {
		fmt.Printf("OK: %s\n", info.Path)
		return nil
	}
	filePath := filepath.Join(ctx.Repository, info.Path)
	algorithm, algErr := GetHashAlgorithmByType(info.HashType)
	if algErr == nil {
		if current, hashErr := HashFileToHexString(filePath, algorithm); hashErr == nil {
			fmt.Printf("MISMATCH: %s\n", info.Path)
			fmt.Printf("  Stored:  %s\n", info.HashStr)
			fmt.Printf("  Current: %s\n", current)
			return nil
		}
	}
	fmt.Printf("MISMATCH: %s\n", info.Path)
	return nil
}

func (a *ChecksumAction) String() string { return "--checksum" }

// FixAction is a placeholder action; see dcfhfix for actual repair logic.
type FixAction struct {
	Mode string // "auto", "manual", "none"
}

func (a *FixAction) Execute(entry FilterEntry, _ *FilterContext) error {
	path, err := entry.RelativePath()
	if err != nil {
		return err
	}
	switch a.Mode {
	case "auto":
		fmt.Printf("AUTO-FIX: %s (would apply automatic fixes)\n", path)
	case "manual":
		fmt.Printf("MANUAL-FIX: %s (would prompt for manual fixes)\n", path)
	case "none":
		fmt.Printf("NO-FIX: %s (validation only, no fixes applied)\n", path)
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
