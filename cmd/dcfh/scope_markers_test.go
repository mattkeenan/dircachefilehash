package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		name           string
		in             []string
		wantSegs       []scopeSegment
		wantNoIgnoreFn bool
	}{
		{
			name:     "empty argv: one empty implicit print",
			in:       nil,
			wantSegs: []scopeSegment{{kind: scopePrint}},
		},
		{
			name: "filter flags before any marker stay in the implicit print segment",
			in:   []string{"--name", "*.go", "--min-size", "1K"},
			wantSegs: []scopeSegment{
				{kind: scopePrint, args: []string{"--name", "*.go", "--min-size", "1K"}},
			},
		},
		{
			name: "explicit --print opens a fresh print segment",
			in:   []string{"--name", "*.go", "--print", "--max-size", "1M"},
			wantSegs: []scopeSegment{
				{kind: scopePrint, args: []string{"--name", "*.go"}},
				{kind: scopePrint, args: []string{"--max-size", "1M"}},
			},
		},
		{
			name: "--ignore opens an ignore segment",
			in:   []string{"--name", "*.go", "--ignore", "--name", "*_test.go"},
			wantSegs: []scopeSegment{
				{kind: scopePrint, args: []string{"--name", "*.go"}},
				{kind: scopeIgnore, args: []string{"--name", "*_test.go"}},
			},
		},
		{
			name: "interleaved markers, both kinds, in any order",
			in: []string{
				"--ignore", "--name", "*.tmp",
				"--print", "--min-size", "1M",
				"--ignore", "--name", "*.bak",
			},
			wantSegs: []scopeSegment{
				{kind: scopePrint},
				{kind: scopeIgnore, args: []string{"--name", "*.tmp"}},
				{kind: scopePrint, args: []string{"--min-size", "1M"}},
				{kind: scopeIgnore, args: []string{"--name", "*.bak"}},
			},
		},
		{
			name:           "--no-ignore-file is consumed wherever it appears",
			in:             []string{"--no-ignore-file", "--name", "*.go"},
			wantNoIgnoreFn: true,
			wantSegs: []scopeSegment{
				{kind: scopePrint, args: []string{"--name", "*.go"}},
			},
		},
		{
			name:           "--no-ignore-file mid-segment doesn't pollute the segment",
			in:             []string{"--name", "*.go", "--no-ignore-file", "--ignore", "--name", "*.tmp"},
			wantNoIgnoreFn: true,
			wantSegs: []scopeSegment{
				{kind: scopePrint, args: []string{"--name", "*.go"}},
				{kind: scopeIgnore, args: []string{"--name", "*.tmp"}},
			},
		},
		{
			name: "bare --print marker with no following flags survives as an empty segment",
			in:   []string{"--print", "--ignore", "--name", "*.tmp"},
			wantSegs: []scopeSegment{
				{kind: scopePrint},
				{kind: scopePrint},
				{kind: scopeIgnore, args: []string{"--name", "*.tmp"}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSegs, gotNoIgnoreFn := splitArgs(tc.in)
			if !reflect.DeepEqual(gotSegs, tc.wantSegs) {
				t.Errorf("segments mismatch:\n  got:  %+v\n  want: %+v", gotSegs, tc.wantSegs)
			}
			if gotNoIgnoreFn != tc.wantNoIgnoreFn {
				t.Errorf("noIgnoreFile = %v, want %v", gotNoIgnoreFn, tc.wantNoIgnoreFn)
			}
		})
	}
}

func TestResolveScopes_SegmentParsing(t *testing.T) {
	t.Run("happy path: print + ignore both populate", func(t *testing.T) {
		_, prints, ignores, _, _, err := resolveScopes(
			[]string{"--name", "*.go", "--ignore", "--name", "*_test.go"},
			cmdStatus,
		)
		if err != nil {
			t.Fatalf("resolveScopes: %v", err)
		}
		if len(prints) != 1 || len(prints[0].Names) != 1 || prints[0].Names[0] != "*.go" {
			t.Errorf("prints: %+v", prints)
		}
		if len(ignores) != 1 || len(ignores[0].Names) != 1 || ignores[0].Names[0] != "*_test.go" {
			t.Errorf("ignores: %+v", ignores)
		}
	})

	t.Run("empty segment yields empty FilterOptions, kept in slot", func(t *testing.T) {
		_, prints, ignores, _, _, err := resolveScopes(
			[]string{"--ignore", "--name", "*.tmp"},
			cmdStatus,
		)
		if err != nil {
			t.Fatalf("resolveScopes: %v", err)
		}
		if len(prints) != 1 || !prints[0].IsEmpty() {
			t.Errorf("expected one empty implicit print segment, got %+v", prints)
		}
		if len(ignores) != 1 || ignores[0].IsEmpty() {
			t.Errorf("expected one populated ignore segment, got %+v", ignores)
		}
	})

	t.Run("bad flag in segment zero tagged as print segment #0", func(t *testing.T) {
		_, _, _, _, _, err := resolveScopes(
			[]string{"--bogus-flag", "value"},
			cmdStatus,
		)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "print segment #0") {
			t.Errorf("error should tag segment-0 kind, got %q", err)
		}
	})

	t.Run("malformed size value tagged with kind context", func(t *testing.T) {
		_, _, _, _, _, err := resolveScopes(
			[]string{"--ignore", "--min-size", "not-a-size"},
			cmdStatus,
		)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "ignore segment") {
			t.Errorf("error should tag segment kind, got %q", err)
		}
	})
}

// TestResolveScopes_DupesCmdFlagsAndPositionals asserts that the
// dupes-only flags (--exclusive, -H, --fs-dedupe) populate state and
// that positional args after segment-zero parsing flow back as path
// prefixes — the union of behaviours that buildDupeFilter relies on.
func TestResolveScopes_DupesCmdFlagsAndPositionals(t *testing.T) {
	state, prints, ignores, positionals, noIgn, err := resolveScopes(
		[]string{"--exclusive=no", "-H", "--fs-dedupe", "sub/", "--ignore", "--name", "*.bak"},
		cmdDupes,
	)
	if err != nil {
		t.Fatalf("resolveScopes: %v", err)
	}
	if bool(state.exclusive) {
		t.Errorf("exclusive=true; want false (--exclusive=no)")
	}
	if !state.ignoreHardlinks {
		t.Errorf("ignoreHardlinks=false; want true (-H)")
	}
	if !state.fsDedupe {
		t.Errorf("fsDedupe=false; want true (--fs-dedupe)")
	}
	if len(positionals) != 1 || positionals[0] != "sub/" {
		t.Errorf("positionals=%v; want [\"sub/\"]", positionals)
	}
	if len(prints) != 1 || !prints[0].IsEmpty() {
		t.Errorf("prints=%+v; want one empty implicit segment", prints)
	}
	if len(ignores) != 1 || len(ignores[0].Names) != 1 || ignores[0].Names[0] != "*.bak" {
		t.Errorf("ignores=%+v; want one segment with Names=[*.bak]", ignores)
	}
	if noIgn {
		t.Errorf("noIgnoreFile=true unexpectedly")
	}
}

// TestResolveScopes_PersistentFlagInTailSegment asserts that root
// persistent flags parse anywhere in the argv, not just inside
// segment zero — accepted syntactically without erroring. Tail
// segments bind to throwaways, so the value is discarded; only
// segment-zero appearances flow through to the package globals.
func TestResolveScopes_PersistentFlagInTailSegment(t *testing.T) {
	saved := flagVerbose
	flagVerbose = 0
	defer func() { flagVerbose = saved }()

	_, _, _, _, _, err := resolveScopes(
		[]string{"--ignore", "--name", "*.tmp", "--verbose"},
		cmdStatus,
	)
	if err != nil {
		t.Fatalf("resolveScopes: %v", err)
	}
	if flagVerbose != 0 {
		t.Errorf("flagVerbose=%d; tail-segment --verbose should not write through, want 0", flagVerbose)
	}
}

// TestResolveScopes_PersistentFlagInSegmentZeroSurvivesTail asserts
// that a persistent flag written by segment zero is preserved across
// the tail-segment parse — the regression that motivated tail
// throwaway binding (without it, the tail's BoolVar reset
// flagJSON to its false default).
func TestResolveScopes_PersistentFlagInSegmentZeroSurvivesTail(t *testing.T) {
	saved := flagJSON
	flagJSON = false
	defer func() { flagJSON = saved }()

	_, _, _, _, _, err := resolveScopes(
		[]string{"--json", "--ignore", "--name", "*.tmp"},
		cmdStatus,
	)
	if err != nil {
		t.Fatalf("resolveScopes: %v", err)
	}
	if !flagJSON {
		t.Errorf("flagJSON=false after --json in segment zero; tail parse clobbered it")
	}
}
