package main

import (
	"fmt"
	"slices"

	"github.com/spf13/pflag"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// Command-name labels used by the cmdFlagRegistry to scope flag
// groups. Pass these (rather than raw strings) into resolveScopes /
// RegisterCmdFlags — a typo'd registry entry silently skips its
// flag group otherwise.
const (
	cmdStatus = "status"
	cmdUpdate = "update"
	cmdDupes  = "dupes"
)

// filterFlagsState is the per-segment state populated by the registry
// in RegisterCmdFlags. String fields hold raw flag values; size and
// date strings parse into FilterOptions only when BuildFilterOptions
// runs (so error messages reach the user via cobra's RunE rather
// than init()).
type filterFlagsState struct {
	// Shared filter dialect — every command, every segment.
	minSize      string
	maxSize      string
	sizes        []string
	startDate    string
	endDate      string
	tz           string
	mtimes       []string
	mmins        []string
	ctimes       []string
	cmins        []string
	names        []string
	inames       []string
	paths        []string
	ipaths       []string
	hashes       []string
	hashPrefixes []string
	hashTypes    []string
	empty        bool
	deleted      bool

	// Dupes segment-zero-only state. Meaningless inside an explicit
	// --print / --ignore segment (these are command-level toggles).
	exclusive       yesNoFlag
	ignoreHardlinks bool
	fsDedupe        bool
}

func newFilterFlagsState() *filterFlagsState {
	return &filterFlagsState{exclusive: yesNoFlag(true)}
}

// cmdFlagGroup is one entry in the scope-marker command-flag registry.
// commands is the allow-list of commands; empty == universal.
// perSegment, when true, registers the group in every --print /
// --ignore segment as well as segment zero. False == segment zero only.
type cmdFlagGroup struct {
	register   func(fs *pflag.FlagSet, state *filterFlagsState)
	commands   []string
	perSegment bool
}

// cmdFlagRegistry is the single source of truth for which flags each
// scope-marker command accepts. A new command-specific flag is one
// new entry here plus the matching field on filterFlagsState.
var cmdFlagRegistry = []cmdFlagGroup{
	{
		register:   func(fs *pflag.FlagSet, _ *filterFlagsState) { registerRootPersistentFlags(fs) },
		perSegment: true,
	},
	{
		register:   RegisterFilterFlags,
		perSegment: true,
	},
	{
		commands: []string{cmdDupes},
		register: func(fs *pflag.FlagSet, state *filterFlagsState) {
			fs.Var(&state.exclusive, flagExclusive,
				"restrict results to groups fully inside the given paths (yes|no, default yes)")
			fs.BoolVarP(&state.ignoreHardlinks, flagIgnoreHardlinks, "H", false,
				"collapse hardlinks to the same inode to one entry per group")
			fs.BoolVar(&state.fsDedupe, flagFSDedupe, false,
				"reclaim disk blocks from duplicates via FIDEDUPERANGE (Linux only; combine with --dry-run to see the plan without changing anything)")
		},
	},
}

// registerSegmentZeroFlags installs every cmdFlagRegistry entry
// applicable to the named command on fs — filter dialect, root
// persistent dialect, and command-specific toggles.
//
// Persistent flags are re-registered against the same package-level
// vars cobra normally drives, since DisableFlagParsing=true on these
// commands bypasses cobra's pre-RunE flag parse.
func registerSegmentZeroFlags(fs *pflag.FlagSet, state *filterFlagsState, command string) {
	for _, group := range cmdFlagRegistry {
		if len(group.commands) > 0 && !slices.Contains(group.commands, command) {
			continue
		}
		group.register(fs, state)
	}
}

// registerTailSegmentFlags installs only the per-segment groups
// (filter + persistent dialect). Command-specific toggles are
// rejected as unknown flags inside an explicit --print / --ignore
// segment.
func registerTailSegmentFlags(fs *pflag.FlagSet, state *filterFlagsState) {
	for _, group := range cmdFlagRegistry {
		if !group.perSegment {
			continue
		}
		group.register(fs, state)
	}
}

// RegisterFilterFlags registers the shared flat-flag dialect (see
// dcfh.FilterOptions) on fs and stores values in state.
func RegisterFilterFlags(fs *pflag.FlagSet, state *filterFlagsState) {
	fs.StringVar(&state.minSize, "min-size", "",
		"minimum file size (inclusive); binary suffixes K/M/G/T (e.g. 1K=1024)")
	fs.StringVar(&state.maxSize, "max-size", "",
		"maximum file size (inclusive); binary suffixes K/M/G/T")
	fs.StringSliceVar(&state.sizes, "size", nil,
		"strict size predicate ±N[K|M|G|T|c|w|b]; repeatable; find(1) syntax")
	fs.StringVar(&state.startDate, "start-date", "",
		"minimum mtime (inclusive); partial ISO-8601, e.g. 2026 or 2026-01-01T00")
	fs.StringVar(&state.endDate, "end-date", "",
		"maximum mtime (exclusive); partial ISO-8601")
	fs.StringVar(&state.tz, "tz", "",
		"IANA timezone for bare date-times (default: $TZ or system local)")
	fs.StringSliceVar(&state.mtimes, "mtime", nil,
		"file age in days ±N; repeatable; find(1) syntax")
	fs.StringSliceVar(&state.mmins, "mmin", nil,
		"file age in minutes ±N; repeatable")
	fs.StringSliceVar(&state.ctimes, "ctime", nil,
		"file ctime in days ±N; repeatable")
	fs.StringSliceVar(&state.cmins, "cmin", nil,
		"file ctime in minutes ±N; repeatable")
	fs.StringSliceVar(&state.names, "name", nil,
		"basename pattern (gitignore syntax — same as .dcfh/ignore lines); repeatable (OR'd within kind)")
	fs.StringSliceVar(&state.inames, "iname", nil,
		"basename pattern, case-insensitive; repeatable")
	fs.StringSliceVar(&state.paths, "path", nil,
		"full-path pattern (gitignore syntax); repeatable. Distinct from positional path-prefix args.")
	fs.StringSliceVar(&state.ipaths, "ipath", nil,
		"full-path pattern, case-insensitive; repeatable")
	fs.StringSliceVar(&state.hashes, "hash", nil,
		"exact hash (hex); repeatable")
	fs.StringSliceVar(&state.hashPrefixes, "hash-prefix", nil,
		"hash prefix (hex); repeatable")
	fs.StringSliceVar(&state.hashTypes, "hash-type", nil,
		"hash algorithm: SHA1, SHA256, or SHA512; repeatable")
	fs.BoolVar(&state.empty, "empty", false,
		"match zero-size files only")
	fs.BoolVar(&state.deleted, "deleted", false,
		"match tombstoned (deleted) entries only")
}

// BuildFilterOptions translates the registered flag state into a
// FilterOptions ready for dcfh.BuildFilter. Returns an empty
// FilterOptions when nothing is set (so callers can pass it through to a
// request struct unconditionally).
func BuildFilterOptions(state *filterFlagsState) (dcfh.FilterOptions, error) {
	opts := dcfh.FilterOptions{
		TZ:           state.tz,
		Sizes:        state.sizes,
		MTimes:       state.mtimes,
		MMins:        state.mmins,
		CTimes:       state.ctimes,
		CMins:        state.cmins,
		Names:        state.names,
		INames:       state.inames,
		Paths:        state.paths,
		IPaths:       state.ipaths,
		Hashes:       state.hashes,
		HashPrefixes: state.hashPrefixes,
		HashTypes:    state.hashTypes,
		Empty:        state.empty,
		Deleted:      state.deleted,
	}
	if state.minSize != "" {
		n, err := dcfh.ParseSizeBound(state.minSize)
		if err != nil {
			return opts, fmt.Errorf("--min-size: %w", err)
		}
		opts.MinSize = &n
	}
	if state.maxSize != "" {
		n, err := dcfh.ParseSizeBound(state.maxSize)
		if err != nil {
			return opts, fmt.Errorf("--max-size: %w", err)
		}
		opts.MaxSize = &n
	}
	if state.startDate != "" || state.endDate != "" {
		startT, endT, err := dcfh.ResolveDates(state.startDate, state.endDate, state.tz)
		if err != nil {
			return opts, err
		}
		opts.StartDate, opts.EndDate = startT, endT
	}
	return opts, nil
}
