package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// dir builds a directory node from exported types and aggregates its
// children's Stats (the tui package can only see the exported tree, not
// the pkg-internal builder).
func dir(label string, children ...*dcfh.Node) *dcfh.Node {
	n := &dcfh.Node{Label: label, IsDir: true, Children: children}
	for _, c := range children {
		n.Stats.Files += c.Stats.Files
		n.Stats.Bytes += c.Stats.Bytes
		n.Stats.Added += c.Stats.Added
		n.Stats.Modified += c.Stats.Modified
		n.Stats.Deleted += c.Stats.Deleted
		n.Stats.Unchanged += c.Stats.Unchanged
		n.Stats.AddedBytes += c.Stats.AddedBytes
		n.Stats.ModifiedBytes += c.Stats.ModifiedBytes
		n.Stats.DeletedBytes += c.Stats.DeletedBytes
	}
	return n
}

// treeForSim builds a representative tree:
//
//	docs/old.md   (deleted)
//	src/main.go   (modified)
//	src/new.go    (added)
//	readme.md     (unchanged)
func treeForSim(t *testing.T) *dcfh.Tree {
	t.Helper()
	oldmd := &dcfh.Node{Label: "old.md", Cat: dcfh.Deleted, Stats: dcfh.Stats{Deleted: 1, DeletedBytes: 900}}
	mainGo := &dcfh.Node{Label: "main.go", Cat: dcfh.Modified, Stats: dcfh.Stats{Files: 1, Bytes: 200, Modified: 1, ModifiedBytes: 200}}
	newGo := &dcfh.Node{Label: "new.go", Cat: dcfh.Added, Stats: dcfh.Stats{Files: 1, Bytes: 50, Added: 1, AddedBytes: 50}}
	readme := &dcfh.Node{Label: "readme.md", Cat: dcfh.Unchanged, Stats: dcfh.Stats{Files: 1, Bytes: 10, Unchanged: 1}}
	root := dir("", dir("docs", oldmd), dir("src", mainGo, newGo), readme)
	return &dcfh.Tree{Root: root}
}

// containsRune reports whether the simulation screen's cell buffer holds
// r anywhere — used to detect the stats-pane divider.
func containsRune(sim tcell.SimulationScreen, r rune) bool {
	cells, _, _ := sim.GetContents()
	for _, c := range cells {
		if slices.Contains(c.Runes, r) {
			return true
		}
	}
	return false
}

// driveUntilQuit runs the viewer loop and injects key until it returns,
// retrying because keys posted before Init() are dropped by the
// simulation screen. Returns the loop's error.
func driveUntilQuit(t *testing.T, sim tcell.SimulationScreen, key tcell.Key, r rune) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- runScreen(sim, treeForSim(t), Options{Title: "status"}) }()
	for {
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Millisecond):
			sim.InjectKey(key, r, tcell.ModNone)
		}
	}
}

// newSimModel returns a model plus a sized, initialised simulation screen.
// Height is fixed at 24 (every caller used the same value).
func newSimModel(t *testing.T, w int, o Options) (*model, tcell.SimulationScreen) {
	t.Helper()
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("sim init: %v", err)
	}
	sim.SetSize(w, 24)
	if o.MinWidthForStats <= 0 {
		o.MinWidthForStats = defaultMinWidthForStats
	}
	return newModel(treeForSim(t), o), sim
}

// TC-13 (AC4/FR6): width gating — two panes when wide, tree-only when
// narrow; no panic at either size or after a resize.
func TestWidthGating(t *testing.T) {
	tree := treeForSim(t)
	o := Options{Title: "update", MinWidthForStats: 80}

	// Wide: stats pane present (divider drawn).
	wide := tcell.NewSimulationScreen("UTF-8")
	if err := wide.Init(); err != nil {
		t.Fatal(err)
	}
	wide.SetSize(120, 24)
	mWide := newModel(tree, o)
	mWide.draw(wide)
	wide.Show()
	if !containsRune(wide, tcell.RuneVLine) {
		t.Errorf("wide screen should show the stats-pane divider")
	}

	// Narrow: no divider, tree uses full width.
	narrow := tcell.NewSimulationScreen("UTF-8")
	if err := narrow.Init(); err != nil {
		t.Fatal(err)
	}
	narrow.SetSize(40, 24)
	mNarrow := newModel(tree, o)
	mNarrow.draw(narrow)
	narrow.Show()
	if containsRune(narrow, tcell.RuneVLine) {
		t.Errorf("narrow screen should not show the stats-pane divider")
	}

	// Resize across the threshold must not panic.
	narrow.SetSize(120, 24)
	mNarrow.draw(narrow)
	narrow.Show()
}

// TC-14 (AC4/FR5): navigation — expand reveals children; down moves the
// selection; collapse hides children again.
func TestNavigation(t *testing.T) {
	m, sim := newSimModel(t, 100, Options{Title: "update"})
	defer sim.Fini()

	startRows := len(m.rows)
	// Select the first directory and expand it.
	m.sel = 0
	first := m.current()
	if first == nil || !first.IsDir {
		t.Fatalf("expected first row to be a directory, got %+v", first)
	}
	m.handleKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if len(m.rows) <= startRows {
		t.Errorf("expand did not reveal children: %d -> %d rows", startRows, len(m.rows))
	}
	if m.current() != first {
		t.Errorf("selection should stay on the expanded dir")
	}

	// Down moves into the first child.
	m.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if m.sel != 1 {
		t.Errorf("down should move selection to row 1, got %d", m.sel)
	}

	// Collapse the dir again (left on a child jumps to parent, left on
	// the open parent collapses).
	m.handleKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)) // to parent
	m.handleKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)) // collapse
	if len(m.rows) != startRows {
		t.Errorf("collapse did not restore row count: want %d, got %d", startRows, len(m.rows))
	}
}

// TC-13c (AC9/FR10): a sort/reverse keypress re-orders visible children
// in place, preserves the selected node, and never re-reads the data
// layer (the tree object is the same pointer — no PostRunTree round-trip).
func TestLiveResortPreservesSelectionNoReRead(t *testing.T) {
	tree := treeForSim(t)
	m := newModel(tree, Options{Title: "update", MinWidthForStats: 80})

	// Expand everything so we have children to reorder.
	for _, r := range append([]rowItem(nil), m.rows...) {
		if r.node.IsDir {
			m.expanded[r.node] = true
		}
	}
	m.rebuildRows()

	// Pick a deterministic node to track across the re-sort.
	target := m.rows[len(m.rows)-1].node
	m.selectNode(target)

	beforeRoot := m.root
	beforeOrder := rowOrder(m)

	// Switch to name sort, then reverse.
	m.handleKey(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone))
	afterName := rowOrder(m)
	m.handleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	afterReverse := rowOrder(m)

	if m.current() != target {
		t.Errorf("selection not preserved across re-sort")
	}
	if m.root != beforeRoot {
		t.Errorf("re-sort must not replace the tree (data-layer re-read)")
	}
	if beforeOrder == afterName && afterName == afterReverse {
		t.Errorf("sort keys had no visible effect on ordering")
	}
}

// TC-15 (AC6/NFR5): the full event loop runs and tears down cleanly when
// the user quits ('q'); a second Fini is safe (idempotent).
func TestRunScreen_QuitAndTeardown(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	sim.SetSize(100, 24)
	if err := driveUntilQuit(t, sim, tcell.KeyRune, 'q'); err != nil {
		t.Fatalf("runScreen returned error on clean quit: %v", err)
	}
	sim.Fini() // idempotent: must not panic
}

// Ctrl-C also quits the loop cleanly.
func TestRunScreen_CtrlCQuits(t *testing.T) {
	sim := tcell.NewSimulationScreen("UTF-8")
	sim.SetSize(100, 24)
	if err := driveUntilQuit(t, sim, tcell.KeyCtrlC, 0); err != nil {
		t.Fatalf("Ctrl-C should quit cleanly, got %v", err)
	}
	sim.Fini()
}

// Run on an empty tree is a clean no-op (FR8) and never opens a screen.
func TestRun_EmptyTreeNoOp(t *testing.T) {
	if err := Run(nil, Options{}); err != nil {
		t.Errorf("Run(nil) = %v, want nil", err)
	}
	if err := Run(&dcfh.Tree{}, Options{}); err != nil {
		t.Errorf("Run(empty) = %v, want nil", err)
	}
}

// screenText flattens the simulation screen's cell buffer into one string
// per row, so tests can assert on rendered header/footer/pane text.
func screenText(sim tcell.SimulationScreen) string {
	cells, w, h := sim.GetContents()
	var b strings.Builder
	for y := range h {
		for x := range w {
			c := cells[y*w+x]
			if len(c.Runes) > 0 {
				b.WriteRune(c.Runes[0])
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TC-7/TC-8 (FR5/FR6/AC1/AC2): a freshly opened viewer defaults to
// change_bytes(desc); 'f' switches to change_files; 'r' flips direction.
func TestDefaultSortAndKeyToggles(t *testing.T) {
	m, sim := newSimModel(t, 100, Options{Title: "status"})
	defer sim.Fini()

	m.draw(sim)
	sim.Show()
	if got := screenText(sim); !strings.Contains(got, "sort:change_bytes(desc)") {
		t.Errorf("default header missing change_bytes(desc); header line:\n%s", firstLine(got))
	}

	// 'f' → change_files (still desc by default).
	m.handleKey(tcell.NewEventKey(tcell.KeyRune, 'f', tcell.ModNone))
	m.draw(sim)
	sim.Show()
	if got := screenText(sim); !strings.Contains(got, "sort:change_files(desc)") {
		t.Errorf("after 'f' header missing change_files(desc); header line:\n%s", firstLine(got))
	}

	// 'r' flips direction.
	m.handleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	m.draw(sim)
	sim.Show()
	if got := screenText(sim); !strings.Contains(got, "sort:change_files(asc)") {
		t.Errorf("after 'r' header missing change_files(asc); header line:\n%s", firstLine(got))
	}
}

// TC-11 (KD6/AC1): the stats pane annotates each change line with its
// bytes via FormatHumanSize when the screen is wide enough.
func TestStatsPaneByteAnnotations(t *testing.T) {
	m, sim := newSimModel(t, 120, Options{Title: "status", MinWidthForStats: 80})
	defer sim.Fini()

	// Default change_bytes(desc) orders root children docs(900) > src(250)
	// > readme(0); select src (Added 1 / Modified 1) to show byte sums.
	m.selectNode(findRow(m, "src"))
	m.draw(sim)
	sim.Show()
	got := screenText(sim)
	for _, want := range []string{
		"Added:     1 (" + dcfh.FormatHumanSize(50) + ")",
		"Modified:  1 (" + dcfh.FormatHumanSize(200) + ")",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stats pane missing %q in:\n%s", want, got)
		}
	}

	// And a deleted-only dir shows its deleted bytes.
	m.selectNode(findRow(m, "docs"))
	m.draw(sim)
	sim.Show()
	if got := screenText(sim); !strings.Contains(got, "Deleted:   1 ("+dcfh.FormatHumanSize(900)+")") {
		t.Errorf("stats pane missing deleted bytes in:\n%s", got)
	}
}

// Task 13: the drawn column tracks the active sort metric end-to-end, not
// Stats.Bytes. docs/ aggregates a deleted file (Stats.Bytes 0, change_bytes
// 900), so it is the discriminator: the old code drew "0 B", the fix draws
// "900 B". (Stats pane disabled via MinWidthForStats > width so the only
// "900" on screen is the tree column, never the pane's "Deleted (900 B)".)
func TestColumnTracksActiveSortMetric(t *testing.T) {
	m, sim := newSimModel(t, 100, Options{Title: "status", MinWidthForStats: 200})
	defer sim.Fini()

	// Default change_bytes(desc): docs row shows its change volume, 900 B.
	m.draw(sim)
	sim.Show()
	line := rowLine(screenText(sim), "docs/")
	if !strings.Contains(line, dcfh.FormatHumanSize(900)) {
		t.Errorf("change_bytes: docs row should show %q (change volume), got:\n%s",
			dcfh.FormatHumanSize(900), line)
	}

	// Toggle to change_files: the column becomes the count (1 deleted file),
	// and the byte size is gone.
	m.handleKey(tcell.NewEventKey(tcell.KeyRune, 'f', tcell.ModNone))
	m.draw(sim)
	sim.Show()
	line = rowLine(screenText(sim), "docs/")
	if strings.Contains(line, dcfh.FormatHumanSize(900)) {
		t.Errorf("change_files: docs row should no longer show bytes, got:\n%s", line)
	}
	if !strings.Contains(line, "1") {
		t.Errorf("change_files: docs row should show the count 1, got:\n%s", line)
	}
}

// --- Task 15: status glyph + colour + bold (FR1–FR9) -----------------------

// expandAllVisible expands every currently-visible directory once and
// rebuilds rows. treeForSim nests only one level, so a single pass reveals
// every leaf.
func expandAllVisible(m *model) {
	for _, r := range append([]rowItem(nil), m.rows...) {
		if r.node.IsDir {
			m.expanded[r.node] = true
		}
	}
	m.rebuildRows()
}

// rowYOf returns the y of the first screen row whose flattened text contains
// sub, or -1.
func rowYOf(sim tcell.SimulationScreen, sub string) int {
	cells, w, h := sim.GetContents()
	for y := range h {
		var b strings.Builder
		for x := range w {
			c := cells[y*w+x]
			if len(c.Runes) > 0 {
				b.WriteRune(c.Runes[0])
			} else {
				b.WriteByte(' ')
			}
		}
		if strings.Contains(b.String(), sub) {
			return y
		}
	}
	return -1
}

// styleOfRuneInRow returns the style of the first cell on row y holding r.
func styleOfRuneInRow(sim tcell.SimulationScreen, y int, r rune) (tcell.Style, bool) {
	cells, w, _ := sim.GetContents()
	if y < 0 {
		return tcell.StyleDefault, false
	}
	for x := range w {
		c := cells[y*w+x]
		if len(c.Runes) > 0 && c.Runes[0] == r {
			return c.Style, true
		}
	}
	return tcell.StyleDefault, false
}

// fgBoldReverse decomposes a style into the assertions task 15 cares about.
func fgBoldReverse(st tcell.Style) (tcell.Color, bool, bool) {
	fg, _, attr := st.Decompose()
	return fg, attr&tcell.AttrBold != 0, attr&tcell.AttrReverse != 0
}

// treeAllThreeDir builds a tree with a single visible top-level directory
// "proj" holding one added, one modified and one deleted child — so the
// rendered dir row exercises the all-three (white) blend, which the
// treeForSim root (unrendered) cannot.
func treeAllThreeDir() *dcfh.Tree {
	a := &dcfh.Node{Label: "a", Cat: dcfh.Added, Stats: dcfh.Stats{Files: 1, Bytes: 1, Added: 1, AddedBytes: 1}}
	md := &dcfh.Node{Label: "m", Cat: dcfh.Modified, Stats: dcfh.Stats{Files: 1, Bytes: 1, Modified: 1, ModifiedBytes: 1}}
	d := &dcfh.Node{Label: "d", Cat: dcfh.Deleted, Stats: dcfh.Stats{Deleted: 1, DeletedBytes: 1}}
	return &dcfh.Tree{Root: dir("", dir("proj", a, md, d))}
}

// TC-U1 (AC1–AC4, FR1–FR5): nodeStyle is pure — table-driven over all 8
// present-sets, asserting glyph, foreground colour, bold, the safe glyph
// alphabet, and bold == (set ≠ ∅).
func TestNodeStyle(t *testing.T) {
	safe := func(r rune) bool {
		return r == '+' || r == '~' || r == '-' || r == '*' || r == ' '
	}
	cases := []struct {
		name    string
		a, m, d int
		glyph   rune
		fg      tcell.Color
		bold    bool
	}{
		{"unchanged", 0, 0, 0, ' ', tcell.ColorDefault, false},
		{"added", 1, 0, 0, '+', tcell.ColorGreen, true},
		{"modified", 0, 1, 0, '~', tcell.ColorBlue, true},
		{"deleted", 0, 0, 1, '-', tcell.ColorRed, true},
		{"add+mod", 1, 1, 0, '*', tcell.ColorAqua, true},
		{"mod+del", 0, 1, 1, '*', tcell.ColorFuchsia, true},
		{"add+del", 1, 0, 1, '*', tcell.ColorYellow, true},
		{"all-three", 1, 1, 1, '*', tcell.ColorWhite, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := &dcfh.Node{Stats: dcfh.Stats{Added: tc.a, Modified: tc.m, Deleted: tc.d}}
			glyph, st := nodeStyle(n)
			if !safe(glyph) {
				t.Fatalf("glyph %q not in safe alphabet {+ ~ - * space}", glyph)
			}
			if glyph != tc.glyph {
				t.Errorf("glyph = %q, want %q", glyph, tc.glyph)
			}
			fg, bold, _ := fgBoldReverse(st)
			if fg != tc.fg {
				t.Errorf("fg = %v, want %v", fg, tc.fg)
			}
			if bold != tc.bold {
				t.Errorf("bold = %v, want %v", bold, tc.bold)
			}
			set := tc.a > 0 || tc.m > 0 || tc.d > 0
			if bold != set {
				t.Errorf("bold (%v) must equal (set ≠ ∅) (%v)", bold, set)
			}
		})
	}
}

// TC-S1 (AC1/AC3, FR1/FR3): the status glyph appears on each changed row and
// not on the unchanged leaf.
func TestRenderGlyphPlacement(t *testing.T) {
	m, sim := newSimModel(t, 100, Options{Title: "status"})
	defer sim.Fini()
	expandAllVisible(m)
	m.draw(sim)
	sim.Show()
	screen := screenText(sim)

	for _, tc := range []struct {
		label string
		glyph string
	}{
		{"new.go", "+"},
		{"main.go", "~"},
		{"old.md", "-"},
		{"docs/", "-"}, // deleted-only dir
		{"src/", "*"},  // add+mod mixed dir
	} {
		if line := rowLine(screen, tc.label); !strings.Contains(line, tc.glyph) {
			t.Errorf("%s row missing glyph %q; line:\n%q", tc.label, tc.glyph, line)
		}
	}
	// readme.md (unchanged) carries no status glyph.
	if line := rowLine(screen, "readme.md"); strings.ContainsAny(line, "+~*") {
		t.Errorf("unchanged readme.md row should carry no status glyph; line:\n%q", line)
	}
}

// TC-S2 (AC1/AC4, FR1): changed leaves render their category colour and bold;
// the unchanged leaf is default fg and not bold.
func TestRenderLeafColourAndBold(t *testing.T) {
	m, sim := newSimModel(t, 100, Options{Title: "status"})
	defer sim.Fini()
	expandAllVisible(m)
	m.draw(sim)
	sim.Show()

	for _, tc := range []struct {
		label string
		glyph rune
		fg    tcell.Color
	}{
		{"new.go", '+', tcell.ColorGreen},
		{"main.go", '~', tcell.ColorBlue},
		{"old.md", '-', tcell.ColorRed},
	} {
		st, ok := styleOfRuneInRow(sim, rowYOf(sim, tc.label), tc.glyph)
		if !ok {
			t.Fatalf("%s: glyph %q not found on its row", tc.label, tc.glyph)
		}
		fg, bold, _ := fgBoldReverse(st)
		if fg != tc.fg || !bold {
			t.Errorf("%s: fg=%v bold=%v, want fg=%v bold=true", tc.label, fg, bold, tc.fg)
		}
	}

	// readme.md: style of its label cell is default fg and non-bold.
	st, ok := styleOfRuneInRow(sim, rowYOf(sim, "readme.md"), 'r')
	if !ok {
		t.Fatal("readme.md label cell not found")
	}
	if fg, bold, _ := fgBoldReverse(st); fg != tcell.ColorDefault || bold {
		t.Errorf("readme.md: fg=%v bold=%v, want default/non-bold", fg, bold)
	}
}

// TC-S3/TC-S5 (AC2/AC9, FR2): directory blend colour — a deleted-only dir is
// red (not "unchanged"), and an add+mod dir is aqua (cyan); both bold.
func TestRenderDirectoryBlend(t *testing.T) {
	m, sim := newSimModel(t, 100, Options{Title: "status"})
	defer sim.Fini()
	m.draw(sim) // top-level dirs visible without expansion
	sim.Show()

	st, ok := styleOfRuneInRow(sim, rowYOf(sim, "docs/"), '-')
	if !ok {
		t.Fatal("docs/ glyph '-' not found (deleted-only dir should render '-')")
	}
	if fg, bold, _ := fgBoldReverse(st); fg != tcell.ColorRed || !bold {
		t.Errorf("docs/ deleted-only: fg=%v bold=%v, want Red/bold", fg, bold)
	}

	st, ok = styleOfRuneInRow(sim, rowYOf(sim, "src/"), '*')
	if !ok {
		t.Fatal("src/ glyph '*' not found (add+mod dir should render '*')")
	}
	if fg, bold, _ := fgBoldReverse(st); fg != tcell.ColorAqua || !bold {
		t.Errorf("src/ add+mod: fg=%v bold=%v, want Aqua/bold", fg, bold)
	}
}

// TC-S6 (AC10): an all-three directory renders white / '*' / bold and is
// distinguishable from an unchanged row (which is default / no-glyph / non-bold).
func TestRenderAllThreeDirectory(t *testing.T) {
	m := newModel(treeAllThreeDir(), Options{Title: "status"})
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("sim init: %v", err)
	}
	defer sim.Fini()
	sim.SetSize(100, 24)
	m.draw(sim)
	sim.Show()

	st, ok := styleOfRuneInRow(sim, rowYOf(sim, "proj/"), '*')
	if !ok {
		t.Fatal("proj/ glyph '*' not found")
	}
	fg, bold, _ := fgBoldReverse(st)
	if fg != tcell.ColorWhite || !bold {
		t.Errorf("all-three proj/: fg=%v bold=%v, want White/bold", fg, bold)
	}
}

// TC-S7 (AC6, FR6): selection composes Reverse over the category style while
// the status glyph stays on the row.
func TestRenderSelectionComposes(t *testing.T) {
	m, sim := newSimModel(t, 100, Options{Title: "status"})
	defer sim.Fini()
	m.selectNode(findRow(m, "docs")) // a changed (deleted-only) dir
	m.draw(sim)
	sim.Show()

	st, ok := styleOfRuneInRow(sim, rowYOf(sim, "docs/"), '-')
	if !ok {
		t.Fatal("selected docs/ glyph '-' not found")
	}
	fg, bold, reverse := fgBoldReverse(st)
	if !reverse {
		t.Errorf("selected row should set Reverse; style fg=%v bold=%v", fg, bold)
	}
	if !bold || fg != tcell.ColorRed {
		t.Errorf("selection must compose over the category style (Red/bold), got fg=%v bold=%v", fg, bold)
	}
}

// TC-S8 (AC11, FR8): the stats-pane Modified line renders blue, matching a
// modified leaf — not the old yellow.
func TestStatsPaneModifiedIsBlue(t *testing.T) {
	m, sim := newSimModel(t, 120, Options{Title: "status", MinWidthForStats: 80})
	defer sim.Fini()
	m.selectNode(findRow(m, "src"))
	m.draw(sim)
	sim.Show()

	y := rowYOf(sim, "Modified:")
	if y < 0 {
		t.Fatal("stats pane 'Modified:' line not found")
	}
	st, ok := styleOfRuneInRow(sim, y, '~')
	if !ok {
		t.Fatal("stats pane Modified glyph '~' not found")
	}
	if fg, _, _ := fgBoldReverse(st); fg != tcell.ColorBlue {
		t.Errorf("stats-pane Modified fg=%v, want Blue", fg)
	}
}

// TC-S9 (AC7, FR7): at a narrow width the +2 glyph columns squeeze the
// right-aligned value, which drops (guard holds) — no panic, label survives.
func TestRenderNarrowWidthDropsValue(t *testing.T) {
	m, sim := newSimModel(t, 12, Options{Title: "status"})
	defer sim.Fini()
	m.draw(sim) // must not panic
	sim.Show()
	line := rowLine(screenText(sim), "docs")
	if !strings.Contains(line, "docs") {
		t.Errorf("narrow render dropped the label too; line:\n%q", line)
	}
	if strings.Contains(line, dcfh.FormatHumanSize(900)) {
		t.Errorf("narrow render should drop the size value; line:\n%q", line)
	}
}

// TC-S10 (AC12, FR9): the stats pane shows the glyph-prefixed, colour-matched
// category legend lines and the "* mixed" note.
func TestStatsPaneLegend(t *testing.T) {
	m, sim := newSimModel(t, 120, Options{Title: "status", MinWidthForStats: 80})
	defer sim.Fini()
	m.selectNode(findRow(m, "src"))
	m.draw(sim)
	sim.Show()
	got := screenText(sim)
	for _, want := range []string{"+ Added:", "~ Modified:", "- Deleted:", "* mixed"} {
		if !strings.Contains(got, want) {
			t.Errorf("stats-pane legend missing %q in:\n%s", want, got)
		}
	}
}

// rowLine returns the first flattened screen line containing sub (helper).
func rowLine(screen, sub string) string {
	for ln := range strings.SplitSeq(screen, "\n") {
		if strings.Contains(ln, sub) {
			return ln
		}
	}
	return ""
}

// findRow returns the visible node with the given label (test helper).
func findRow(m *model, label string) *dcfh.Node {
	for _, r := range m.rows {
		if r.node.Label == label {
			return r.node
		}
	}
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func rowOrder(m *model) string {
	var b strings.Builder
	for _, r := range m.rows {
		b.WriteString(r.node.Label)
		b.WriteByte(';')
	}
	return b.String()
}
