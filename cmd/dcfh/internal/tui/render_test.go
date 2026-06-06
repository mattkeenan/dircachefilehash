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
func newSimModel(t *testing.T, w, h int, o Options) (*model, tcell.SimulationScreen) {
	t.Helper()
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("sim init: %v", err)
	}
	sim.SetSize(w, h)
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
	m, sim := newSimModel(t, 100, 24, Options{Title: "update"})
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
	m, sim := newSimModel(t, 100, 24, Options{Title: "status"})
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
	m, sim := newSimModel(t, 120, 24, Options{Title: "status", MinWidthForStats: 80})
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
