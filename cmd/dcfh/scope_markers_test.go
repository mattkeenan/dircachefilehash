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

func TestBuildSegments(t *testing.T) {
	t.Run("happy path: print + ignore both populate", func(t *testing.T) {
		segs := []scopeSegment{
			{kind: scopePrint, args: []string{"--name", "*.go"}},
			{kind: scopeIgnore, args: []string{"--name", "*_test.go"}},
		}
		prints, ignores, err := buildSegments(segs)
		if err != nil {
			t.Fatalf("buildSegments: %v", err)
		}
		if len(prints) != 1 || len(prints[0].Names) != 1 || prints[0].Names[0] != "*.go" {
			t.Errorf("prints: %+v", prints)
		}
		if len(ignores) != 1 || len(ignores[0].Names) != 1 || ignores[0].Names[0] != "*_test.go" {
			t.Errorf("ignores: %+v", ignores)
		}
	})

	t.Run("empty segment yields empty FilterOptions, kept in slot", func(t *testing.T) {
		segs := []scopeSegment{
			{kind: scopePrint}, // bare implicit print
			{kind: scopeIgnore, args: []string{"--name", "*.tmp"}},
		}
		prints, ignores, err := buildSegments(segs)
		if err != nil {
			t.Fatalf("buildSegments: %v", err)
		}
		if len(prints) != 1 || !prints[0].IsEmpty() {
			t.Errorf("expected one empty print segment, got %+v", prints)
		}
		if len(ignores) != 1 || ignores[0].IsEmpty() {
			t.Errorf("expected one populated ignore segment, got %+v", ignores)
		}
	})

	t.Run("bad flag inside a segment is reported with segment context", func(t *testing.T) {
		segs := []scopeSegment{
			{kind: scopePrint, args: []string{"--bogus-flag", "value"}},
		}
		_, _, err := buildSegments(segs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "print segment") {
			t.Errorf("error should tag segment kind, got %q", err)
		}
	})

	t.Run("malformed size value bubbles up via BuildFilterOptions", func(t *testing.T) {
		segs := []scopeSegment{
			{kind: scopeIgnore, args: []string{"--min-size", "not-a-size"}},
		}
		_, _, err := buildSegments(segs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "ignore segment") {
			t.Errorf("error should tag segment kind, got %q", err)
		}
	})
}
