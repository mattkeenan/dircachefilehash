package tui

import (
	"sort"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// sortKey selects the active child-ordering metric. Counts come straight
// from the already-aggregated dcfh.Stats (no new data, no re-read) —
// switching key is a pure render-layer operation (KD8/FR10).
type sortKey int

const (
	// sortChange: Added+Modified+Deleted, descending by default — the
	// "where did the most change happen, regardless of type" metric.
	sortChange sortKey = iota
	sortAdded
	sortModified
	sortDeleted
	sortName
)

// label is the short name shown in the header sort indicator.
func (k sortKey) label() string {
	switch k {
	case sortAdded:
		return "added"
	case sortModified:
		return "modified"
	case sortDeleted:
		return "deleted"
	case sortName:
		return "name"
	default:
		return "change"
	}
}

// metric returns the count a node contributes for key. Name has no count.
func metric(n *dcfh.Node, key sortKey) int {
	switch key {
	case sortAdded:
		return n.Stats.Added
	case sortModified:
		return n.Stats.Modified
	case sortDeleted:
		return n.Stats.Deleted
	case sortChange:
		return n.Stats.Added + n.Stats.Modified + n.Stats.Deleted
	default:
		return 0
	}
}

// sortNodes returns a NEW slice of children ordered by key/reverse — the
// input is never mutated, so the underlying tree stays in canonical
// order. Count metrics default to descending (largest change first);
// name defaults to ascending. The reverse flag flips the primary
// direction. Name-ascending is always the deterministic tiebreak.
func sortNodes(children []*dcfh.Node, key sortKey, reverse bool) []*dcfh.Node {
	out := make([]*dcfh.Node, len(children))
	copy(out, children)
	sort.SliceStable(out, func(i, j int) bool {
		return nodeLess(out[i], out[j], key, reverse)
	})
	return out
}

// nodeLess is the comparator behind sortNodes.
func nodeLess(a, b *dcfh.Node, key sortKey, reverse bool) bool {
	if key == sortName {
		if a.Label != b.Label {
			asc := a.Label < b.Label
			if reverse {
				return !asc
			}
			return asc
		}
		return false
	}
	av, bv := metric(a, key), metric(b, key)
	if av != bv {
		desc := av > bv // count metrics: larger first by default
		if reverse {
			return !desc
		}
		return desc
	}
	// Tiebreak: name ascending, direction-independent and stable.
	return a.Label < b.Label
}

// keyForRune maps a sort-toggle key rune to its sortKey, reporting
// whether the rune is a recognised sort key.
func keyForRune(r rune) (sortKey, bool) {
	switch r {
	case 'c':
		return sortChange, true
	case 'a':
		return sortAdded, true
	case 'm':
		return sortModified, true
	case 'd':
		return sortDeleted, true
	case 'n':
		return sortName, true
	default:
		return sortChange, false
	}
}
