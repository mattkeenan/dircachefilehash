package tui

import (
	"sort"
	"strconv"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// sortKey selects the active child-ordering metric. Counts come straight
// from the already-aggregated dcfh.Stats (no new data, no re-read) —
// switching key is a pure render-layer operation (KD8/FR10).
type sortKey int

const (
	// sortChangeBytes: Added+Modified+Deleted *bytes*, descending by
	// default — the default metric, ranking "where did the most change
	// happen by volume" (large deletions included). Zero value/default.
	sortChangeBytes sortKey = iota
	// sortChangeFiles: Added+Modified+Deleted *counts* — the former
	// "change" metric, now reachable via the 'f' key.
	sortChangeFiles
	sortAdded
	sortModified
	sortDeleted
	sortName
)

// label is the short name shown in the header sort indicator.
func (k sortKey) label() string {
	switch k {
	case sortChangeFiles:
		return "change_files"
	case sortAdded:
		return "added"
	case sortModified:
		return "modified"
	case sortDeleted:
		return "deleted"
	case sortName:
		return "name"
	default:
		return "change_bytes"
	}
}

// metric returns the value a node contributes for key, as int64 so byte
// sums over a large subtree never truncate/overflow (KD4). Count keys
// return int64(count); name has no value.
func metric(n *dcfh.Node, key sortKey) int64 {
	switch key {
	case sortChangeFiles:
		return int64(n.Stats.Added + n.Stats.Modified + n.Stats.Deleted)
	case sortAdded:
		return int64(n.Stats.Added)
	case sortModified:
		return int64(n.Stats.Modified)
	case sortDeleted:
		return int64(n.Stats.Deleted)
	case sortName:
		return 0
	default: // sortChangeBytes
		return n.Stats.AddedBytes + n.Stats.ModifiedBytes + n.Stats.DeletedBytes
	}
}

// columnText formats the right-aligned per-row value for the active sort
// key, reusing metric() so the displayed number can never diverge from the
// ordering. The change_bytes value formats as a human size; count keys
// format as a decimal integer. name has no numeric key (metric() returns 0
// for it), so it falls back to the change_bytes value (change volume).
//
// Precondition: n is non-nil (rebuildRows never enqueues a nil node).
func columnText(n *dcfh.Node, key sortKey) string {
	if key == sortName {
		key = sortChangeBytes // name → change volume; metric(name) is 0
	}
	v := metric(n, key)
	if key == sortChangeBytes {
		return dcfh.FormatHumanSize(v)
	}
	return strconv.FormatInt(v, 10)
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
		return sortChangeBytes, true
	case 'f':
		return sortChangeFiles, true
	case 'a':
		return sortAdded, true
	case 'm':
		return sortModified, true
	case 'd':
		return sortDeleted, true
	case 'n':
		return sortName, true
	default:
		return sortChangeBytes, false
	}
}
