package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func setupDupesRepo(t *testing.T, files map[string]string) *MetaStore {
	t.Helper()
	sized := make(map[string]sizedFile, len(files))
	for rel, content := range files {
		sized[rel] = sizedFile{content: content}
	}
	return setupDupesRepoSized(t, sized)
}

func TestFindDuplicates_RealDuplicates(t *testing.T) {
	ms := setupDupesRepo(t, map[string]string{
		"a.txt":     "shared A",
		"b.txt":     "shared A",
		"sub/c.txt": "shared A",
		"d.txt":     "shared B",
		"sub/e.txt": "shared B",
		"f.txt":     "unique",
		"sub/g.txt": "another unique",
	})
	defer func() { _ = ms.Close() }()

	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Exclusive: true})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("want 2 duplicate groups, got %d: %+v", len(groups), groups)
	}
	// Groups must be sorted by hash for determinism.
	if groups[0].Hash > groups[1].Hash {
		t.Errorf("groups not sorted by hash: %q then %q", groups[0].Hash, groups[1].Hash)
	}
	byContent := map[string][]string{
		"shared A": {"a.txt", "b.txt", "sub/c.txt"},
		"shared B": {"d.txt", "sub/e.txt"},
	}
	matched := 0
	for _, g := range groups {
		for _, want := range byContent {
			if len(g.Files) == len(want) && slices.Equal(g.Files, want) {
				matched++
				break
			}
		}
		// Files within a group must be in path-sorted order
		// (skiplist iteration is path-sorted, so no per-group sort).
		if !slices.IsSorted(g.Files) {
			t.Errorf("group %q files not sorted: %v", g.Hash, g.Files)
		}
		if g.Count != len(g.Files) {
			t.Errorf("Count %d != len(Files) %d", g.Count, len(g.Files))
		}
	}
	if matched != 2 {
		t.Errorf("group contents mismatch; groups=%+v", groups)
	}
}

func TestFindDuplicates_NoDuplicates(t *testing.T) {
	ms := setupDupesRepo(t, map[string]string{
		"a": "one",
		"b": "two",
		"c": "three",
	})
	defer func() { _ = ms.Close() }()

	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Exclusive: true})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("want 0 groups, got %d: %+v", len(groups), groups)
	}
}

func TestFindDuplicates_ContextCancellation(t *testing.T) {
	ms := setupDupesRepo(t, map[string]string{
		"a": "x", "b": "x", "c": "x",
	})
	defer func() { _ = ms.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the call
	_, err := ms.FindDuplicates(ctx, ms.scanRun(), map[string]string{}, DupeFilter{Exclusive: true})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

// dupesPathFilterFixture seeds a repo with three dupe groups spread
// across a/, b/, c/ so each test can pick which prefixes to pass.
//
//	group1: a/x, a/y, b/x    (cross-dir)
//	group2: b/y, c/x         (cross-dir, no member in a/)
//	group3: c/y, c/z         (entirely inside c/)
func dupesPathFilterFixture(t *testing.T) *MetaStore {
	t.Helper()
	return setupDupesRepo(t, map[string]string{
		"a/x": "g1", "a/y": "g1", "b/x": "g1",
		"b/y": "g2", "c/x": "g2",
		"c/y": "g3", "c/z": "g3",
		"a/solo": "unique-a",
		"b/solo": "unique-b",
	})
}

func groupFiles(groups []DuplicateGroup) [][]string {
	out := make([][]string, len(groups))
	for i, g := range groups {
		out[i] = g.Files
	}
	return out
}

func TestFindDuplicates_PathFilter_ZeroPaths(t *testing.T) {
	ms := dupesPathFilterFixture(t)
	defer func() { _ = ms.Close() }()

	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Exclusive: true})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("want 3 groups (whole repo), got %d: %+v", len(groups), groups)
	}
}

func TestFindDuplicates_PathFilter_ExclusiveOneDir(t *testing.T) {
	ms := dupesPathFilterFixture(t)
	defer func() { _ = ms.Close() }()

	// Only c/ — group3 is fully inside, group1 has members outside c/
	// so its in-c/ count drops to 0, group2 drops to a singleton.
	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Paths: []string{"c/"}, Exclusive: true})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d: %v", len(groups), groupFiles(groups))
	}
	if !slices.Equal(groups[0].Files, []string{"c/y", "c/z"}) {
		t.Errorf("want [c/y c/z], got %v", groups[0].Files)
	}
}

func TestFindDuplicates_PathFilter_ExclusiveTwoDirs(t *testing.T) {
	ms := dupesPathFilterFixture(t)
	defer func() { _ = ms.Close() }()

	// a/ ∪ c/. group1 loses its b/x member → still dup (a/x,a/y).
	// group2 loses a/… (none), keeps c/x only → singleton, dropped.
	// group3 stays.
	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Paths: []string{"a/", "c/"}, Exclusive: true})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	got := groupFiles(groups)
	want := [][]string{{"a/x", "a/y"}, {"c/y", "c/z"}}
	// Order by Hash is stable but test-independent; match as set.
	if len(got) != len(want) {
		t.Fatalf("want %d groups, got %d: %v", len(want), len(got), got)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if slices.Equal(g, w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing group %v in %v", w, got)
		}
	}
}

func TestFindDuplicates_PathFilter_NonExclusive(t *testing.T) {
	ms := dupesPathFilterFixture(t)
	defer func() { _ = ms.Close() }()

	// --exclusive=no with a/: cross-dir group1 (has a/x,a/y,b/x) is
	// reported in full; group2 has no member in a/ so it's dropped;
	// group3 has no member in a/ so it's dropped.
	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Paths: []string{"a/"}, Exclusive: false})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d: %v", len(groups), groupFiles(groups))
	}
	if !slices.Equal(groups[0].Files, []string{"a/x", "a/y", "b/x"}) {
		t.Errorf("want [a/x a/y b/x], got %v", groups[0].Files)
	}
}

// sizedFile lets a test pin the on-disk size and mtime of a fixture
// file. Zero-valued fields no-op, so setupDupesRepoSized subsumes the
// content-only setupDupesRepo.
type sizedFile struct {
	content string
	size    int
	mtime   time.Time
}

// setupDupesRepoSized seeds a repo with the given fixture files, plus
// optional extraLinks maps: each linkPath → targetPath triggers an
// os.Link after files are written, before the initial Update, so the
// scanner walks both names. targetPath must be in files.
func setupDupesRepoSized(t *testing.T, files map[string]sizedFile, extraLinks ...map[string]string) *MetaStore {
	t.Helper()
	root := t.TempDir()
	for rel, spec := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		body := []byte(spec.content)
		if spec.size > len(body) {
			pad := make([]byte, spec.size-len(body))
			body = append(body, pad...)
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
		if !spec.mtime.IsZero() {
			if err := os.Chtimes(full, spec.mtime, spec.mtime); err != nil {
				t.Fatalf("chtimes %s: %v", full, err)
			}
		}
	}
	for _, linkMap := range extraLinks {
		for linkRel, targetRel := range linkMap {
			linkFull := filepath.Join(root, linkRel)
			targetFull := filepath.Join(root, targetRel)
			if err := os.MkdirAll(filepath.Dir(linkFull), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", filepath.Dir(linkFull), err)
			}
			if err := os.Link(targetFull, linkFull); err != nil {
				t.Fatalf("link %s -> %s: %v", linkRel, targetRel, err)
			}
		}
	}
	ms := NewMetaStore(root, filepath.Join(root, ".dcfh"))
	if err := ms.Update(context.Background(), ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	return ms
}

func u64(v uint64) *uint64 { return &v }

// mustFilter is a test helper: build a FilterExpr from FilterOptions or
// fail loudly. Lets DupeFilter literals stay compact in the size/date
// test cases below.
func mustFilter(t *testing.T, opts FilterOptions) FilterExpr {
	t.Helper()
	expr, err := BuildFilter(opts)
	if err != nil {
		t.Fatalf("BuildFilter: %v", err)
	}
	return expr
}

func TestFindDuplicates_SizeFilter_MinDropsBelowTwo(t *testing.T) {
	// group1 (small): two tiny duplicates → dropped by --min-size
	// group2 (mixed): one small + one large dup of the same content;
	//   with --min-size 512 only the large member survives → singleton
	//   → regression gate that filter-before-bucketing works.
	// group3 (large): two large duplicates → survives.
	mtime := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ms := setupDupesRepoSized(t, map[string]sizedFile{
		"small1":  {content: "A", size: 16, mtime: mtime},
		"small2":  {content: "A", size: 16, mtime: mtime},
		"mixed_s": {content: "MIX", size: 16, mtime: mtime},
		"mixed_L": {content: "MIX", size: 1024, mtime: mtime},
		"large1":  {content: "BIG", size: 4096, mtime: mtime},
		"large2":  {content: "BIG", size: 4096, mtime: mtime},
	})
	defer func() { _ = ms.Close() }()

	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{},
		DupeFilter{Exclusive: true, Predicate: mustFilter(t, FilterOptions{MinSize: u64(512)})})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group (large only), got %d: %v", len(groups), groupFiles(groups))
	}
	if !slices.Equal(groups[0].Files, []string{"large1", "large2"}) {
		t.Errorf("want [large1 large2], got %v", groups[0].Files)
	}
	// Note the mixed group is identical-content across a small+large
	// pair; the small member is dropped by the filter, leaving the
	// large member alone, and the group therefore isn't emitted.
	// That's the load-bearing assertion: we never see {mixed_L}.
}

func TestFindDuplicates_SizeFilter_MaxOnly(t *testing.T) {
	mtime := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ms := setupDupesRepoSized(t, map[string]sizedFile{
		"small1": {content: "A", size: 16, mtime: mtime},
		"small2": {content: "A", size: 16, mtime: mtime},
		"large1": {content: "BIG", size: 4096, mtime: mtime},
		"large2": {content: "BIG", size: 4096, mtime: mtime},
	})
	defer func() { _ = ms.Close() }()

	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{},
		DupeFilter{Exclusive: true, Predicate: mustFilter(t, FilterOptions{MaxSize: u64(100)})})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 1 || !slices.Equal(groups[0].Files, []string{"small1", "small2"}) {
		t.Fatalf("want [small1 small2], got %v", groupFiles(groups))
	}
}

func TestFindDuplicates_SizeFilter_MinEqualsMax(t *testing.T) {
	mtime := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ms := setupDupesRepoSized(t, map[string]sizedFile{
		"a": {content: "A", size: 100, mtime: mtime},
		"b": {content: "A", size: 100, mtime: mtime},
		"c": {content: "C", size: 200, mtime: mtime},
		"d": {content: "C", size: 200, mtime: mtime},
	})
	defer func() { _ = ms.Close() }()

	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{},
		DupeFilter{Exclusive: true, Predicate: mustFilter(t, FilterOptions{MinSize: u64(100), MaxSize: u64(100)})})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 1 || !slices.Equal(groups[0].Files, []string{"a", "b"}) {
		t.Fatalf("want [a b], got %v", groupFiles(groups))
	}
}

func TestFindDuplicates_DateFilter_Range(t *testing.T) {
	jan := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	// Two groups of same content but spanning different months.
	ms := setupDupesRepoSized(t, map[string]sizedFile{
		"j1": {content: "shared", size: 32, mtime: jan},
		"j2": {content: "shared", size: 32, mtime: jan},
		"f1": {content: "feb", size: 32, mtime: feb},
		"f2": {content: "feb", size: 32, mtime: feb},
		"m1": {content: "mar", size: 32, mtime: mar},
		"m2": {content: "mar", size: 32, mtime: mar},
	})
	defer func() { _ = ms.Close() }()

	// [Feb 1, Mar 1): only feb files pass.
	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{},
		DupeFilter{Exclusive: true, Predicate: mustFilter(t, FilterOptions{StartDate: start, EndDate: end})})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group (feb), got %d: %v", len(groups), groupFiles(groups))
	}
	if !slices.Equal(groups[0].Files, []string{"f1", "f2"}) {
		t.Errorf("want [f1 f2], got %v", groups[0].Files)
	}
}

func TestFindDuplicates_DateFilter_BoundaryInclusivity(t *testing.T) {
	// Exact start boundary: inclusive. Exact end boundary: excluded.
	boundary := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	ms := setupDupesRepoSized(t, map[string]sizedFile{
		"in1":  {content: "g1", size: 32, mtime: boundary},
		"in2":  {content: "g1", size: 32, mtime: boundary},
		"out1": {content: "g2", size: 32, mtime: boundary.Add(24 * time.Hour)},
		"out2": {content: "g2", size: 32, mtime: boundary.Add(24 * time.Hour)},
	})
	defer func() { _ = ms.Close() }()

	// [boundary, boundary+24h): only in-files qualify.
	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{},
		DupeFilter{Exclusive: true, Predicate: mustFilter(t, FilterOptions{StartDate: boundary, EndDate: boundary.Add(24 * time.Hour)})})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 1 || !slices.Equal(groups[0].Files, []string{"in1", "in2"}) {
		t.Fatalf("want [in1 in2], got %v", groupFiles(groups))
	}
}

func TestFindDuplicates_DateFilter_BerlinDST(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("Europe/Berlin tzdata unavailable: %v", err)
	}
	// Spring-forward 2026: 01:59 CET → 03:00 CEST on March 29.
	// Files on either side of the transition.
	before := time.Date(2026, 3, 29, 1, 30, 0, 0, berlin) // CET, 00:30 UTC
	after := time.Date(2026, 3, 29, 4, 30, 0, 0, berlin)  // CEST, 02:30 UTC
	ms := setupDupesRepoSized(t, map[string]sizedFile{
		"pre1":  {content: "pre", size: 32, mtime: before},
		"pre2":  {content: "pre", size: 32, mtime: before},
		"post1": {content: "post", size: 32, mtime: after},
		"post2": {content: "post", size: 32, mtime: after},
	})
	defer func() { _ = ms.Close() }()

	// Ask for [March 29, March 30) in Berlin wall time — spans the
	// transition. Both groups should come through regardless of DST.
	start := time.Date(2026, 3, 29, 0, 0, 0, 0, berlin)
	end := time.Date(2026, 3, 30, 0, 0, 0, 0, berlin)
	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{},
		DupeFilter{Exclusive: true, Predicate: mustFilter(t, FilterOptions{StartDate: start, EndDate: end})})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("want 2 groups across DST, got %d: %v", len(groups), groupFiles(groups))
	}
	// A narrower range that only covers CET hours (pre-transition)
	// must exclude the post-transition group entirely.
	narrow, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{},
		DupeFilter{
			Exclusive: true,
			Predicate: mustFilter(t, FilterOptions{
				StartDate: time.Date(2026, 3, 29, 0, 0, 0, 0, berlin),
				EndDate:   time.Date(2026, 3, 29, 2, 0, 0, 0, berlin), // pre-DST
			}),
		})
	if err != nil {
		t.Fatalf("narrow FindDuplicates: %v", err)
	}
	if len(narrow) != 1 || !slices.Equal(narrow[0].Files, []string{"pre1", "pre2"}) {
		t.Fatalf("want [pre1 pre2] in narrow window, got %v", groupFiles(narrow))
	}
}

func TestFindDuplicates_CombinedFilters(t *testing.T) {
	feb := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	// Dupes scattered across a/ and b/ with varied sizes and months.
	ms := setupDupesRepoSized(t, map[string]sizedFile{
		"a/feb_big_1": {content: "ab", size: 2048, mtime: feb},
		"a/feb_big_2": {content: "ab", size: 2048, mtime: feb},
		"a/feb_sml_1": {content: "as", size: 16, mtime: feb}, // drops below min-size
		"a/feb_sml_2": {content: "as", size: 16, mtime: feb},
		"a/mar_big_1": {content: "am", size: 2048, mtime: mar}, // outside date range
		"a/mar_big_2": {content: "am", size: 2048, mtime: mar},
		"b/feb_big_1": {content: "bb", size: 2048, mtime: feb}, // outside path
		"b/feb_big_2": {content: "bb", size: 2048, mtime: feb},
	})
	defer func() { _ = ms.Close() }()

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	groups, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{},
		DupeFilter{
			Paths:     []string{"a/"},
			Exclusive: true,
			Predicate: mustFilter(t, FilterOptions{
				MinSize:   u64(512),
				StartDate: start,
				EndDate:   end,
			}),
		})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group (a/feb_big_*), got %d: %v", len(groups), groupFiles(groups))
	}
	got := groups[0].Files
	for _, f := range got {
		if !strings.HasPrefix(f, "a/feb_big_") {
			t.Errorf("unexpected file %q in combined-filter result", f)
		}
	}
	if len(got) != 2 {
		t.Errorf("want 2 files, got %v", got)
	}
}

// setupDupesRepoWithLinks seeds a repo like setupDupesRepo but also
// hardlinks each linkPath → targetPath (targetPath must be in files),
// so both land in the index sharing (Dev, Ino).
func setupDupesRepoWithLinks(t *testing.T, files map[string]string, links map[string]string) *MetaStore {
	t.Helper()
	sized := make(map[string]sizedFile, len(files))
	for rel, content := range files {
		sized[rel] = sizedFile{content: content}
	}
	return setupDupesRepoSized(t, sized, links)
}

func TestFindDuplicates_IgnoreHardlinks_PureHardlinkGroup(t *testing.T) {
	// Two entries, same content, same inode (hard linked).
	// Without the flag: one group of 2. With the flag: no group.
	ms := setupDupesRepoWithLinks(t,
		map[string]string{"a.txt": "shared"},
		map[string]string{"b.txt": "a.txt"},
	)
	defer func() { _ = ms.Close() }()

	off, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Exclusive: true})
	if err != nil {
		t.Fatalf("FindDuplicates (off): %v", err)
	}
	if len(off) != 1 || !slices.Equal(off[0].Files, []string{"a.txt", "b.txt"}) {
		t.Fatalf("flag off: want [a.txt b.txt], got %v", groupFiles(off))
	}

	on, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Exclusive: true, IgnoreHardlinks: true})
	if err != nil {
		t.Fatalf("FindDuplicates (on): %v", err)
	}
	if len(on) != 0 {
		t.Errorf("flag on: want no groups, got %v", groupFiles(on))
	}
}

func TestFindDuplicates_IgnoreHardlinks_MixedGroup(t *testing.T) {
	// Three entries, same content: a.txt and b.txt hard linked;
	// c.txt is an independent copy. Flag off: group of 3.
	// Flag on: group of 2 (representative hardlink + c.txt).
	ms := setupDupesRepoWithLinks(t,
		map[string]string{
			"a.txt": "shared",
			"c.txt": "shared",
		},
		map[string]string{"b.txt": "a.txt"},
	)
	defer func() { _ = ms.Close() }()

	off, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Exclusive: true})
	if err != nil {
		t.Fatalf("FindDuplicates (off): %v", err)
	}
	if len(off) != 1 || !slices.Equal(off[0].Files, []string{"a.txt", "b.txt", "c.txt"}) {
		t.Fatalf("flag off: want [a.txt b.txt c.txt], got %v", groupFiles(off))
	}

	on, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Exclusive: true, IgnoreHardlinks: true})
	if err != nil {
		t.Fatalf("FindDuplicates (on): %v", err)
	}
	// The hardlink pair collapses to its first path-sorted member (a.txt),
	// so the surviving group is [a.txt, c.txt].
	if len(on) != 1 || !slices.Equal(on[0].Files, []string{"a.txt", "c.txt"}) {
		t.Fatalf("flag on: want [a.txt c.txt], got %v", groupFiles(on))
	}
}

func TestFindDuplicates_IgnoreHardlinks_AllHardlinked(t *testing.T) {
	// Three paths, all hard linked to the same inode.
	// Flag on: group disappears entirely.
	ms := setupDupesRepoWithLinks(t,
		map[string]string{"a.txt": "shared"},
		map[string]string{
			"b.txt": "a.txt",
			"c.txt": "a.txt",
		},
	)
	defer func() { _ = ms.Close() }()

	off, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Exclusive: true})
	if err != nil {
		t.Fatalf("FindDuplicates (off): %v", err)
	}
	if len(off) != 1 || !slices.Equal(off[0].Files, []string{"a.txt", "b.txt", "c.txt"}) {
		t.Fatalf("flag off: want [a.txt b.txt c.txt], got %v", groupFiles(off))
	}

	on, err := ms.FindDuplicates(context.Background(), ms.scanRun(), map[string]string{}, DupeFilter{Exclusive: true, IgnoreHardlinks: true})
	if err != nil {
		t.Fatalf("FindDuplicates (on): %v", err)
	}
	if len(on) != 0 {
		t.Errorf("flag on: want no groups, got %v", groupFiles(on))
	}
}

func TestDuplicateGroup_Fields(t *testing.T) {
	group := DuplicateGroup{
		Hash:  "abc123def456",
		Files: []string{"file1.txt", "file2.txt", "dir/file3.txt"},
		Count: 3,
	}

	if group.Hash != "abc123def456" {
		t.Errorf("Expected hash 'abc123def456', got '%s'", group.Hash)
	}

	if len(group.Files) != 3 {
		t.Errorf("Expected 3 files, got %d", len(group.Files))
	}

	if group.Count != 3 {
		t.Errorf("Expected count 3, got %d", group.Count)
	}

	expectedFiles := []string{"file1.txt", "file2.txt", "dir/file3.txt"}
	for i, expected := range expectedFiles {
		if group.Files[i] != expected {
			t.Errorf("Expected file[%d] '%s', got '%s'", i, expected, group.Files[i])
		}
	}
}

func TestMetaStore_FindDuplicates_EmptyIndex(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Create MetaStore instance
	ms := NewMetaStore(tempDir, tempDir)
	defer func() { _ = ms.Close() }()

	// Create empty index
	if err := ms.createEmptyIndex(); err != nil {
		t.Fatalf("Failed to create empty index: %v", err)
	}

	// Test FindDuplicates with empty flags
	flags := map[string]string{}
	duplicates, err := ms.FindDuplicates(context.Background(), ms.scanRun(), flags, DupeFilter{Exclusive: true})
	if err != nil {
		t.Fatalf("FindDuplicates failed: %v", err)
	}

	if len(duplicates) != 0 {
		t.Errorf("Expected no duplicates in empty index, got %d", len(duplicates))
	}

	// Report string copy stats
	copies, accesses, rate := GetStringCopyStats()
	t.Logf("String copy stats: %d copies out of %d accesses (%.2f%% copy rate)", copies, accesses, rate)
}

func TestMetaStore_FindDuplicates_WithFlags(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Create MetaStore instance
	ms := NewMetaStore(tempDir, tempDir)
	defer func() { _ = ms.Close() }()

	// Create empty index
	if err := ms.createEmptyIndex(); err != nil {
		t.Fatalf("Failed to create empty index: %v", err)
	}

	// Test different flag combinations
	testFlags := []map[string]string{
		{},                 // No flags
		{"v": "1"},         // Verbose level 1
		{"v": "2"},         // Verbose level 2
		{"other": "value"}, // Other flags
	}

	for i, flags := range testFlags {
		t.Run("flags_test_"+string(rune(i+'0')), func(t *testing.T) {
			duplicates, err := ms.FindDuplicates(context.Background(), ms.scanRun(), flags, DupeFilter{Exclusive: true})
			if err != nil {
				t.Fatalf("FindDuplicates failed with flags %v: %v", flags, err)
			}

			// With empty index, should always return no duplicates
			if len(duplicates) != 0 {
				t.Errorf("Expected no duplicates with flags %v, got %d", flags, len(duplicates))
			}
		})
	}
}

// Mock test for duplicate detection logic (would need more complex setup for real testing)
func TestDuplicateGroup_CreationAndValidation(t *testing.T) {
	// Test creating a duplicate group
	files := []string{
		"documents/file1.txt",
		"backup/file1_copy.txt",
		"archive/old_file1.txt",
	}

	group := DuplicateGroup{
		Hash:  "sha256:abcdef123456789",
		Files: files,
		Count: len(files),
	}

	// Validate the group
	if group.Count != len(group.Files) {
		t.Errorf("Count mismatch: expected %d, got %d", len(group.Files), group.Count)
	}

	if len(group.Hash) == 0 {
		t.Error("Hash should not be empty")
	}

	if len(group.Files) < 2 {
		t.Error("Duplicate group should have at least 2 files")
	}

	// Test that files are properly stored
	for i, expectedFile := range files {
		if group.Files[i] != expectedFile {
			t.Errorf("File[%d]: expected '%s', got '%s'", i, expectedFile, group.Files[i])
		}
	}
}

func TestDuplicateGroup_EmptyGroup(t *testing.T) {
	// Test handling of empty group
	group := DuplicateGroup{}

	if group.Hash != "" {
		t.Errorf("Empty group hash should be empty, got '%s'", group.Hash)
	}

	if len(group.Files) != 0 {
		t.Errorf("Empty group should have 0 files, got %d", len(group.Files))
	}

	if group.Count != 0 {
		t.Errorf("Empty group count should be 0, got %d", group.Count)
	}
}

func TestDuplicateGroup_SingleFile(t *testing.T) {
	// Test group with single file (not really a duplicate, but test data structure)
	group := DuplicateGroup{
		Hash:  "single_file_hash",
		Files: []string{"single_file.txt"},
		Count: 1,
	}

	if group.Count != 1 {
		t.Errorf("Single file group count should be 1, got %d", group.Count)
	}

	if len(group.Files) != 1 {
		t.Errorf("Single file group should have 1 file, got %d", len(group.Files))
	}

	if group.Files[0] != "single_file.txt" {
		t.Errorf("Expected file 'single_file.txt', got '%s'", group.Files[0])
	}
}

// Test that duplicate groups maintain consistency
func TestDuplicateGroup_Consistency(t *testing.T) {
	testCases := []struct {
		name    string
		hash    string
		files   []string
		count   int
		isValid bool
	}{
		{
			name:    "valid group",
			hash:    "valid_hash",
			files:   []string{"file1.txt", "file2.txt"},
			count:   2,
			isValid: true,
		},
		{
			name:    "count mismatch",
			hash:    "hash",
			files:   []string{"file1.txt", "file2.txt"},
			count:   3,
			isValid: false,
		},
		{
			name:    "empty hash",
			hash:    "",
			files:   []string{"file1.txt", "file2.txt"},
			count:   2,
			isValid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			group := DuplicateGroup{
				Hash:  tc.hash,
				Files: tc.files,
				Count: tc.count,
			}

			// Check basic consistency
			isValid := group.Count == len(group.Files) && group.Hash != ""

			if isValid != tc.isValid {
				t.Errorf("Expected validity %v, got %v", tc.isValid, isValid)
			}
		})
	}
}
