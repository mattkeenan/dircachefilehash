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

// nodeBytes builds a labelled node with per-category byte sums (counts
// left zero — change_bytes reads only the byte fields).
func nodeBytes(label string, addedBytes, modifiedBytes, deletedBytes int64) *dcfh.Node {
	return &dcfh.Node{
		Label: label,
		IsDir: true,
		Stats: dcfh.Stats{
			AddedBytes:    addedBytes,
			ModifiedBytes: modifiedBytes,
			DeletedBytes:  deletedBytes,
		},
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

	if got := order(sortNodes(in, sortChangeFiles, false)); got != "b,d,a,c" {
		t.Errorf("sortChangeFiles desc = %q, want b,d,a,c", got)
	}
	if got := order(sortNodes(in, sortChangeFiles, true)); got != "c,a,b,d" {
		t.Errorf("sortChangeFiles reversed = %q, want c,a,b,d", got)
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
	_ = sortNodes(in, sortChangeBytes, false)
	if after := order(in); after != before {
		t.Errorf("input mutated: %q -> %q", before, after)
	}
}

func TestKeyForRune(t *testing.T) {
	cases := map[rune]sortKey{
		'c': sortChangeBytes,
		'f': sortChangeFiles,
		'a': sortAdded,
		'm': sortModified,
		'd': sortDeleted,
		'n': sortName,
	}
	for r, want := range cases {
		if got, ok := keyForRune(r); !ok || got != want {
			t.Errorf("keyForRune(%q) = %v,%v want %v,true", r, got, ok, want)
		}
	}
	if _, ok := keyForRune('x'); ok {
		t.Errorf("keyForRune('x') should be unrecognised")
	}
}

// TC-7/TC-6 (FR1/FR5/NFR5): change_bytes orders by the byte sum, default
// descending, and is int64-correct for a subtree summing past 2³¹.
func TestSortNodes_ChangeBytes(t *testing.T) {
	const big = int64(3) << 30 // 3 GiB > MaxInt32
	in := []*dcfh.Node{
		nodeBytes("a", 100, 0, 0),  // 100
		nodeBytes("b", 0, 0, big),  // 3 GiB (deletion dominates)
		nodeBytes("c", 50, 50, 50), // 150
		nodeBytes("d", 0, 0, 0),    // 0
	}
	if got := order(sortNodes(in, sortChangeBytes, false)); got != "b,c,a,d" {
		t.Errorf("change_bytes desc = %q, want b,c,a,d", got)
	}
	// int64-correct: the 3 GiB deletion must rank first, not overflow to a
	// negative/truncated value behind the smaller siblings.
	if metric(in[1], sortChangeBytes) != big {
		t.Errorf("metric big = %d, want %d", metric(in[1], sortChangeBytes), big)
	}
	if got := order(sortNodes(in, sortChangeBytes, true)); got != "d,a,c,b" {
		t.Errorf("change_bytes reversed = %q, want d,a,c,b", got)
	}
}

// TC-8 (FR6/AC2): the rename surfaces in label() output; no metric is
// labelled with the bare word "change".
func TestSortKeyLabels(t *testing.T) {
	want := map[sortKey]string{
		sortChangeBytes: "change_bytes",
		sortChangeFiles: "change_files",
		sortAdded:       "added",
		sortModified:    "modified",
		sortDeleted:     "deleted",
		sortName:        "name",
	}
	for k, lbl := range want {
		if got := k.label(); got != lbl {
			t.Errorf("label(%d) = %q, want %q", k, got, lbl)
		}
		if got := k.label(); got == "change" {
			t.Errorf("label(%d) is the bare metric name %q", k, got)
		}
	}
}
