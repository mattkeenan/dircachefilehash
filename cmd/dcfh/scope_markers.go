package main

import (
	"fmt"

	"github.com/spf13/pflag"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// Scope markers split the per-command argv into independent filter
// groups. A `--print` segment narrows what the command reports; a
// `--ignore` segment subtracts from that report (and on commands that
// scan, also short-circuits the scan walker so subtracted entries are
// never re-stat'd or re-hashed).
//
// Composition is documented at the top of pkg/flat_filter.go's
// BuildPrintIgnoreTree: AND across print segments, OR across ignore
// segments, the ignore-result negated and AND'd into the final tree.
//
// `--no-ignore-file` is recognised at the top level (not bound to a
// scope) and is the only way to suppress the persistent .dcfh/ignore
// file's effect for the run.
const (
	markerPrint        = "--print"
	markerIgnore       = "--ignore"
	markerNoIgnoreFile = "--no-ignore-file"
)

// scopeKind tags a segment as either a print or an ignore group.
type scopeKind int

const (
	scopePrint scopeKind = iota
	scopeIgnore
)

func (k scopeKind) String() string {
	if k == scopeIgnore {
		return "ignore"
	}
	return "print"
}

// scopeSegment is one print- or ignore-group from the parsed argv. args
// is the slice of tokens that belong to this group, with the marker
// itself stripped. The implicit first segment (the one that starts
// before any explicit marker) is always present and always kind=print.
type scopeSegment struct {
	kind scopeKind
	args []string
}

// splitArgs walks args left-to-right and partitions them into scope
// segments. The first segment is always an implicit print segment;
// further `--print` / `--ignore` tokens start fresh segments of the
// matching kind. `--no-ignore-file` is consumed wherever it appears
// and surfaced via the bool return.
//
// splitArgs makes no attempt to distinguish filter flags from
// command-specific flags or positionals — every non-marker token
// lands in the currently-open segment. Callers that want to peel off
// command-specific bits (e.g. dupes' `--exclusive`, positional
// path-prefix args) should do so before calling splitArgs, or after
// against the implicit first segment's args.
func splitArgs(args []string) (segs []scopeSegment, noIgnoreFile bool) {
	segs = []scopeSegment{{kind: scopePrint}}
	for _, tok := range args {
		switch tok {
		case markerNoIgnoreFile:
			noIgnoreFile = true
		case markerPrint:
			segs = append(segs, scopeSegment{kind: scopePrint})
		case markerIgnore:
			segs = append(segs, scopeSegment{kind: scopeIgnore})
		default:
			segs[len(segs)-1].args = append(segs[len(segs)-1].args, tok)
		}
	}
	return segs, noIgnoreFile
}

// parseSegmentZero parses the implicit first segment of a
// scope-marker argv. Returns the per-segment state (carrying both
// FilterOptions inputs and any command-specific toggles like dupes'
// --fs-dedupe), the FilterOptions, and any positional residual —
// only segment zero ever owns positionals.
func parseSegmentZero(args []string, command string) (*filterFlagsState, dcfh.FilterOptions, []string, error) {
	state := newFilterFlagsState()
	fs := pflag.NewFlagSet("scope-segment-0", pflag.ContinueOnError)
	fs.SetOutput(discardWriter{})
	registerSegmentZeroFlags(fs, state, command)
	if err := fs.Parse(args); err != nil {
		return nil, dcfh.FilterOptions{}, nil, err
	}
	opts, err := BuildFilterOptions(state)
	if err != nil {
		return nil, dcfh.FilterOptions{}, nil, err
	}
	return state, opts, fs.Args(), nil
}

// parseTailSegment parses one explicit --print / --ignore segment.
// Tail segments only see filter + persistent flags and never carry
// positionals; FilterOptions is the only meaningful output.
func parseTailSegment(args []string) (dcfh.FilterOptions, error) {
	state := newFilterFlagsState()
	fs := pflag.NewFlagSet("scope-segment", pflag.ContinueOnError)
	fs.SetOutput(discardWriter{})
	registerTailSegmentFlags(fs, state)
	if err := fs.Parse(args); err != nil {
		return dcfh.FilterOptions{}, err
	}
	if extras := fs.Args(); len(extras) > 0 {
		return dcfh.FilterOptions{}, fmt.Errorf("unexpected positional args: %v", extras)
	}
	return BuildFilterOptions(state)
}

// resolveScopes is the canonical RunE preamble for scope-marker
// commands. Splits argv, parses segment zero with the command's full
// registry view, then parses every explicit --print / --ignore
// segment with the tail-segment view. Empty input collapses to a
// single empty print segment. Errors carry the offending segment's
// index so the user can pinpoint the rejected flag.
func resolveScopes(args []string, command string) (state *filterFlagsState, prints, ignores []dcfh.FilterOptions, positionals []string, noIgnoreFile bool, err error) {
	segs, noIgnoreFile := splitArgs(args)
	state, zeroOpts, positionals, err := parseSegmentZero(segs[0].args, command)
	if err != nil {
		return nil, nil, nil, nil, false, fmt.Errorf("print segment #0: %w", err)
	}
	prints = []dcfh.FilterOptions{zeroOpts}
	for i, seg := range segs[1:] {
		opts, perr := parseTailSegment(seg.args)
		if perr != nil {
			return nil, nil, nil, nil, false, fmt.Errorf("%s segment #%d: %w", seg.kind, i+1, perr)
		}
		switch seg.kind {
		case scopePrint:
			prints = append(prints, opts)
		case scopeIgnore:
			ignores = append(ignores, opts)
		}
	}
	return state, prints, ignores, positionals, noIgnoreFile, nil
}

// discardWriter swallows pflag's usage-on-error output. We surface our
// own errors with segment context so the default usage text (which
// references the throwaway "scope-segment" FlagSet) would only confuse.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
