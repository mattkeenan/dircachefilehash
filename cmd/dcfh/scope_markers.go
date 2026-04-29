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

// buildSegments is the test seam over parseSegment: parses every
// segment with the empty-command, tail-segment registry view (filter
// + persistent dialect, no command-specific flags) and bins results
// by kind. Production callers go through resolveScopes, which adds
// segment-zero command-specific handling and positional residual.
func buildSegments(segs []scopeSegment) (prints, ignores []dcfh.FilterOptions, err error) {
	for i, seg := range segs {
		_, opts, extras, perr := parseSegment(seg.args, "", false)
		if perr != nil {
			return nil, nil, fmt.Errorf("%s segment #%d: %w", seg.kind, i, perr)
		}
		if len(extras) > 0 {
			return nil, nil, fmt.Errorf("%s segment #%d: unexpected positional args: %v", seg.kind, i, extras)
		}
		switch seg.kind {
		case scopePrint:
			prints = append(prints, opts)
		case scopeIgnore:
			ignores = append(ignores, opts)
		}
	}
	return prints, ignores, nil
}

// parseSegment runs one segment's args through a one-shot
// pflag.FlagSet configured via the cmdFlagRegistry. firstSegment
// selects between the segment-zero parser (command-specific toggles
// allowed) and the tail-segment parser used for explicit --print /
// --ignore groups. Returns the segment's state, FilterOptions, and
// positional residual.
//
// ContinueOnError keeps a bad flag from bringing down the whole
// process — buildSegments / resolveScopes wrap the error with
// segment context.
func parseSegment(args []string, command string, firstSegment bool) (*filterFlagsState, dcfh.FilterOptions, []string, error) {
	state := newFilterFlagsState()
	name := "scope-segment"
	if firstSegment {
		name = "scope-segment-0"
	}
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.SetOutput(discardWriter{}) // pflag prints usage on error; suppress it.
	RegisterCmdFlags(fs, state, command, firstSegment)
	if err := fs.Parse(args); err != nil {
		return nil, dcfh.FilterOptions{}, nil, err
	}
	opts, err := BuildFilterOptions(state)
	if err != nil {
		return nil, dcfh.FilterOptions{}, nil, err
	}
	return state, opts, fs.Args(), nil
}

// resolveScopes is the canonical RunE preamble for commands that
// support scope markers. It splits argv, parses segment 0 with the
// command's full registry view (filter + persistent + command-
// specific), then parses the remaining segments with the per-segment
// view (filter + persistent only). Returns the composed
// prints/ignores/positionals/noIgnoreFile quadruple plus the
// segment-zero state for command-specific flag readout.
//
// Empty input collapses to a single empty print segment. Tail
// segments must not produce positional residuals — only segment zero
// owns positionals. Errors carry the offending segment's index so
// the user knows which `--print` / `--ignore` group rejected their
// flag.
func resolveScopes(args []string, command string) (state *filterFlagsState, prints, ignores []dcfh.FilterOptions, positionals []string, noIgnoreFile bool, err error) {
	segs, noIgnoreFile := splitArgs(args)
	state, zeroOpts, positionals, err := parseSegment(segs[0].args, command, true)
	if err != nil {
		return nil, nil, nil, nil, false, fmt.Errorf("print segment #0: %w", err)
	}
	prints = []dcfh.FilterOptions{zeroOpts}
	for i, seg := range segs[1:] {
		_, opts, extras, perr := parseSegment(seg.args, command, false)
		if perr != nil {
			return nil, nil, nil, nil, false, fmt.Errorf("%s segment #%d: %w", seg.kind, i+1, perr)
		}
		if len(extras) > 0 {
			return nil, nil, nil, nil, false, fmt.Errorf("%s segment #%d: unexpected positional args: %v", seg.kind, i+1, extras)
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
