package tui

import (
	"strings"
	"testing"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// node builds a labelled node with the given per-category counts.
func node(label string, added, modified, deleted int) *dcfh.Node {
	return &dcfh.Node{
		Label: label,
		IsDir: true,
		Stats: dcfh.Stats{Added: added, Modified: modified, Deleted: deleted},
	}
}

func order(nodes []*dcfh.Node) string {
	labels := make([]string, len(nodes))
	for i, n := range nodes {
		labels[i] = n.Label
	}
	return strings.Join(labels, ",")
}

// TC-13b (AC9/FR10): comparators order by the selected key; total-change
// = add+mod+del; ties break by name-asc; reverse flips; name is stable.
func TestSortNodes_Comparators(t *testing.T) {
	// total-change: a=3, b=5, c=1, d=5 (tie b/d → name asc)
	in := []*dcfh.Node{
		node("a", 1, 1, 1), // 3
		node("b", 5, 0, 0), // 5
		node("c", 0, 1, 0), // 1
		node("d", 0, 2, 3), // 5
	}

	if got := order(sortNodes(in, sortChange, false)); got != "b,d,a,c" {
		t.Errorf("sortChange desc = %q, want b,d,a,c", got)
	}
	if got := order(sortNodes(in, sortChange, true)); got != "c,a,b,d" {
		t.Errorf("sortChange reversed = %q, want c,a,b,d", got)
	}

	// added: b=5 > a=1 > (c=0,d=0 → name asc)
	if got := order(sortNodes(in, sortAdded, false)); got != "b,a,c,d" {
		t.Errorf("sortAdded desc = %q, want b,a,c,d", got)
	}

	// deleted: d=3 > a=1 > (b=0,c=0 → name asc)
	if got := order(sortNodes(in, sortDeleted, false)); got != "d,a,b,c" {
		t.Errorf("sortDeleted desc = %q, want d,a,b,c", got)
	}

	// name asc / desc
	if got := order(sortNodes(in, sortName, false)); got != "a,b,c,d" {
		t.Errorf("sortName asc = %q, want a,b,c,d", got)
	}
	if got := order(sortNodes(in, sortName, true)); got != "d,c,b,a" {
		t.Errorf("sortName desc = %q, want d,c,b,a", got)
	}
}

// The input slice must never be mutated by a sort (the canonical tree
// order is preserved for the next re-sort).
func TestSortNodes_DoesNotMutateInput(t *testing.T) {
	in := []*dcfh.Node{node("z", 0, 0, 0), node("a", 9, 0, 0)}
	before := order(in)
	_ = sortNodes(in, sortChange, false)
	if after := order(in); after != before {
		t.Errorf("input mutated: %q -> %q", before, after)
	}
}

func TestKeyForRune(t *testing.T) {
	cases := map[rune]sortKey{'c': sortChange, 'a': sortAdded, 'm': sortModified, 'd': sortDeleted, 'n': sortName}
	for r, want := range cases {
		if got, ok := keyForRune(r); !ok || got != want {
			t.Errorf("keyForRune(%q) = %v,%v want %v,true", r, got, ok, want)
		}
	}
	if _, ok := keyForRune('x'); ok {
		t.Errorf("keyForRune('x') should be unrecognised")
	}
}
