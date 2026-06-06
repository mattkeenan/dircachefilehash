package dircachefilehash

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Category labels a file (or summarises a directory) by how it changed
// between the before/after states the interactive-tree viewer compares.
type Category int

const (
	// Unchanged: present and identical in both before/after.
	Unchanged Category = iota
	// Added: new on disk (untracked) at the after state.
	Added
	// Modified: present in both but content changed.
	Modified
	// Deleted: removed on disk relative to the before state.
	Deleted
)

// Stats aggregates per-subtree counts and per-category byte sums.
//
// Files and Bytes describe the *live* footprint: Files counts only
// non-deleted files and Bytes sums their current sizes. The per-category
// counts (Added/Modified/Deleted/Unchanged) classify every node;
// Deleted is tracked separately and is NOT included in Files/Bytes, so
// the invariant Files == Added+Modified+Unchanged holds for any subtree.
//
// AddedBytes/ModifiedBytes/DeletedBytes record the bytes behind each
// change category for the change_bytes sort (task 12). Added/Modified
// bytes are the file's current size; DeletedBytes separately retains the
// last-known deleted size — sourced from the in-index tombstone (status)
// or ChangeSet.DeletedSizes (update), per KD2/KD3 — and is likewise NOT
// part of Bytes/Files.
type Stats struct {
	Files     int   // live (non-deleted) file count in subtree
	Bytes     int64 // aggregated current size of live files
	Added     int
	Modified  int
	Deleted   int
	Unchanged int

	AddedBytes    int64 // current size of added files
	ModifiedBytes int64 // current size of modified files
	DeletedBytes  int64 // last-known size of deleted files (not in Bytes)
}

// Node is one entry in the viewer tree: a directory (IsDir, with
// Children) or a file (Cat set). Label is the already-sanitised base
// name — the render layer draws it verbatim and never re-derives a
// display string from a raw path.
type Node struct {
	Label    string
	IsDir    bool
	Cat      Category // file category; directories summarise via Stats
	Stats    Stats    // aggregated over the subtree
	Children []*Node  // directories only
}

// Tree is the immutable result handed to the render layer.
type Tree struct {
	Root *Node
}

// ChangeSet carries a command's per-path change labels. status fills it
// from StatusResult; update fills it from the enriched UpdateResult. It
// labels live entries by path-set membership; Deleted paths are unioned
// into the tree even when absent from the merged index (the update-full
// case, where the merged index retains no deleted entries).
//
// DeletedSizes carries the last-known size of each deleted path for the
// update path, where the entry is gone after the atomic rename so its
// size cannot be read from the merged index. It is named *Sizes (not
// *Bytes) to avoid colliding with the aggregate Stats.DeletedBytes. nil
// on the status path (the in-index tombstone supplies the size there);
// indexing a nil map yields 0, so the union step needs no guard.
type ChangeSet struct {
	Added        []string
	Modified     []string
	Deleted      []string
	DeletedSizes map[string]int64
}

// treeEntry is the pure builder's input: one merged-index file. Keeping
// the input a plain struct (rather than *binaryEntry) makes the
// aggregation/category logic unit-testable with literals — no skiplist
// fixture required.
type treeEntry struct {
	Path    string
	Size    int64
	Deleted bool
}

// BuildTree adapts the merged-index skiplist into the pure builder's
// input and returns the viewer tree. It performs NO filesystem access:
// per-file sizes and the deleted flag come from the already-loaded
// merged index (KD2 single source), and cs supplies the
// added/modified/deleted labels for live entries.
func BuildTree(merged *skiplistWrapper, cs ChangeSet) *Tree {
	var entries []treeEntry
	if merged != nil {
		merged.ForEach(func(e *binaryEntry, _ string) bool {
			entries = append(entries, treeEntry{
				Path:    e.RelativePath(),
				Size:    e.FileSize,
				Deleted: e.IsDeleted(),
			})
			return true
		})
	}
	return buildTreeFromEntries(entries, cs)
}

// SanitiseLabel exposes the reject-by-default label sanitiser to the
// render layer, so error/teardown text (which may embed an
// attacker-influenced filename via a wrapped OS/tcell error) is
// neutralised by the same helper that cleans node labels (KD6).
func SanitiseLabel(s string) string { return sanitiseLabel(s) }

// buildTreeFromEntries is the pure, terminal-free, filesystem-free tree
// builder. It splits each path on '/', creates directory nodes,
// attaches file leaves with a Category, unions in ChangeSet.Deleted
// paths not already present, rolls Stats up every directory, sanitises
// every label, and orders children canonically (name ascending — the
// render layer owns any runtime re-sort, KD8).
func buildTreeFromEntries(entries []treeEntry, cs ChangeSet) *Tree {
	added := sliceToSet(cs.Added)
	modified := sliceToSet(cs.Modified)

	root := &Node{IsDir: true}
	// dirs is keyed by the raw slash-joined directory prefix ("" = root)
	// so directories dedup on their real names even when two raw names
	// sanitise to the same Label.
	dirs := map[string]*Node{"": root}
	// seenDeleted records which deleted paths already exist as entries
	// (the status/cache case) so the union step doesn't double-count.
	seenDeleted := make(map[string]bool)

	insert := func(path string, size int64, cat Category) {
		if path == "" {
			return
		}
		parts := strings.Split(path, "/")
		cur := root
		prefix := ""
		for _, comp := range parts[:len(parts)-1] {
			if prefix == "" {
				prefix = comp
			} else {
				prefix += "/" + comp
			}
			next := dirs[prefix]
			if next == nil {
				next = &Node{Label: sanitiseLabel(comp), IsDir: true}
				dirs[prefix] = next
				cur.Children = append(cur.Children, next)
			}
			cur = next
		}
		name := parts[len(parts)-1]
		cur.Children = append(cur.Children, &Node{
			Label: sanitiseLabel(name),
			Cat:   cat,
			Stats: leafStats(size, cat),
		})
	}

	for _, e := range entries {
		cat := Unchanged
		switch {
		case e.Deleted:
			cat = Deleted
			seenDeleted[e.Path] = true
		case modified[e.Path]:
			cat = Modified
		case added[e.Path]:
			cat = Added
		}
		insert(e.Path, e.Size, cat)
	}

	// Union in deleted paths absent from the merged entries. After a
	// full update the merged index carries no deleted entries, so the
	// ChangeSet is the only source of deletions; their last-known size
	// travels on cs.DeletedSizes (nil map → 0, no guard needed). A path
	// present as both a tombstone and in DeletedSizes is skipped here via
	// seenDeleted — the tombstone size wins (KD2: both are last-known).
	for _, p := range cs.Deleted {
		if seenDeleted[p] {
			continue
		}
		insert(p, cs.DeletedSizes[p], Deleted)
	}

	aggregate(root)
	sortChildren(root)
	return &Tree{Root: root}
}

// leafStats builds the Stats for a single file leaf. A deleted leaf is
// count-only (not counted in Files/Bytes); every other category counts
// one live file with its current size.
func leafStats(size int64, cat Category) Stats {
	switch cat {
	case Added:
		return Stats{Files: 1, Bytes: size, Added: 1, AddedBytes: size}
	case Modified:
		return Stats{Files: 1, Bytes: size, Modified: 1, ModifiedBytes: size}
	case Deleted:
		// Deleted stays out of Files/Bytes; DeletedBytes separately
		// retains the last-known size for the change_bytes sort.
		return Stats{Deleted: 1, DeletedBytes: size}
	default: // Unchanged
		return Stats{Files: 1, Bytes: size, Unchanged: 1}
	}
}

// aggregate sums children's Stats into each directory, post-order, and
// returns the subtree total.
func aggregate(n *Node) Stats {
	if !n.IsDir {
		return n.Stats
	}
	var s Stats
	for _, c := range n.Children {
		cs := aggregate(c)
		s.Files += cs.Files
		s.Bytes += cs.Bytes
		s.Added += cs.Added
		s.Modified += cs.Modified
		s.Deleted += cs.Deleted
		s.Unchanged += cs.Unchanged
		s.AddedBytes += cs.AddedBytes
		s.ModifiedBytes += cs.ModifiedBytes
		s.DeletedBytes += cs.DeletedBytes
	}
	n.Stats = s
	return s
}

// sortChildren orders every directory's children name-ascending with a
// stable tiebreak (the index already yields a deterministic order). This
// is the canonical order; the render layer re-sorts in place at draw
// time for the runtime sort toggles (KD8).
func sortChildren(n *Node) {
	sort.SliceStable(n.Children, func(i, j int) bool {
		return n.Children[i].Label < n.Children[j].Label
	})
	for _, c := range n.Children {
		if c.IsDir {
			sortChildren(c)
		}
	}
}

// sliceToSet builds a membership set from a path slice.
func sliceToSet(paths []string) map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	m := make(map[string]bool, len(paths))
	for _, p := range paths {
		m[p] = true
	}
	return m
}

// sanitiseLabel returns s with every rune that is not safely printable
// replaced by a backslash escape, so no control byte, escape introducer
// (ESC/CSI/OSC/DCS), DEL, C1 byte, or invalid-UTF-8 byte can reach the
// terminal when the label is drawn. It is a reject-by-default printable
// ALLOWLIST (a rune is kept only if safeRune accepts it), not a
// blocklist of known-bad sequences — so it also neutralises bytes
// outside any enumerated escape set.
func sanitiseLabel(s string) string {
	if labelClean(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 byte — escape the raw byte value.
			fmt.Fprintf(&b, `\x%02x`, s[i])
			i++
			continue
		}
		if safeRune(r) {
			b.WriteRune(r)
		} else {
			b.WriteString(escapeRune(r))
		}
		i += size
	}
	return b.String()
}

// labelClean reports whether s is valid UTF-8 and entirely composed of
// safe runes (the common case — avoids allocating a builder).
func labelClean(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !safeRune(r) {
			return false
		}
	}
	return true
}

// safeRune reports whether r may be drawn to the terminal verbatim. It
// rejects the C0 range (including \r \n \t \b), DEL, the C1 range, the
// UTF-8 replacement rune, and anything non-printable per unicode.IsPrint.
func safeRune(r rune) bool {
	if r == utf8.RuneError {
		return false
	}
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
		return false
	}
	return unicode.IsPrint(r)
}

// escapeRune renders an unsafe (but valid) rune as a backslash escape,
// e.g. ESC → `\x1b`, U+009B → ``, reusing strconv's quoting.
func escapeRune(r rune) string {
	q := strconv.QuoteRune(r)
	return q[1 : len(q)-1] // strip surrounding single quotes
}
