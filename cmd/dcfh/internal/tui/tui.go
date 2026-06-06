// Package tui renders the read-only, gdu-style post-run interactive tree
// for `dcfh status --interactive-tree` and `dcfh update --interactive-tree`.
//
// It consumes only the exported *dcfh.Tree (never the unexported index
// types) and owns all terminal handling: it must be invoked ONLY behind
// the caller's `!--json && IsTerminal(stdout)` guard. The label
// sanitiser in the data layer is defence-in-depth; the guard is the
// primary control. The viewer never mutates the index or filesystem.
package tui

import (
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v2"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// defaultMinWidthForStats is the fallback width threshold for showing the
// right-hand stats pane when Options.MinWidthForStats is unset.
const defaultMinWidthForStats = 80

// Options configures a viewer run.
type Options struct {
	// MinWidthForStats is the minimum terminal width (columns) at which
	// the right-hand before/after stats pane is shown; below it the tree
	// pane uses the full width (FR6). Zero falls back to a sane default.
	MinWidthForStats int
	// Title labels the header — typically the command name ("status" or
	// "update").
	Title string
}

// Run opens a full-screen tcell viewer over t and blocks until the user
// quits (q / Esc / Ctrl-C). It returns nil on a clean quit and a
// sanitised error if the screen cannot be initialised (FR9) — in which
// case nothing has been mutated and the terminal is left usable. A nil
// or empty tree is a clean no-op.
func Run(t *dcfh.Tree, o Options) error {
	if t == nil || t.Root == nil {
		return nil
	}
	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("interactive tree unavailable: %s", sanitiseError(err))
	}
	return runScreen(screen, t, o)
}

// runScreen drives the viewer against an already-constructed screen. It
// is the seam the tests use with tcell's SimulationScreen; Run supplies
// a real terminal screen. It owns Init/Fini so teardown is guaranteed on
// every exit path.
func runScreen(screen tcell.Screen, t *dcfh.Tree, o Options) error {
	if err := screen.Init(); err != nil {
		// Fini after a failed Init must be safe; tcell guarantees this.
		screen.Fini()
		return fmt.Errorf("interactive tree unavailable: %s", sanitiseError(err))
	}

	// Idempotent teardown installed immediately after a successful Init
	// and before any draw (KD7/FR9): every exit path — quit, panic,
	// Ctrl-C — restores the terminal exactly once.
	var finiOnce sync.Once
	fini := func() { finiOnce.Do(screen.Fini) }
	defer fini()

	if o.MinWidthForStats <= 0 {
		o.MinWidthForStats = defaultMinWidthForStats
	}

	m := newModel(t, o)
	for {
		m.draw(screen)
		screen.Show()

		switch ev := screen.PollEvent().(type) {
		case *tcell.EventResize:
			screen.Sync()
		case *tcell.EventKey:
			if m.handleKey(ev) {
				return nil
			}
		case *tcell.EventInterrupt:
			return nil
		case nil:
			// PollEvent returns nil once the screen is finalised.
			return nil
		}
	}
}

// handleKey applies one key event to the model and reports whether the
// viewer should quit. It is split out (and operates purely on the model)
// so the event handling is unit-testable via tcell's SimulationScreen.
func (m *model) handleKey(ev *tcell.EventKey) (quit bool) {
	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyCtrlC:
		return true
	case tcell.KeyUp:
		m.moveUp()
	case tcell.KeyDown:
		m.moveDown()
	case tcell.KeyRight, tcell.KeyEnter:
		m.expand()
	case tcell.KeyLeft:
		m.collapseOrParent()
	case tcell.KeyRune:
		return m.handleRune(ev.Rune())
	}
	return false
}

func (m *model) handleRune(r rune) (quit bool) {
	switch r {
	case 'q':
		return true
	case 'j':
		m.moveDown()
	case 'k':
		m.moveUp()
	case 'l':
		m.expand()
	case 'h':
		m.collapseOrParent()
	case 'r':
		cur := m.current()
		m.reverse = !m.reverse
		m.rebuildRows()
		m.selectNode(cur)
	default:
		if key, ok := keyForRune(r); ok {
			cur := m.current()
			m.sortKey = key
			m.rebuildRows()
			m.selectNode(cur)
		}
	}
	return false
}

func (m *model) moveDown() {
	if m.sel < len(m.rows)-1 {
		m.sel++
	}
}

func (m *model) moveUp() {
	if m.sel > 0 {
		m.sel--
	}
}

// expand opens the selected directory, or steps into its first child if
// it is already open. No-op on files.
func (m *model) expand() {
	cur := m.current()
	if cur == nil || !cur.IsDir {
		return
	}
	if !m.expanded[cur] {
		m.expanded[cur] = true
		m.rebuildRows()
		m.selectNode(cur)
		return
	}
	if m.sel+1 < len(m.rows) && m.rows[m.sel+1].depth > m.rows[m.sel].depth {
		m.sel++
	}
}

// collapseOrParent closes an open directory, or jumps to the parent row
// when the selection is a file or an already-closed directory.
func (m *model) collapseOrParent() {
	cur := m.current()
	if cur == nil {
		return
	}
	if cur.IsDir && m.expanded[cur] {
		delete(m.expanded, cur)
		m.rebuildRows()
		m.selectNode(cur)
		return
	}
	d := m.rows[m.sel].depth
	if d == 0 {
		return
	}
	for i := m.sel - 1; i >= 0; i-- {
		if m.rows[i].depth == d-1 {
			m.sel = i
			return
		}
	}
}

// sanitiseError neutralises any control/escape bytes an OS or tcell error
// string might carry (e.g. an attacker-influenced filename) before it is
// printed to the restored terminal — via the same helper used for node
// labels (KD6/S3).
func sanitiseError(err error) string {
	if err == nil {
		return ""
	}
	return dcfh.SanitiseLabel(err.Error())
}
