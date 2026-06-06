package dircachefilehash

import (
	"strings"
	"testing"
)

// findChild returns the named direct child of n, or nil.
func findChild(n *Node, label string) *Node {
	for _, c := range n.Children {
		if c.Label == label {
			return c
		}
	}
	return nil
}

// descend walks a slash path of labels from the root, failing the test
// if any component is missing.
func descend(t *testing.T, root *Node, path string) *Node {
	t.Helper()
	cur := root
	for comp := range strings.SplitSeq(path, "/") {
		next := findChild(cur, comp)
		if next == nil {
			t.Fatalf("descend: %q missing at %q", path, comp)
		}
		cur = next
	}
	return cur
}

// TC-1 (AC5): directory Stats equal the sum of their children, up to the root.
func TestBuildTree_AggregationSumsUp(t *testing.T) {
	entries := []treeEntry{
		{Path: "a/b/x.txt", Size: 100},
		{Path: "a/b/y.txt", Size: 200},
		{Path: "a/c/z.txt", Size: 50},
		{Path: "top.txt", Size: 7},
	}
	tree := buildTreeFromEntries(entries, ChangeSet{})

	root := tree.Root
	if root.Stats.Files != 4 {
		t.Errorf("root Files = %d, want 4", root.Stats.Files)
	}
	if root.Stats.Bytes != 357 {
		t.Errorf("root Bytes = %d, want 357", root.Stats.Bytes)
	}

	a := descend(t, root, "a")
	if a.Stats.Files != 3 || a.Stats.Bytes != 350 {
		t.Errorf("a Stats = %+v, want Files=3 Bytes=350", a.Stats)
	}
	ab := descend(t, root, "a/b")
	if ab.Stats.Files != 2 || ab.Stats.Bytes != 300 {
		t.Errorf("a/b Stats = %+v, want Files=2 Bytes=300", ab.Stats)
	}
	if !ab.IsDir {
		t.Errorf("a/b should be a directory")
	}
}

// TC-2 (AC5/FR7): category assignment from ChangeSet membership; the
// per-category invariant Files == Added+Modified+Unchanged holds.
func TestBuildTree_CategoryAssignment(t *testing.T) {
	entries := []treeEntry{
		{Path: "added.txt", Size: 1},
		{Path: "mod.txt", Size: 2},
		{Path: "same.txt", Size: 3},
	}
	cs := ChangeSet{Added: []string{"added.txt"}, Modified: []string{"mod.txt"}}
	tree := buildTreeFromEntries(entries, cs)
	s := tree.Root.Stats

	if s.Added != 1 || s.Modified != 1 || s.Unchanged != 1 {
		t.Errorf("counts = %+v, want Added=1 Modified=1 Unchanged=1", s)
	}
	if s.Files != s.Added+s.Modified+s.Unchanged {
		t.Errorf("Files=%d != Added+Modified+Unchanged=%d", s.Files, s.Added+s.Modified+s.Unchanged)
	}
	if c := findChild(tree.Root, "added.txt"); c == nil || c.Cat != Added {
		t.Errorf("added.txt category wrong: %+v", c)
	}
	if c := findChild(tree.Root, "mod.txt"); c == nil || c.Cat != Modified {
		t.Errorf("mod.txt category wrong: %+v", c)
	}
}

// TC-3 (correctness: deleted-union, update-full): a deleted path absent
// from the merged entries is synthesised count-only and propagates up.
func TestBuildTree_DeletedUnion_AbsentFromEntries(t *testing.T) {
	entries := []treeEntry{
		{Path: "a/b/live.txt", Size: 10},
	}
	cs := ChangeSet{Deleted: []string{"a/b/gone.txt"}}
	tree := buildTreeFromEntries(entries, cs)

	gone := descend(t, tree.Root, "a/b/gone.txt")
	if gone.Cat != Deleted {
		t.Errorf("gone.txt Cat = %v, want Deleted", gone.Cat)
	}
	if gone.Stats.Files != 0 || gone.Stats.Bytes != 0 {
		t.Errorf("synthesised deleted node should be count-only, got %+v", gone.Stats)
	}
	if gone.Stats.Deleted != 1 {
		t.Errorf("gone.txt Stats.Deleted = %d, want 1", gone.Stats.Deleted)
	}
	ab := descend(t, tree.Root, "a/b")
	if ab.Stats.Deleted != 1 {
		t.Errorf("a/b Stats.Deleted = %d, want 1 (propagated)", ab.Stats.Deleted)
	}
	if ab.Stats.Files != 1 {
		t.Errorf("a/b Stats.Files = %d, want 1 (live only)", ab.Stats.Files)
	}
	if tree.Root.Stats.Deleted != 1 {
		t.Errorf("root Stats.Deleted = %d, want 1", tree.Root.Stats.Deleted)
	}
}

// TC-4 (correctness: deleted via flag, status): an entry already flagged
// IsDeleted AND named in ChangeSet.Deleted yields exactly one node.
func TestBuildTree_DeletedFlag_NoDoubleCount(t *testing.T) {
	entries := []treeEntry{
		{Path: "gone.txt", Size: 99, Deleted: true},
	}
	cs := ChangeSet{Deleted: []string{"gone.txt"}}
	tree := buildTreeFromEntries(entries, cs)

	var goneCount int
	for _, c := range tree.Root.Children {
		if c.Label == "gone.txt" {
			goneCount++
		}
	}
	if goneCount != 1 {
		t.Fatalf("expected exactly 1 gone.txt node, got %d", goneCount)
	}
	if tree.Root.Stats.Deleted != 1 {
		t.Errorf("root Stats.Deleted = %d, want 1 (no double count)", tree.Root.Stats.Deleted)
	}
	if tree.Root.Stats.Files != 0 {
		t.Errorf("deleted-flag file should not count in Files, got %d", tree.Root.Stats.Files)
	}
}

// TC-5 (FR8): empty entries + empty ChangeSet → valid zeroed tree.
func TestBuildTree_Empty(t *testing.T) {
	tree := buildTreeFromEntries(nil, ChangeSet{})
	if tree == nil || tree.Root == nil {
		t.Fatal("empty build returned nil tree/root")
	}
	if !tree.Root.IsDir {
		t.Error("root should be a directory")
	}
	if len(tree.Root.Children) != 0 {
		t.Errorf("root should have no children, got %d", len(tree.Root.Children))
	}
	if (tree.Root.Stats != Stats{}) {
		t.Errorf("root Stats should be zero, got %+v", tree.Root.Stats)
	}
}

// Children are emitted in canonical name-ascending order at every level.
func TestBuildTree_CanonicalOrder(t *testing.T) {
	entries := []treeEntry{
		{Path: "zeta.txt", Size: 1},
		{Path: "alpha.txt", Size: 1},
		{Path: "mid/b.txt", Size: 1},
		{Path: "mid/a.txt", Size: 1},
	}
	tree := buildTreeFromEntries(entries, ChangeSet{})
	labels := make([]string, 0, len(tree.Root.Children))
	for _, c := range tree.Root.Children {
		labels = append(labels, c.Label)
	}
	want := []string{"alpha.txt", "mid", "zeta.txt"}
	if strings.Join(labels, ",") != strings.Join(want, ",") {
		t.Errorf("root order = %v, want %v", labels, want)
	}
	mid := descend(t, tree.Root, "mid")
	if mid.Children[0].Label != "a.txt" || mid.Children[1].Label != "b.txt" {
		t.Errorf("mid order = %v, want [a.txt b.txt]", []string{mid.Children[0].Label, mid.Children[1].Label})
	}
}

// TC-6 (AC7): sanitiseLabel neutralises control/escape/DEL/C1/invalid-UTF-8
// bytes — including bytes OUTSIDE any enumerated CSI set, so a regression
// to a literal blocklist fails.
func TestSanitiseLabel_RejectByDefault(t *testing.T) {
	cases := []struct {
		name string
		in   string
		// substrings that must NOT survive in the output as raw bytes
		rawForbidden []string
	}{
		{"clearscreen CSI", "evil\x1b[2Jname", []string{"\x1b"}},
		{"OSC title", "t\x1b]0;pwned\x07x", []string{"\x1b", "\x07"}},
		{"DEL byte", "a\x7fb", []string{"\x7f"}},
		{"lone C1 CSI", "a\x9bb", []string{"\x9b"}},
		{"carriage return", "a\rb", []string{"\r"}},
		{"backspace", "a\bb", []string{"\b"}},
		{"tab+newline", "a\tb\nc", []string{"\t", "\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := sanitiseLabel(tc.in)
			for _, bad := range tc.rawForbidden {
				if strings.Contains(out, bad) {
					t.Errorf("output %q still contains raw %q", out, bad)
				}
			}
			// Every rune in the output must itself be safe.
			for _, r := range out {
				if !safeRune(r) {
					t.Errorf("output %q contains unsafe rune %U", out, r)
				}
			}
		})
	}

	// Invalid UTF-8 byte must be escaped, not passed through.
	invalid := "good\xffbad"
	out := sanitiseLabel(invalid)
	if strings.ContainsRune(out, 0xff) {
		t.Errorf("invalid byte survived: %q", out)
	}
	if !strings.Contains(out, `\xff`) {
		t.Errorf("invalid byte not escaped as \\xff: %q", out)
	}

	// A clean label is returned unchanged (fast path).
	clean := "normal-file_name.txt"
	if got := sanitiseLabel(clean); got != clean {
		t.Errorf("clean label altered: %q -> %q", clean, got)
	}
	// Unicode printable runes are preserved.
	unicodeName := "café-déjà.txt"
	if got := sanitiseLabel(unicodeName); got != unicodeName {
		t.Errorf("unicode label altered: %q -> %q", unicodeName, got)
	}
}
