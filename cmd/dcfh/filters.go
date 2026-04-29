package main

import (
	"fmt"
	"slices"

	"github.com/spf13/pflag"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// filterFlagsState is the per-segment state populated by the registry
// in RegisterCmdFlags. The string fields hold raw flag values; size
// and date strings are parsed into FilterOptions only when
// BuildFilterOptions runs (so error messages reach the user via
// cobra's RunE rather than init()).
//
// Command-specific (non-filter) fields live alongside the filter
// dialect because they all share one cobra-disabled, scope-marker
// argv parse. Each per-command registration in RegisterCmdFlags
// declares which subset of fields the command's segment-zero parser
// touches; segments 1+ only ever see the shared filter dialect via
// RegisterFilterFlags.
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

	// Dupes-only command flags (registered via RegisterCmdFlags("dupes")
	// on the segment-zero parser only — meaningless inside an explicit
	// --print / --ignore segment).
	exclusive       yesNoFlag
	ignoreHardlinks bool
	fsDedupe        bool
}

// newFilterFlagsState returns a filterFlagsState seeded with defaults
// for non-zero-value command-specific fields (today: --exclusive=yes
// for dupes). Filter-dialect fields are zero-value by design.
func newFilterFlagsState() *filterFlagsState {
	return &filterFlagsState{exclusive: yesNoFlag(true)}
}

// cmdFlagGroup is one entry in the scope-marker command-flag registry.
// commands is the set of commands the group registers for; an empty
// list means "all scope-marker commands". perSegment, when true,
// registers the group on every --print / --ignore segment as well as
// segment zero — used for the root persistent dialect and the shared
// filter dialect, both of which users expect to work anywhere on the
// command line. Command-specific flags (e.g. dupes' --fs-dedupe) are
// segment-zero only: they're command-level toggles, not per-segment
// filters.
type cmdFlagGroup struct {
	register   func(fs *pflag.FlagSet, state *filterFlagsState)
	commands   []string
	perSegment bool
}

// cmdFlagRegistry is the single source of truth for which flags each
// scope-marker command accepts. parseSegmentZero / parseSegment walk
// this list in registration order. A new command-specific flag is one
// new entry here plus the matching field on filterFlagsState; no
// other call site has to learn about it.
//
// Order matters only for help text (the segment-zero --help would
// list flags in this order); behaviour is independent of order
// because the package-var globals are write-only from these calls.
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
		commands: []string{"dupes"},
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

// RegisterCmdFlags installs every flag valid for the named command +
// segment-position on fs. firstSegment selects between the segment-
// zero parser (where command-level toggles like --fs-dedupe are
// allowed) and the tail-segment parser used by explicit --print /
// --ignore groups (filter dialect + persistent flags only).
//
// Persistent flags are re-registered against the same package-level
// vars cobra normally drives — necessary because scope-marker
// commands set DisableFlagParsing=true, bypassing cobra's pre-RunE
// flag parse. Last-write-wins for repeated tokens, matching every
// other CLI in this codebase.
func RegisterCmdFlags(fs *pflag.FlagSet, state *filterFlagsState, command string, firstSegment bool) {
	for _, group := range cmdFlagRegistry {
		if !firstSegment && !group.perSegment {
			continue
		}
		if len(group.commands) > 0 && !slices.Contains(group.commands, command) {
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
