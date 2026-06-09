package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// statsPaneWidth is the fixed width of the right-hand stats pane (plus a
// one-column divider). The pane is only shown when the screen is at
// least Options.MinWidthForStats columns wide (FR6).
const statsPaneWidth = 32

// rowItem is one visible line in the flattened tree (respecting the
// current expand/collapse state and active sort).
type rowItem struct {
	node  *dcfh.Node
	depth int
}

// model is the viewer's in-memory state. It owns no terminal resources;
// Run drives it against a tcell.Screen.
type model struct {
	root     *dcfh.Node
	opts     Options
	expanded map[*dcfh.Node]bool
	sortKey  sortKey
	reverse  bool

	rows []rowItem
	sel  int // index into rows
	top  int // first visible row (scroll offset)
}

func newModel(t *dcfh.Tree, o Options) *model {
	m := &model{
		root:     t.Root,
		opts:     o,
		expanded: make(map[*dcfh.Node]bool),
		sortKey:  sortChangeBytes,
	}
	m.rebuildRows()
	return m
}

// rebuildRows flattens the visible tree into m.rows using the active
// sort. Children are sorted via sortNodes, which copies — the canonical
// tree order is never mutated (so re-sorting is a pure view operation).
func (m *model) rebuildRows() {
	m.rows = m.rows[:0]
	var walk func(n *dcfh.Node, depth int)
	walk = func(n *dcfh.Node, depth int) {
		for _, c := range sortNodes(n.Children, m.sortKey, m.reverse) {
			m.rows = append(m.rows, rowItem{node: c, depth: depth})
			if c.IsDir && m.expanded[c] {
				walk(c, depth+1)
			}
		}
	}
	walk(m.root, 0)
	m.clampSel()
}

func (m *model) clampSel() {
	if m.sel >= len(m.rows) {
		m.sel = len(m.rows) - 1
	}
	if m.sel < 0 {
		m.sel = 0
	}
}

func (m *model) current() *dcfh.Node {
	if m.sel >= 0 && m.sel < len(m.rows) {
		return m.rows[m.sel].node
	}
	return nil
}

// selectNode moves the selection onto n if it is currently visible,
// preserving the user's focus across a re-sort or collapse.
func (m *model) selectNode(n *dcfh.Node) {
	for i, r := range m.rows {
		if r.node == n {
			m.sel = i
			return
		}
	}
	m.clampSel()
}

// draw renders the whole frame: header, tree pane, optional stats pane,
// and footer.
func (m *model) draw(s tcell.Screen) {
	s.Clear()
	width, height := s.Size()
	if width <= 0 || height <= 0 {
		return
	}

	treeWidth := width
	showStats := width >= m.opts.MinWidthForStats && m.opts.MinWidthForStats > 0
	if showStats {
		treeWidth = width - statsPaneWidth - 1
	}

	bodyTop, bodyBottom := 1, height-2 // rows [bodyTop, bodyBottom]
	m.drawHeader(s, width)
	m.drawFooter(s, width, height-1)
	m.scrollToSel(bodyTop, bodyBottom)
	m.drawTree(s, bodyTop, bodyBottom, treeWidth)
	if showStats {
		m.drawDivider(s, treeWidth, bodyTop, bodyBottom)
		m.drawStats(s, treeWidth+1, bodyTop, bodyBottom, width)
	}
}

// scrollToSel keeps the selected row inside the visible body window.
func (m *model) scrollToSel(bodyTop, bodyBottom int) {
	viewH := max(bodyBottom-bodyTop+1, 1)
	if m.sel < m.top {
		m.top = m.sel
	}
	if m.sel >= m.top+viewH {
		m.top = m.sel - viewH + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

func (m *model) drawHeader(s tcell.Screen, width int) {
	style := tcell.StyleDefault.Bold(true)
	dir := "desc"
	if (m.sortKey == sortName) != m.reverse {
		// name defaults ascending; reverse flips. Counts default desc.
		dir = "asc"
	}
	rs := m.root.Stats
	header := fmt.Sprintf("%s — interactive tree   sort:%s(%s)   %d files, %s",
		m.opts.Title, m.sortKey.label(), dir, rs.Files, dcfh.FormatHumanSize(rs.Bytes))
	drawText(s, 0, 0, width, style, header)
}

func (m *model) drawFooter(s tcell.Screen, width, y int) {
	style := tcell.StyleDefault.Dim(true)
	help := "↑/↓ move  →/← expand/collapse  c/f/a/m/d/n sort  r reverse  q quit"
	drawText(s, 0, y, width, style, help)
}

func (m *model) drawDivider(s tcell.Screen, x, top, bottom int) {
	style := tcell.StyleDefault.Dim(true)
	for y := top; y <= bottom; y++ {
		s.SetContent(x, y, tcell.RuneVLine, nil, style)
	}
}

func (m *model) drawTree(s tcell.Screen, top, bottom, width int) {
	if len(m.rows) == 0 {
		drawText(s, 0, top, width, tcell.StyleDefault.Dim(true), "(no changes to display)")
		return
	}
	y := top
	for i := m.top; i < len(m.rows) && y <= bottom; i++ {
		row := m.rows[i]
		selected := i == m.sel
		m.drawRow(s, y, width, row, selected)
		y++
	}
}

func (m *model) drawRow(s tcell.Screen, y, width int, row rowItem, selected bool) {
	glyph, base := nodeStyle(row.node)
	if selected {
		base = base.Reverse(true)
	}
	// Paint the whole row background first so selection spans the pane.
	for x := range width {
		s.SetContent(x, y, ' ', nil, base)
	}

	indent := row.depth * 2
	marker := "  "
	if row.node.IsDir {
		if m.expanded[row.node] {
			marker = "▾ "
		} else {
			marker = "▸ "
		}
	}
	label := row.node.Label
	if row.node.IsDir {
		label += "/"
	}
	left := fmt.Sprintf("%*s%s%c %s", indent, "", marker, glyph, label)
	x := drawText(s, 0, y, width, base, left)

	// Right-aligned value tracks the active sort metric: change volume
	// (human size) for change_bytes/name, else the integer count.
	colVal := columnText(row.node, m.sortKey)
	colX := width - len(colVal)
	if colX > x+1 {
		drawText(s, colX, y, width-colX, base, colVal)
	}
}

func (m *model) drawStats(s tcell.Screen, x, top, bottom, width int) {
	maxW := width - x
	if maxW <= 0 {
		return
	}
	n := m.current()
	if n == nil {
		drawText(s, x+1, top, maxW-1, tcell.StyleDefault.Dim(true), "(nothing selected)")
		return
	}
	kind := "file"
	if n.IsDir {
		kind = "dir"
	}
	st := n.Stats
	lines := []struct {
		label string
		style tcell.Style
	}{
		{"  Selected: " + n.Label, tcell.StyleDefault.Bold(true)},
		{"  Type:      " + kind, tcell.StyleDefault},
		{"", tcell.StyleDefault},
		{fmt.Sprintf("  Files:     %d", st.Files), tcell.StyleDefault},
		{"  Size:      " + dcfh.FormatHumanSize(st.Bytes), tcell.StyleDefault},
		{"", tcell.StyleDefault},
		{fmt.Sprintf("+ Added:     %d (%s)", st.Added, dcfh.FormatHumanSize(st.AddedBytes)), styleAdded},
		{fmt.Sprintf("~ Modified:  %d (%s)", st.Modified, dcfh.FormatHumanSize(st.ModifiedBytes)), styleModified},
		{fmt.Sprintf("- Deleted:   %d (%s)", st.Deleted, dcfh.FormatHumanSize(st.DeletedBytes)), styleDeleted},
		{fmt.Sprintf("  Unchanged: %d", st.Unchanged), tcell.StyleDefault},
		{"* mixed (directory)", tcell.StyleDefault.Dim(true)},
	}
	y := top
	for _, ln := range lines {
		if y > bottom {
			break
		}
		drawText(s, x+1, y, maxW-1, ln.style, ln.label)
		y++
	}
}

// drawText writes already-sanitised text from (x,y), clipped to maxW
// columns on rune boundaries. It never re-derives a display string from
// a raw path — callers pass sanitised labels (dcfh.sanitiseLabel) or
// strings this package built from safe numeric/enum data.
func drawText(s tcell.Screen, x, y, maxW int, style tcell.Style, text string) int {
	if maxW <= 0 {
		return x
	}
	col := 0
	for _, r := range text {
		if col >= maxW {
			break
		}
		s.SetContent(x+col, y, r, nil, style)
		col++
	}
	return x + col
}

var (
	styleAdded    = tcell.StyleDefault.Foreground(tcell.ColorGreen)
	styleModified = tcell.StyleDefault.Foreground(tcell.ColorBlue)
	styleDeleted  = tcell.StyleDefault.Foreground(tcell.ColorRed)
)

// nodeStyle maps a node's present change-category set (Stats counts > 0) to its
// status glyph and base style (foreground colour + bold). Pure; identical for
// leaf and dir. Unchanged → (' ', default). Glyph is always one of
// '+','~','-','*',' ' — never a control rune (drawText sanitised-string contract).
// Colours are ANSI-palette names (0–15) so terminal themes remap them.
func nodeStyle(n *dcfh.Node) (rune, tcell.Style) {
	const (
		bA = 1 << iota // added
		bM             // modified
		bD             // deleted
	)
	set := 0
	if n.Stats.Added > 0 {
		set |= bA
	}
	if n.Stats.Modified > 0 {
		set |= bM
	}
	if n.Stats.Deleted > 0 {
		set |= bD
	}
	var (
		glyph  rune
		colour tcell.Color
	)
	switch set {
	case 0:
		return ' ', tcell.StyleDefault
	case bA:
		glyph, colour = '+', tcell.ColorGreen
	case bM:
		glyph, colour = '~', tcell.ColorBlue
	case bD:
		glyph, colour = '-', tcell.ColorRed
	case bA | bM:
		glyph, colour = '*', tcell.ColorAqua // cyan
	case bM | bD:
		glyph, colour = '*', tcell.ColorFuchsia // magenta
	case bA | bD:
		glyph, colour = '*', tcell.ColorYellow
	default: // bA | bM | bD
		glyph, colour = '*', tcell.ColorWhite
	}
	return glyph, tcell.StyleDefault.Foreground(colour).Bold(true)
}
