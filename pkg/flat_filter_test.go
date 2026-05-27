package dircachefilehash

import (
	"strings"
	"testing"
	"time"
)

func TestMinMaxSizeTest(t *testing.T) {
	cases := []struct {
		name string
		size int64
		min  int64
		max  int64
		ok   bool
		test FilterExpr
	}{
		{"min equal", 1024, 1024, 0, true, &MinSizeTest{Min: 1024}},
		{"min above", 2048, 1024, 0, true, &MinSizeTest{Min: 1024}},
		{"min below", 512, 1024, 0, false, &MinSizeTest{Min: 1024}},
		{"max equal", 1024, 0, 1024, true, &MaxSizeTest{Max: 1024}},
		{"max below", 512, 0, 1024, true, &MaxSizeTest{Max: 1024}},
		{"max above", 2048, 0, 1024, false, &MaxSizeTest{Max: 1024}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &EntryInfo{FileSize: tc.size}
			got, err := tc.test.Evaluate(info.AsFilterEntry(), &FilterContext{})
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if got != tc.ok {
				t.Fatalf("got %v, want %v", got, tc.ok)
			}
		})
	}
}

func TestMTimeRangeTest(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		mtime time.Time
		start time.Time
		end   time.Time
		want  bool
	}{
		{"in range", t2, t1, t3, true},
		{"start inclusive", t1, t1, t3, true},
		{"end exclusive", t3, t1, t3, false},
		{"before start", t0, t1, t3, false},
		{"unbounded start", t0, time.Time{}, t3, true},
		{"unbounded end", t3, t1, time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &EntryInfo{MTimeWall: TimeToWall(tc.mtime)}
			expr := &MTimeRangeTest{Start: tc.start, End: tc.end}
			got, err := expr.Evaluate(info.AsFilterEntry(), &FilterContext{})
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildFilterEmpty(t *testing.T) {
	expr, err := BuildFilter(FilterOptions{})
	if err != nil {
		t.Fatalf("BuildFilter: %v", err)
	}
	if expr != nil {
		t.Fatalf("expected nil expr for empty opts, got %v", expr)
	}
}

func TestBuildFilterAndAcrossKinds(t *testing.T) {
	min := int64(1024)
	expr, err := BuildFilter(FilterOptions{
		MinSize: &min,
		Names:   []string{"*.go"},
	})
	if err != nil {
		t.Fatalf("BuildFilter: %v", err)
	}

	// Match: large + name matches.
	yes := &EntryInfo{Path: "main.go", FileSize: 2048}
	got, err := expr.Evaluate(yes.AsFilterEntry(), &FilterContext{})
	if err != nil || !got {
		t.Fatalf("yes case: got %v err %v", got, err)
	}

	// Reject: small but matching name.
	small := &EntryInfo{Path: "main.go", FileSize: 100}
	got, err = expr.Evaluate(small.AsFilterEntry(), &FilterContext{})
	if err != nil || got {
		t.Fatalf("small case: got %v err %v", got, err)
	}

	// Reject: large but wrong name.
	other := &EntryInfo{Path: "main.py", FileSize: 2048}
	got, err = expr.Evaluate(other.AsFilterEntry(), &FilterContext{})
	if err != nil || got {
		t.Fatalf("other case: got %v err %v", got, err)
	}
}

func TestBuildFilterOrWithinKind(t *testing.T) {
	expr, err := BuildFilter(FilterOptions{
		Names: []string{"*.go", "*.md"},
	})
	if err != nil {
		t.Fatalf("BuildFilter: %v", err)
	}
	for _, path := range []string{"main.go", "README.md"} {
		info := &EntryInfo{Path: path}
		got, err := expr.Evaluate(info.AsFilterEntry(), &FilterContext{})
		if err != nil || !got {
			t.Fatalf("%s: got %v err %v", path, got, err)
		}
	}
	other := &EntryInfo{Path: "main.py"}
	got, err := expr.Evaluate(other.AsFilterEntry(), &FilterContext{})
	if err != nil || got {
		t.Fatalf("other: got %v err %v", got, err)
	}
}

func TestBuildFilterMinExceedsMax(t *testing.T) {
	min := int64(2048)
	max := int64(1024)
	_, err := BuildFilter(FilterOptions{MinSize: &min, MaxSize: &max})
	if err == nil {
		t.Fatal("expected error when min > max")
	}
}

func TestBuildFilterStartNotBeforeEnd(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := BuildFilter(FilterOptions{StartDate: t1, EndDate: t1})
	if err == nil {
		t.Fatal("expected error when start == end")
	}
}

func TestBuildFilterSizeAndAge(t *testing.T) {
	expr, err := BuildFilter(FilterOptions{
		Sizes:  []string{"+1K"},
		MTimes: []string{"-365"},
	})
	if err != nil {
		t.Fatalf("BuildFilter: %v", err)
	}
	if expr == nil {
		t.Fatal("expected non-nil expr")
	}
	// Smoke test: ensure both predicates run without erroring.
	info := &EntryInfo{
		Path:      "test.txt",
		FileSize:  4096,
		MTimeWall: TimeToWall(time.Now().Add(-7 * 24 * time.Hour)),
	}
	got, err := expr.Evaluate(info.AsFilterEntry(), &FilterContext{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !got {
		t.Fatal("expected match for >1K and within last year")
	}
}

func TestParseSizeTestSpec(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		mode string
		err  bool
	}{
		{in: "+1K", want: 1024, mode: "+"},
		{in: "-100", want: 100 * 512, mode: "-"},
		{in: "=1M", want: 1024 * 1024, mode: "="},
		{in: "1c", want: 1, mode: "="},
		{in: "10w", want: 20, mode: "="},
		{in: "", err: true},
		{in: "+", err: true},
		{in: "+abc", err: true},
		{in: "1Z", err: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseSizeTestSpec(tc.in)
			if tc.err {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Size != tc.want || got.Mode != tc.mode {
				t.Fatalf("got %+v, want size=%d mode=%s", got, tc.want, tc.mode)
			}
		})
	}
}

func TestSizeBoundString(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{512, "512"},
		{1024, "1K"},
		{2 * 1024 * 1024, "2M"},
		{3 * 1024 * 1024 * 1024, "3G"},
		{1024 * 1024 * 1024 * 1024, "1T"},
		{1500, "1500"},
	}
	for _, tc := range cases {
		if got := SizeBoundString(tc.in); got != tc.want {
			t.Errorf("SizeBoundString(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseSizeBound(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "0", want: 0},
		{in: "100", want: 100},
		{in: "1K", want: 1024},
		{in: "1k", want: 1024},
		{in: "2M", want: 2 * 1024 * 1024},
		{in: "1G", want: 1024 * 1024 * 1024},
		{in: "1T", want: 1024 * 1024 * 1024 * 1024},
		{in: "", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "+1", wantErr: true},
		{in: "1.5K", wantErr: true},
		{in: "abc", wantErr: true},
		{in: "1X", wantErr: true},
		{in: "K", wantErr: true},
		{in: " 1K", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseSizeBound(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParsePartialDateTime_UTC(t *testing.T) {
	tests := []struct {
		in   string
		want time.Time
	}{
		{"2026", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-03", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-03-15", time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)},
		{"2026-03-15T06", time.Date(2026, 3, 15, 6, 0, 0, 0, time.UTC)},
		{"2026-03-15T06:30", time.Date(2026, 3, 15, 6, 30, 0, 0, time.UTC)},
		{"2026-03-15T06:30:45", time.Date(2026, 3, 15, 6, 30, 45, 0, time.UTC)},
		{"2026Z", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-03-15T06:30:45Z", time.Date(2026, 3, 15, 6, 30, 45, 0, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParsePartialDateTime(tc.in, time.UTC)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestParsePartialDateTime_Offsets(t *testing.T) {
	tests := []struct {
		in       string
		wantUnix int64
	}{
		{"2026-01-01T00:00:00+09:00", time.Date(2025, 12, 31, 15, 0, 0, 0, time.UTC).Unix()},
		{"2026-01-01T00:00:00-05:30", time.Date(2026, 1, 1, 5, 30, 0, 0, time.UTC).Unix()},
		{"2026-01-01T00:00:00+0100", time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC).Unix()},
		{"2026-01-01T00:00:00+02", time.Date(2025, 12, 31, 22, 0, 0, 0, time.UTC).Unix()},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParsePartialDateTime(tc.in, time.UTC)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Unix() != tc.wantUnix {
				t.Errorf("got unix %d (%s), want %d", got.Unix(), got, tc.wantUnix)
			}
		})
	}
}

func TestParsePartialDateTime_Invalid(t *testing.T) {
	bad := []string{
		"", "abc", "2026-13", "2026-00", "2026-01-00", "2026-01-32",
		"2026-02-30", "2026-01-01T24", "2026-01-01T00:60", "2026-01-01T00:00:60",
		"2026T", "2026-01-01+25:00", "202", "2026-1", "2026-01-01Z+02:00",
	}
	for _, s := range bad {
		t.Run(s, func(t *testing.T) {
			if _, err := ParsePartialDateTime(s, time.UTC); err == nil {
				t.Errorf("want error for %q", s)
			}
		})
	}
}

func TestParsePartialDateTime_Berlin(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("Europe/Berlin tzdata unavailable: %v", err)
	}
	// Winter: January is CET (+01:00).
	got, err := ParsePartialDateTime("2026", berlin)
	if err != nil {
		t.Fatalf("parse 2026: %v", err)
	}
	_, offset := got.Zone()
	if offset != 3600 {
		t.Errorf("Jan 1 Berlin offset = %d, want 3600 (CET)", offset)
	}
	if got.Unix() != time.Date(2026, 1, 1, 0, 0, 0, 0, berlin).Unix() {
		t.Errorf("Jan 1 midnight Berlin instant mismatch")
	}
	// Summer: July is CEST (+02:00).
	got, err = ParsePartialDateTime("2026-07", berlin)
	if err != nil {
		t.Fatalf("parse 2026-07: %v", err)
	}
	_, offset = got.Zone()
	if offset != 7200 {
		t.Errorf("Jul 1 Berlin offset = %d, want 7200 (CEST)", offset)
	}
}

func TestResolveZone(t *testing.T) {
	loc, err := ResolveZone("")
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if loc != time.Local {
		t.Errorf("empty flag should return time.Local, got %v", loc)
	}
	if _, err := ResolveZone("UTC"); err != nil {
		t.Errorf("UTC: %v", err)
	}
	if loc, err := ResolveZone("Europe/Berlin"); err != nil {
		t.Skipf("Europe/Berlin tzdata unavailable: %v", err)
	} else if loc.String() != "Europe/Berlin" {
		t.Errorf("got %s, want Europe/Berlin", loc)
	}
	if _, err := ResolveZone("Europe/NoSuchZone"); err == nil {
		t.Errorf("bogus zone should error")
	}
}

func TestResolveDates(t *testing.T) {
	startT, endT, err := ResolveDates("2025", "2027", "UTC")
	if err != nil {
		t.Fatalf("ResolveDates: %v", err)
	}
	wantStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if !startT.Equal(wantStart) {
		t.Errorf("start: got %v, want %v", startT, wantStart)
	}
	if !endT.Equal(wantEnd) {
		t.Errorf("end: got %v, want %v", endT, wantEnd)
	}
}

func TestBuildPrintIgnoreTree(t *testing.T) {
	eval := func(t *testing.T, expr FilterExpr, path string, size int64) bool {
		t.Helper()
		if expr == nil {
			return true // identity
		}
		info := &EntryInfo{Path: path, FileSize: size}
		got, err := expr.Evaluate(info.AsFilterEntry(), &FilterContext{})
		if err != nil {
			t.Fatalf("Evaluate(%s): %v", path, err)
		}
		return got
	}

	t.Run("empty inputs return nil identity", func(t *testing.T) {
		expr, err := BuildPrintIgnoreTree(nil, nil)
		if err != nil {
			t.Fatalf("BuildPrintIgnoreTree: %v", err)
		}
		if expr != nil {
			t.Fatalf("expected nil expr, got %v", expr)
		}
	})

	t.Run("all-empty options on both sides also collapse to nil", func(t *testing.T) {
		expr, err := BuildPrintIgnoreTree(
			[]FilterOptions{{}, {}},
			[]FilterOptions{{}},
		)
		if err != nil {
			t.Fatalf("BuildPrintIgnoreTree: %v", err)
		}
		if expr != nil {
			t.Fatalf("expected nil expr for all-empty inputs, got %v", expr)
		}
	})

	t.Run("print only: behaves like BuildFilter", func(t *testing.T) {
		expr, err := BuildPrintIgnoreTree(
			[]FilterOptions{{Names: []string{"*.go"}}},
			nil,
		)
		if err != nil {
			t.Fatalf("BuildPrintIgnoreTree: %v", err)
		}
		if !eval(t, expr, "main.go", 0) {
			t.Error("main.go should match")
		}
		if eval(t, expr, "main.py", 0) {
			t.Error("main.py should not match")
		}
	})

	t.Run("ignore only: identity print, negated ignore", func(t *testing.T) {
		expr, err := BuildPrintIgnoreTree(
			nil,
			[]FilterOptions{{Names: []string{"*.tmp"}}},
		)
		if err != nil {
			t.Fatalf("BuildPrintIgnoreTree: %v", err)
		}
		if !eval(t, expr, "main.go", 0) {
			t.Error("main.go should pass (not ignored)")
		}
		if eval(t, expr, "scratch.tmp", 0) {
			t.Error("scratch.tmp should be filtered out by ignore")
		}
	})

	t.Run("multi-print: AND across segments", func(t *testing.T) {
		min := int64(1024)
		expr, err := BuildPrintIgnoreTree(
			[]FilterOptions{
				{Names: []string{"*.go"}},
				{MinSize: &min},
			},
			nil,
		)
		if err != nil {
			t.Fatalf("BuildPrintIgnoreTree: %v", err)
		}
		if !eval(t, expr, "main.go", 2048) {
			t.Error("large *.go should match")
		}
		if eval(t, expr, "main.go", 100) {
			t.Error("small *.go should fail size segment")
		}
		if eval(t, expr, "main.py", 2048) {
			t.Error("large *.py should fail name segment")
		}
	})

	t.Run("multi-ignore: OR across segments, negated", func(t *testing.T) {
		expr, err := BuildPrintIgnoreTree(
			nil,
			[]FilterOptions{
				{Names: []string{"*.tmp"}},
				{Names: []string{"*.bak"}},
			},
		)
		if err != nil {
			t.Fatalf("BuildPrintIgnoreTree: %v", err)
		}
		if !eval(t, expr, "main.go", 0) {
			t.Error("main.go should pass")
		}
		if eval(t, expr, "scratch.tmp", 0) {
			t.Error("*.tmp should be filtered")
		}
		if eval(t, expr, "scratch.bak", 0) {
			t.Error("*.bak should be filtered")
		}
	})

	t.Run("both sides: AND(prints) AND NOT(OR(ignores))", func(t *testing.T) {
		expr, err := BuildPrintIgnoreTree(
			[]FilterOptions{{Names: []string{"*.go"}}},
			[]FilterOptions{{Names: []string{"*_test.go"}}},
		)
		if err != nil {
			t.Fatalf("BuildPrintIgnoreTree: %v", err)
		}
		if !eval(t, expr, "main.go", 0) {
			t.Error("main.go should match")
		}
		if eval(t, expr, "main_test.go", 0) {
			t.Error("main_test.go should be filtered by ignore")
		}
		if eval(t, expr, "main.py", 0) {
			t.Error("main.py should fail print segment")
		}
	})

	t.Run("bad flag inside a print segment is tagged with kind+index", func(t *testing.T) {
		min := int64(2048)
		max := int64(1024)
		_, err := BuildPrintIgnoreTree(
			[]FilterOptions{{}, {MinSize: &min, MaxSize: &max}},
			nil,
		)
		if err == nil {
			t.Fatal("expected error from inverted size bounds")
		}
		if !strings.Contains(err.Error(), "print segment #1") {
			t.Errorf("error should tag print segment #1, got %q", err)
		}
	})

	t.Run("bad flag inside an ignore segment is tagged with kind+index", func(t *testing.T) {
		_, err := BuildPrintIgnoreTree(
			nil,
			[]FilterOptions{{Sizes: []string{"not-a-size"}}},
		)
		if err == nil {
			t.Fatal("expected error from malformed size spec")
		}
		if !strings.Contains(err.Error(), "ignore segment #0") {
			t.Errorf("error should tag ignore segment #0, got %q", err)
		}
	})
}

func TestBuildScanIgnore(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		expr, err := BuildScanIgnore(nil)
		if err != nil {
			t.Fatalf("BuildScanIgnore: %v", err)
		}
		if expr != nil {
			t.Fatalf("expected nil expr, got %v", expr)
		}
	})

	t.Run("all-empty options also collapse to nil", func(t *testing.T) {
		expr, err := BuildScanIgnore([]FilterOptions{{}, {}})
		if err != nil {
			t.Fatalf("BuildScanIgnore: %v", err)
		}
		if expr != nil {
			t.Fatalf("expected nil expr, got %v", expr)
		}
	})

	t.Run("single segment matches without negation", func(t *testing.T) {
		expr, err := BuildScanIgnore([]FilterOptions{{Names: []string{"*.tmp"}}})
		if err != nil {
			t.Fatalf("BuildScanIgnore: %v", err)
		}
		info := &EntryInfo{Path: "scratch.tmp"}
		got, err := expr.Evaluate(info.AsFilterEntry(), &FilterContext{})
		if err != nil || !got {
			t.Fatalf("expected match (drop), got %v err %v", got, err)
		}
		other := &EntryInfo{Path: "main.go"}
		got, err = expr.Evaluate(other.AsFilterEntry(), &FilterContext{})
		if err != nil || got {
			t.Fatalf("expected no match, got %v err %v", got, err)
		}
	})

	t.Run("multi-segment OR semantics", func(t *testing.T) {
		expr, err := BuildScanIgnore([]FilterOptions{
			{Names: []string{"*.tmp"}},
			{Names: []string{"*.bak"}},
		})
		if err != nil {
			t.Fatalf("BuildScanIgnore: %v", err)
		}
		for _, p := range []string{"a.tmp", "b.bak"} {
			info := &EntryInfo{Path: p}
			got, err := expr.Evaluate(info.AsFilterEntry(), &FilterContext{})
			if err != nil || !got {
				t.Fatalf("%s: expected match, got %v err %v", p, got, err)
			}
		}
		info := &EntryInfo{Path: "main.go"}
		got, err := expr.Evaluate(info.AsFilterEntry(), &FilterContext{})
		if err != nil || got {
			t.Fatalf("main.go: expected no match, got %v err %v", got, err)
		}
	})

	t.Run("bad flag bubbles up tagged with index", func(t *testing.T) {
		_, err := BuildScanIgnore([]FilterOptions{{Sizes: []string{"not-a-size"}}})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "ignore segment #0") {
			t.Errorf("error should tag ignore segment #0, got %q", err)
		}
	})
}
