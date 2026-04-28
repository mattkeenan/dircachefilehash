package main

import (
	"fmt"

	"github.com/spf13/pflag"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// filterFlagsState is the per-command state populated by
// RegisterFilterFlags. The string fields hold raw flag values; size and
// date strings are parsed into FilterOptions only when BuildFilterOptions
// runs (so error messages reach the user via cobra's RunE rather than
// init()).
type filterFlagsState struct {
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
		"basename glob; repeatable (multiple --name values are OR'd)")
	fs.StringSliceVar(&state.inames, "iname", nil,
		"basename glob, case-insensitive; repeatable")
	fs.StringSliceVar(&state.paths, "path", nil,
		"full-path glob; repeatable. Distinct from positional path-prefix args.")
	fs.StringSliceVar(&state.ipaths, "ipath", nil,
		"full-path glob, case-insensitive; repeatable")
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
