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

// buildSegments parses each segment's args through a fresh pflag set
// (constructed via RegisterFilterFlags) and assembles the per-kind
// FilterOptions slices. Returns prints, ignores, and the first error
// encountered (with the segment kind tagged in the error context).
//
// Empty-args segments are kept (they collapse to the identity-true
// predicate downstream) so a bare `--print` or `--ignore` is harmless.
func buildSegments(segs []scopeSegment) (prints, ignores []dcfh.FilterOptions, err error) {
	for i, seg := range segs {
		opts, perr := parseSegment(seg.args)
		if perr != nil {
			return nil, nil, fmt.Errorf("%s segment #%d: %w", seg.kind, i, perr)
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

// parseSegment runs one segment's args through a one-shot pflag.FlagSet
// configured for the shared filter dialect, returning the populated
// FilterOptions. ContinueOnError keeps a bad flag from bringing down
// the whole process — buildSegments wraps the error with segment
// context.
func parseSegment(args []string) (dcfh.FilterOptions, error) {
	state := &filterFlagsState{}
	fs := pflag.NewFlagSet("scope-segment", pflag.ContinueOnError)
	fs.SetOutput(discardWriter{}) // pflag prints usage on error; suppress it.
	RegisterFilterFlags(fs, state)
	if err := fs.Parse(args); err != nil {
		return dcfh.FilterOptions{}, err
	}
	if extras := fs.Args(); len(extras) > 0 {
		return dcfh.FilterOptions{}, fmt.Errorf("unexpected positional args inside segment: %v", extras)
	}
	return BuildFilterOptions(state)
}

// discardWriter swallows pflag's usage-on-error output. We surface our
// own errors with segment context so the default usage text (which
// references the throwaway "scope-segment" FlagSet) would only confuse.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
