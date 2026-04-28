package dircachefilehash

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FilterOptions is the flat-flag dialect shared across `dcfh status`,
// `dcfh update`, `dcfh dupes`, and `dcfhfind`. It carries all predicate
// inputs in JSON-friendly form so a CLI can populate it from pflag and a
// remote/wire server can populate it from a request payload — both then
// feed the same BuildFilter to obtain the FilterExpr that the evaluator
// runs.
//
// Within a kind, multiple values OR together (e.g. --name '*.go' --name
// '*.md' matches either). Across kinds, values AND together. The result
// is therefore a CNF tree of MinSizeTest/MaxSizeTest/SizeTest/...; nil
// is returned when no field is set, so the empty-filter fast path stays
// branchless.
//
// --valid / --corrupt are deliberately not exposed via FilterOptions in
// v1: they need the EntryInfo materialiser path and are dcfhfind-only.
type FilterOptions struct {
	// Inclusive size bounds.
	MinSize *uint64 `json:"min_size,omitempty"`
	MaxSize *uint64 `json:"max_size,omitempty"`

	// find-style strict size predicates: each "+N" / "-N" / "=N" with
	// optional binary suffix K/M/G/T/c/w/b.
	Sizes []string `json:"sizes,omitempty"`

	// Absolute date range (start-inclusive, end-exclusive).
	StartDate time.Time `json:"start_date,omitzero"`
	EndDate   time.Time `json:"end_date,omitzero"`
	TZ        string    `json:"tz,omitempty"`

	// find-style age predicates: each "+N" / "-N" / "=N".
	MTimes []string `json:"mtimes,omitempty"`
	MMins  []string `json:"mmins,omitempty"`
	CTimes []string `json:"ctimes,omitempty"`
	CMins  []string `json:"cmins,omitempty"`

	// Glob-match flags (multiple values OR together inside a kind).
	Names  []string `json:"names,omitempty"`
	INames []string `json:"inames,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	IPaths []string `json:"ipaths,omitempty"`

	// Hash predicates.
	Hashes       []string `json:"hashes,omitempty"`
	HashPrefixes []string `json:"hash_prefixes,omitempty"`
	HashTypes    []string `json:"hash_types,omitempty"`

	// Boolean predicates.
	Empty   bool `json:"empty,omitempty"`
	Deleted bool `json:"deleted,omitempty"`
}

// IsEmpty reports whether opts carries any predicate input. The wire and
// CLI layers use this to skip BuildFilter entirely for the empty case.
func (opts FilterOptions) IsEmpty() bool {
	return opts.MinSize == nil && opts.MaxSize == nil &&
		len(opts.Sizes) == 0 &&
		opts.StartDate.IsZero() && opts.EndDate.IsZero() &&
		len(opts.MTimes) == 0 && len(opts.MMins) == 0 &&
		len(opts.CTimes) == 0 && len(opts.CMins) == 0 &&
		len(opts.Names) == 0 && len(opts.INames) == 0 &&
		len(opts.Paths) == 0 && len(opts.IPaths) == 0 &&
		len(opts.Hashes) == 0 && len(opts.HashPrefixes) == 0 &&
		len(opts.HashTypes) == 0 &&
		!opts.Empty && !opts.Deleted
}

// BuildFilter assembles opts into a FilterExpr tree. Returns (nil, nil)
// when opts is empty. Date strings are parsed against opts.TZ (or the
// system local zone via ResolveZone if TZ is empty); size and age
// strings use the same dcfhfind-style "+N" / "-N" / "=N" syntax.
//
// Each appender below owns one flag-kind family and is driven by an
// inline table so adding a new sibling kind (e.g. --atime) is a single
// row, not a sixth conditional block.
func BuildFilter(opts FilterOptions) (FilterExpr, error) {
	if opts.IsEmpty() {
		return nil, nil
	}
	var conjuncts []FilterExpr
	add := func(e FilterExpr) { conjuncts = append(conjuncts, e) }

	if err := addSizeConjuncts(opts, add); err != nil {
		return nil, err
	}
	if err := addDateConjuncts(opts, add); err != nil {
		return nil, err
	}
	if err := addAgeConjuncts(opts, add); err != nil {
		return nil, err
	}
	addGlobConjuncts(opts, add)
	addHashConjuncts(opts, add)
	addBoolConjuncts(opts, add)

	return andAll(conjuncts), nil
}

// addSizeConjuncts handles --min-size / --max-size (with cross-validation
// that min ≤ max) and the repeatable find-style --size spec.
func addSizeConjuncts(opts FilterOptions, add func(FilterExpr)) error {
	if opts.MinSize != nil && opts.MaxSize != nil && *opts.MinSize > *opts.MaxSize {
		return fmt.Errorf("--min-size (%d) exceeds --max-size (%d)", *opts.MinSize, *opts.MaxSize)
	}
	if opts.MinSize != nil {
		add(&MinSizeTest{Min: *opts.MinSize})
	}
	if opts.MaxSize != nil {
		add(&MaxSizeTest{Max: *opts.MaxSize})
	}
	for _, s := range opts.Sizes {
		t, err := parseSizeTestSpec(s)
		if err != nil {
			return fmt.Errorf("--size %s: %w", s, err)
		}
		add(t)
	}
	return nil
}

// addDateConjuncts emits a single MTimeRangeTest for --start-date /
// --end-date (start-inclusive, end-exclusive) and rejects an inverted
// range up front.
func addDateConjuncts(opts FilterOptions, add func(FilterExpr)) error {
	if !opts.StartDate.IsZero() && !opts.EndDate.IsZero() && !opts.StartDate.Before(opts.EndDate) {
		return fmt.Errorf("--start-date (%s) is not before --end-date (%s)",
			opts.StartDate.Format(time.RFC3339), opts.EndDate.Format(time.RFC3339))
	}
	if !opts.StartDate.IsZero() || !opts.EndDate.IsZero() {
		add(&MTimeRangeTest{Start: opts.StartDate, End: opts.EndDate})
	}
	return nil
}

// addAgeConjuncts walks the four ±N age dialects (mtime/mmin/ctime/cmin).
// Each row pairs the flag name with its value slice and the predicate
// constructor; a new dialect is one row, not a copy-pasted loop.
func addAgeConjuncts(opts FilterOptions, add func(FilterExpr)) error {
	kinds := []struct {
		flag   string
		values []string
		mk     func(int, string) FilterExpr
	}{
		{"--mtime", opts.MTimes, func(v int, m string) FilterExpr { return &MTimeTest{Days: v, Mode: m} }},
		{"--mmin", opts.MMins, func(v int, m string) FilterExpr { return &MMinTest{Minutes: v, Mode: m} }},
		{"--ctime", opts.CTimes, func(v int, m string) FilterExpr { return &CTimeTest{Days: v, Mode: m} }},
		{"--cmin", opts.CMins, func(v int, m string) FilterExpr { return &CMinTest{Minutes: v, Mode: m} }},
	}
	for _, k := range kinds {
		for _, s := range k.values {
			v, mode, err := ParseAgeSpec(s)
			if err != nil {
				return fmt.Errorf("%s %s: %w", k.flag, s, err)
			}
			add(k.mk(v, mode))
		}
	}
	return nil
}

// addGlobConjuncts wires --name / --iname / --path / --ipath. Within
// each kind multiple values OR together (orGlobs); across kinds they
// AND via the outer conjunct list.
func addGlobConjuncts(opts FilterOptions, add func(FilterExpr)) {
	kinds := []struct {
		values   []string
		basename bool
		ci       bool
	}{
		{opts.Names, true, false},
		{opts.INames, true, true},
		{opts.Paths, false, false},
		{opts.IPaths, false, true},
	}
	for _, k := range kinds {
		if e := orGlobs(k.values, k.basename, k.ci); e != nil {
			add(e)
		}
	}
}

// addHashConjuncts wires --hash / --hash-prefix / --hash-type with the
// same OR-within / AND-across semantics as addGlobConjuncts.
func addHashConjuncts(opts FilterOptions, add func(FilterExpr)) {
	kinds := []struct {
		values []string
		mk     func(string) FilterExpr
	}{
		{opts.Hashes, func(v string) FilterExpr { return &HashTest{Hash: v} }},
		{opts.HashPrefixes, func(v string) FilterExpr { return &HashPrefixTest{Prefix: v} }},
		{opts.HashTypes, func(v string) FilterExpr { return &HashTypeTest{Type: v} }},
	}
	for _, k := range kinds {
		if e := orMap(k.values, k.mk); e != nil {
			add(e)
		}
	}
}

// addBoolConjuncts wires the on/off predicates (--empty, --deleted).
func addBoolConjuncts(opts FilterOptions, add func(FilterExpr)) {
	kinds := []struct {
		on bool
		mk func() FilterExpr
	}{
		{opts.Empty, func() FilterExpr { return &EmptyTest{} }},
		{opts.Deleted, func() FilterExpr { return &DeletedTest{} }},
	}
	for _, k := range kinds {
		if k.on {
			add(k.mk())
		}
	}
}

// andAll folds [a, b, c, …] into ((((a) AND b) AND c) …). Returns nil for
// an empty slice and the lone element for a singleton.
func andAll(es []FilterExpr) FilterExpr {
	if len(es) == 0 {
		return nil
	}
	out := es[0]
	for _, e := range es[1:] {
		out = &AndExpression{Left: out, Right: e}
	}
	return out
}

// orAll mirrors andAll for OrExpression. Used when a kind admits multiple
// values that should match by union (e.g. multiple --name globs).
func orAll(es []FilterExpr) FilterExpr {
	if len(es) == 0 {
		return nil
	}
	out := es[0]
	for _, e := range es[1:] {
		out = &OrExpression{Left: out, Right: e}
	}
	return out
}

func orGlobs(patterns []string, basename, caseInsensitive bool) FilterExpr {
	caseSensitive := !caseInsensitive
	if basename {
		return orMap(patterns, func(p string) FilterExpr {
			return &NameTest{Pattern: p, CaseSensitive: caseSensitive}
		})
	}
	return orMap(patterns, func(p string) FilterExpr {
		return &PathTest{Pattern: p, CaseSensitive: caseSensitive}
	})
}

// orMap builds an OR-tree of predicates by mapping each value through
// mk. Returns nil for an empty input. Used to fold each repeatable
// flag-kind (--name, --hash, --hash-prefix, ...) into a single
// FilterExpr that BuildFilter can AND with the others.
func orMap[T any](values []T, mk func(T) FilterExpr) FilterExpr {
	if len(values) == 0 {
		return nil
	}
	es := make([]FilterExpr, len(values))
	for i, v := range values {
		es[i] = mk(v)
	}
	return orAll(es)
}

// ParseAgeSpec parses ±N (or N) into (value, mode). Mode is "+", "-", or
// "=". Used by --mtime / --mmin / --ctime / --cmin in both the flat-flag
// dialect and dcfhfind's expression syntax.
func ParseAgeSpec(s string) (int, string, error) {
	if s == "" {
		return 0, "", fmt.Errorf("age spec is empty")
	}
	mode := "="
	digits := s
	switch s[0] {
	case '+':
		mode = "+"
		digits = s[1:]
	case '-':
		mode = "-"
		digits = s[1:]
	case '=':
		digits = s[1:]
	}
	v, err := strconv.Atoi(digits)
	if err != nil {
		return 0, "", fmt.Errorf("invalid age spec %q: %w", s, err)
	}
	if v < 0 {
		return 0, "", fmt.Errorf("age spec %q is negative", s)
	}
	return v, mode, nil
}

// parseSizeTestSpec parses ±N[K|M|G|T|c|w|b] into a *SizeTest. Mirrors
// dcfhfind's existing --size syntax so the same string works on both
// sides.
func parseSizeTestSpec(s string) (*SizeTest, error) {
	if s == "" {
		return nil, fmt.Errorf("size spec is empty")
	}
	mode := "="
	body := s
	switch s[0] {
	case '+':
		mode = "+"
		body = s[1:]
	case '-':
		mode = "-"
		body = s[1:]
	case '=':
		body = s[1:]
	}
	if body == "" {
		return nil, fmt.Errorf("size spec %q has no value", s)
	}

	mult := int64(512) // find(1) default unit is 512-byte blocks
	digits := body
	if last := body[len(body)-1]; last < '0' || last > '9' {
		switch last {
		case 'c':
			mult = 1
		case 'w':
			mult = 2
		case 'b':
			mult = 512
		case 'k', 'K':
			mult = 1024
		case 'M', 'm':
			mult = 1024 * 1024
		case 'G', 'g':
			mult = 1024 * 1024 * 1024
		case 'T', 't':
			mult = 1024 * 1024 * 1024 * 1024
		default:
			return nil, fmt.Errorf("invalid size suffix %q in %q", string(last), s)
		}
		digits = body[:len(body)-1]
	}
	if digits == "" {
		return nil, fmt.Errorf("size spec %q has no digits", s)
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return &SizeTest{Size: n * mult, Mode: mode}, nil
}

// ParseSizeBound parses N[K|M|G|T] into bytes (binary units). Unlike
// parseSizeTestSpec the surface is intentionally narrower — no signs, no
// floats — so --min-size/--max-size have one unambiguous meaning.
func ParseSizeBound(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("size bound is empty")
	}
	digits := s
	mult := uint64(1)
	switch last := s[len(s)-1]; last {
	case 'K', 'k':
		digits, mult = s[:len(s)-1], 1024
	case 'M', 'm':
		digits, mult = s[:len(s)-1], 1024*1024
	case 'G', 'g':
		digits, mult = s[:len(s)-1], 1024*1024*1024
	case 'T', 't':
		digits, mult = s[:len(s)-1], 1024*1024*1024*1024
	default:
		if last < '0' || last > '9' {
			return 0, fmt.Errorf("invalid size suffix %q; want K, M, G, or T", string(last))
		}
	}
	if digits == "" {
		return 0, fmt.Errorf("size bound %q has no digits", s)
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if mult > 1 && n > math.MaxUint64/mult {
		return 0, fmt.Errorf("size %q overflows uint64", s)
	}
	return n * mult, nil
}

// partialDateTimeRE matches: ^(YYYY)(-MM)?(-DD)?(THH)?(:MM)?(:SS)?(Z|±hh(:mm)?)?$
// HH/MM/SS are tied to the T separator so "2026-01-01:30" can't slip
// through as "2026 at ?:30".
var partialDateTimeRE = regexp.MustCompile(
	`^(\d{4})(?:-(\d{2}))?(?:-(\d{2}))?(?:T(\d{2})(?::(\d{2}))?(?::(\d{2}))?)?(Z|[+-]\d{2}(?::?\d{2})?)?$`,
)

// ParsePartialDateTime parses a partial ISO-8601 date-time. Missing
// fields default to the first instant at the given precision, so a
// bare year as --end-date excludes Jan 1 and everything after. An
// explicit Z/±offset overrides zone for that instant only.
func ParsePartialDateTime(s string, zone *time.Location) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("date is empty")
	}
	if zone == nil {
		zone = time.UTC
	}
	m := partialDateTimeRE.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, fmt.Errorf("invalid date %q; want YYYY[-MM[-DD[THH[:MM[:SS]]]]][Z|±hh[:mm]]", s)
	}

	field := func(idx, defaultVal, min, max int, name string) (int, error) {
		if m[idx] == "" {
			return defaultVal, nil
		}
		v, _ := strconv.Atoi(m[idx])
		if v < min || v > max {
			return 0, fmt.Errorf("invalid %s in %q", name, s)
		}
		return v, nil
	}

	year, _ := strconv.Atoi(m[1])
	month, err := field(2, 1, 1, 12, "month")
	if err != nil {
		return time.Time{}, err
	}
	day, err := field(3, 1, 1, 31, "day")
	if err != nil {
		return time.Time{}, err
	}
	hour, err := field(4, 0, 0, 23, "hour")
	if err != nil {
		return time.Time{}, err
	}
	minute, err := field(5, 0, 0, 59, "minute")
	if err != nil {
		return time.Time{}, err
	}
	sec, err := field(6, 0, 0, 59, "second")
	if err != nil {
		return time.Time{}, err
	}

	loc := zone
	if m[7] != "" {
		off, err := parseOffset(m[7])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid offset in %q: %w", s, err)
		}
		loc = off
	}

	// time.Date silently rolls 2026-02-30 into March; a probe catches it.
	probe := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if probe.Year() != year || probe.Month() != time.Month(month) || probe.Day() != day {
		return time.Time{}, fmt.Errorf("invalid date %q", s)
	}

	return time.Date(year, time.Month(month), day, hour, minute, sec, 0, loc), nil
}

func parseOffset(s string) (*time.Location, error) {
	if s == "Z" {
		return time.UTC, nil
	}
	sign := 1
	if s[0] == '-' {
		sign = -1
	}
	body := s[1:]
	var hh, mm int
	var err error
	switch len(body) {
	case 2:
		hh, err = strconv.Atoi(body)
	case 4:
		hh, err = strconv.Atoi(body[:2])
		if err == nil {
			mm, err = strconv.Atoi(body[2:])
		}
	case 5:
		if body[2] != ':' {
			return nil, fmt.Errorf("malformed offset %q", s)
		}
		hh, err = strconv.Atoi(body[:2])
		if err == nil {
			mm, err = strconv.Atoi(body[3:])
		}
	default:
		return nil, fmt.Errorf("malformed offset %q", s)
	}
	if err != nil {
		return nil, err
	}
	if hh > 23 || mm > 59 {
		return nil, fmt.Errorf("offset out of range in %q", s)
	}
	secs := sign * (hh*3600 + mm*60)
	return time.FixedZone(s, secs), nil
}

// ResolveZone returns the IANA location named by flag, or time.Local
// (which honours $TZ) when flag is empty. Unknown zone names are
// fatal — we never silently fall back to UTC.
func ResolveZone(flag string) (*time.Location, error) {
	if flag == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(flag)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", flag, err)
	}
	return loc, nil
}

// ResolveDates is a convenience for callers that have separate
// start/end strings plus a tz string and want them parsed in one shot.
// Returns zero times for empty inputs.
func ResolveDates(start, end, tz string) (startT, endT time.Time, err error) {
	if start == "" && end == "" {
		return time.Time{}, time.Time{}, nil
	}
	zone, err := ResolveZone(tz)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if start != "" {
		startT, err = ParsePartialDateTime(start, zone)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--start-date: %w", err)
		}
	}
	if end != "" {
		endT, err = ParsePartialDateTime(end, zone)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--end-date: %w", err)
		}
	}
	return startT, endT, nil
}

// SizeBoundString renders u as a human-readable size with binary suffix,
// matching the syntax accepted by ParseSizeBound. Used by error messages
// and the FilterOptions String form below.
func SizeBoundString(u uint64) string {
	const k = uint64(1024)
	switch {
	case u >= k*k*k*k && u%(k*k*k*k) == 0:
		return fmt.Sprintf("%dT", u/(k*k*k*k))
	case u >= k*k*k && u%(k*k*k) == 0:
		return fmt.Sprintf("%dG", u/(k*k*k))
	case u >= k*k && u%(k*k) == 0:
		return fmt.Sprintf("%dM", u/(k*k))
	case u >= k && u%k == 0:
		return fmt.Sprintf("%dK", u/k)
	default:
		return strconv.FormatUint(u, 10)
	}
}

// String renders opts as the equivalent dcfhfind expression — useful for
// error messages, JSON dumps, and audit logs.
func (opts FilterOptions) String() string {
	var parts []string
	if opts.MinSize != nil {
		parts = append(parts, "--min-size "+SizeBoundString(*opts.MinSize))
	}
	if opts.MaxSize != nil {
		parts = append(parts, "--max-size "+SizeBoundString(*opts.MaxSize))
	}
	for _, s := range opts.Sizes {
		parts = append(parts, "--size "+s)
	}
	if !opts.StartDate.IsZero() {
		parts = append(parts, "--start-date "+opts.StartDate.Format(time.RFC3339))
	}
	if !opts.EndDate.IsZero() {
		parts = append(parts, "--end-date "+opts.EndDate.Format(time.RFC3339))
	}
	for _, s := range opts.MTimes {
		parts = append(parts, "--mtime "+s)
	}
	for _, s := range opts.MMins {
		parts = append(parts, "--mmin "+s)
	}
	for _, s := range opts.CTimes {
		parts = append(parts, "--ctime "+s)
	}
	for _, s := range opts.CMins {
		parts = append(parts, "--cmin "+s)
	}
	for _, s := range opts.Names {
		parts = append(parts, "--name "+s)
	}
	for _, s := range opts.INames {
		parts = append(parts, "--iname "+s)
	}
	for _, s := range opts.Paths {
		parts = append(parts, "--path "+s)
	}
	for _, s := range opts.IPaths {
		parts = append(parts, "--ipath "+s)
	}
	for _, s := range opts.Hashes {
		parts = append(parts, "--hash "+s)
	}
	for _, s := range opts.HashPrefixes {
		parts = append(parts, "--hash-prefix "+s)
	}
	for _, s := range opts.HashTypes {
		parts = append(parts, "--hash-type "+s)
	}
	if opts.Empty {
		parts = append(parts, "--empty")
	}
	if opts.Deleted {
		parts = append(parts, "--deleted")
	}
	return strings.Join(parts, " ")
}
