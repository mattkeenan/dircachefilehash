package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
	"github.com/mattkeenan/dircachefilehash/pkg/fsdedupe"
)

func TestNormaliseDupePaths(t *testing.T) {
	root := t.TempDir()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs(root): %v", err)
	}

	tests := []struct {
		name    string
		args    []string
		want    []string
		wantErr bool
	}{
		{
			name: "no args returns nil",
			args: nil,
			want: nil,
		},
		{
			name: "absolute path inside repo",
			args: []string{filepath.Join(absRoot, "sub")},
			want: []string{"sub/"},
		},
		{
			name: "trailing slash preserved after normalisation",
			args: []string{filepath.Join(absRoot, "sub") + "/"},
			want: []string{"sub/"},
		},
		{
			name: "repo root dot collapses to whole-repo fast path",
			args: []string{absRoot},
			want: nil,
		},
		{
			name:    "path outside repo rejected",
			args:    []string{"/tmp"},
			wantErr: true,
		},
		{
			name:    "parent traversal rejected",
			args:    []string{filepath.Join(absRoot, "..")},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normaliseDupePaths(absRoot, tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// extractMinSize walks a FilterExpr tree looking for the first
// MinSizeTest leaf (size and time predicates AND together at the top of
// the BuildFilter output, so a left-first descent suffices for these
// tests). Used by the FS-dedupe injection assertions below.
func extractMinSize(pred dcfh.FilterExpr) *uint64 {
	switch p := pred.(type) {
	case nil:
		return nil
	case *dcfh.MinSizeTest:
		v := p.Min
		return &v
	case *dcfh.AndExpression:
		if v := extractMinSize(p.Left); v != nil {
			return v
		}
		return extractMinSize(p.Right)
	}
	return nil
}

// resetDupesFlags resets the dupes-scoped globals the tests touch.
// Keeps the resetFlags() helper in dcfh_test.go focused on the
// root-level flags.
func resetDupesFlags() {
	dupesExclusive = yesNoFlag(true)
	dupesMinSizeStr = ""
	dupesMaxSizeStr = ""
	dupesStartDateStr = ""
	dupesEndDateStr = ""
	dupesTZ = ""
	dupesIgnoreHardlinks = false
	dupesFSDedupe = false
}

func TestBuildDupeFilter_FSDedupeForcesIgnoreHardlinks(t *testing.T) {
	resetDupesFlags()
	dupesFSDedupe = true
	// IgnoreHardlinks is left false; the --fs-dedupe branch must
	// force it on regardless of how -H was set on the command line.
	f, err := buildDupeFilter(dupesCmd, nil)
	if err != nil {
		t.Fatalf("buildDupeFilter: %v", err)
	}
	if !f.IgnoreHardlinks {
		t.Error("--fs-dedupe did not force IgnoreHardlinks=true")
	}
	min := extractMinSize(f.Predicate)
	if min == nil || *min != dedupeDefaultMinSize {
		t.Errorf("MinSize=%v; want default %d when --fs-dedupe and --min-size not set",
			min, dedupeDefaultMinSize)
	}
}

func TestBuildDupeFilter_FSDedupeRespectsUserMinSize(t *testing.T) {
	// Reset, then simulate `--fs-dedupe --min-size 8K`. A user who
	// explicitly sets --min-size wins over the dedupe default.
	resetDupesFlags()
	dupesFSDedupe = true
	dupesMinSizeStr = "8K"
	// Register the transient flag value as "Changed" by parsing via
	// the same cobra flagset a real invocation would use.
	cmd := dupesCmd
	if err := cmd.Flags().Set(flagMinSize, "8K"); err != nil {
		t.Fatalf("Flags.Set: %v", err)
	}
	defer func() {
		// Clean up the Changed flag state so later tests aren't polluted.
		_ = cmd.Flags().Set(flagMinSize, "")
		cmd.Flags().Lookup(flagMinSize).Changed = false
	}()

	f, err := buildDupeFilter(cmd, nil)
	if err != nil {
		t.Fatalf("buildDupeFilter: %v", err)
	}
	min := extractMinSize(f.Predicate)
	if min == nil || *min != 8192 {
		t.Errorf("MinSize=%v; want 8192 (user-provided)", min)
	}
}

func TestBuildDupeFilter_NoFSDedupe_NoImplicitMinSize(t *testing.T) {
	// Regression gate: without --fs-dedupe, MinSize must remain nil
	// when --min-size is absent, so non-dedupe users keep today's
	// behaviour (dupes reports every group regardless of size).
	resetDupesFlags()
	f, err := buildDupeFilter(dupesCmd, nil)
	if err != nil {
		t.Fatalf("buildDupeFilter: %v", err)
	}
	if min := extractMinSize(f.Predicate); min != nil {
		t.Errorf("MinSize=%v; want nil when --fs-dedupe is off", min)
	}
	if f.IgnoreHardlinks {
		t.Error("IgnoreHardlinks=true; want false when -H and --fs-dedupe are both off")
	}
}

func TestRunDedupe_StubReceivesGroups(t *testing.T) {
	// Verifies the cmd-layer dispatch: the dupes groups reach
	// runFSDedupe with correct shape, and its Result flows back
	// without alteration.
	orig := runFSDedupe
	defer func() { runFSDedupe = orig }()

	var captured []fsdedupe.Group
	var capturedOpts fsdedupe.Options
	runFSDedupe = func(_ context.Context, groups []fsdedupe.Group, opts fsdedupe.Options) (*fsdedupe.Result, error) {
		captured = groups
		capturedOpts = opts
		return &fsdedupe.Result{
			Groups: []fsdedupe.GroupResult{{
				Hash:    "h1",
				Outcome: fsdedupe.OutcomeOK,
			}},
			TotalReclaimed: 1024,
		}, nil
	}

	var streamed []string
	onGroup := func(gr fsdedupe.GroupResult) {
		streamed = append(streamed, gr.Hash)
	}

	res, err := runDedupe(context.Background(), "/repo",
		[]dcfh.DuplicateGroup{{Hash: "h1", Files: []string{"a", "b"}, Count: 2}},
		onGroup,
	)
	if err != nil {
		t.Fatalf("runDedupe: %v", err)
	}
	if res == nil || res.TotalReclaimed != 1024 {
		t.Errorf("stub Result not returned: %+v", res)
	}
	if len(captured) != 1 || captured[0].Hash != "h1" || len(captured[0].Files) != 2 {
		t.Errorf("captured groups wrong shape: %+v", captured)
	}
	if capturedOpts.RepoRoot != "/repo" {
		t.Errorf("captured opts.RepoRoot=%q; want /repo", capturedOpts.RepoRoot)
	}
	if capturedOpts.Logf == nil {
		t.Error("captured opts.Logf was nil; want stderr logger")
	}
	if capturedOpts.OnGroup == nil {
		t.Error("captured opts.OnGroup was nil; want propagated callback")
	}
	// The stub returned a Result with one group; in real fsdedupe.Run
	// the callback fires for each group as it completes. Simulate that
	// here so the wiring assertion is end-to-end.
	if capturedOpts.OnGroup != nil {
		capturedOpts.OnGroup(res.Groups[0])
	}
	if len(streamed) != 1 || streamed[0] != "h1" {
		t.Errorf("OnGroup did not surface stub group: %v", streamed)
	}
}

func TestRunDedupe_UnsupportedPlatformErrorSurfaces(t *testing.T) {
	// Simulates the non-Linux / disabled-build path: stub returns
	// ErrUnsupported and runDedupe must pass it back unchanged so
	// the caller can exit non-zero.
	orig := runFSDedupe
	defer func() { runFSDedupe = orig }()
	runFSDedupe = func(context.Context, []fsdedupe.Group, fsdedupe.Options) (*fsdedupe.Result, error) {
		return nil, fsdedupe.ErrUnsupported
	}
	res, err := runDedupe(context.Background(), "/repo", nil, nil)
	if !errors.Is(err, fsdedupe.ErrUnsupported) {
		t.Errorf("err=%v; want ErrUnsupported", err)
	}
	if res != nil {
		t.Errorf("res=%+v; want nil on unsupported", res)
	}
}

func TestYesNoFlag(t *testing.T) {
	f := yesNoFlag(true)
	if f.String() != "yes" {
		t.Errorf("default String=%q, want yes", f.String())
	}
	if err := f.Set("no"); err != nil {
		t.Fatalf("Set(no): %v", err)
	}
	if bool(f) {
		t.Errorf("after Set(no), want false")
	}
	if err := f.Set("bogus"); err == nil {
		t.Errorf("Set(bogus) should error")
	}
	if f.Type() != "yes|no" {
		t.Errorf("Type()=%q, want yes|no", f.Type())
	}
}
