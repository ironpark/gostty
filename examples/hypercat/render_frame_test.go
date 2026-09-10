package main

import (
	"image/color"
	"testing"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/internal/frontend"
)

// A cell holds a grapheme cluster, not a codepoint. The cell says "e" and the
// combining acute is still in the terminal, so a renderer that draws the
// codepoint draws the wrong word.
func TestCellsCarryTheirWholeCluster(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	feedTab(t, tab, "e\u0301tude") // decomposed, so the acute is a cell of its own to a naive reader
	if got := tab.frame.cells[0].Codepoint; got != 'e' {
		t.Fatalf("first cell codepoint = %q, want the cluster's base", got)
	}
	if got, want := tab.presentation().ClusterAt(0, 0, tab.frame.cells[0]), "e\u0301"; got != want {
		t.Errorf("clusterAt(0, 0) = %q, want %q", got, want)
	}
	// A cell with nothing but its codepoint is not remembered as a cluster, so
	// the map stays the size of the rare case rather than of the grid.
	if got, want := tab.presentation().ClusterAt(1, 0, tab.frame.cells[1]), "t"; got != want {
		t.Errorf("clusterAt(1, 0) = %q, want %q", got, want)
	}
	if tab.frame.clusters[1] != "" {
		t.Error("a single-codepoint cell was recorded as a cluster")
	}
}

// With grapheme clustering on, an emoji written as several codepoints is one
// two-column cell rather than one cell per codepoint. It is the mode that puts
// it there, so this is really a test that the tab turns it on.
func TestGraphemeClusteringPutsAnEmojiInOneCell(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	if enabled, err := tab.vt.ModeEnabled(gostty.ModeGraphemeCluster); err != nil || !enabled {
		t.Fatalf("ModeEnabled(grapheme cluster) = %v, %v; want true", enabled, err)
	}

	feedTab(t, tab, "\U0001F1F0\U0001F1F7|")
	if got := tab.frame.cells[0].Flags.Wide; got != gostty.CellWidthWide {
		t.Errorf("the flag's cell is %v, want a wide one", got)
	}
	if got, want := tab.presentation().ClusterAt(0, 0, tab.frame.cells[0]), "\U0001F1F0\U0001F1F7"; got != want {
		t.Errorf("clusterAt(0, 0) = %q, want the whole flag %q", got, want)
	}
	// The next column is the wide cell's tail, so the text after it starts at
	// column two rather than at column one.
	if got := tab.frame.cells[2].Codepoint; got != '|' {
		t.Errorf("cell after the flag = %q, want the text to resume at column 2", got)
	}
}

// A full reset must not quietly take grapheme clustering away: it is set as the
// default, not only as the current value.
func TestGraphemeClusteringSurvivesAReset(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	feedTab(t, tab, "\x1bc")
	if enabled, err := tab.vt.ModeEnabled(gostty.ModeGraphemeCluster); err != nil || !enabled {
		t.Fatalf("ModeEnabled(grapheme cluster) after RIS = %v, %v; want true", enabled, err)
	}
}

// The cluster read is per cell and each cell is a call into the terminal, so
// it follows what the terminal rewrote rather than what has to be repainted.
// A theme change repaints every row without changing a character on any of
// them, and must not re-read the whole grid to be told so.
func TestClustersAreNotRereadForARepaint(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	feedTab(t, tab, "e\u0301tude")

	// A repaint with no new output: every row is marked, none was rewritten.
	tab.frame.redraw.MarkAll()
	if err := tab.refreshSnapshot(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !tab.frame.redraw.all {
		t.Fatal("the repaint was dropped")
	}
	if tab.frame.redraw.rewritten(0) {
		t.Error("a row nothing was printed to counts as rewritten")
	}
	// The text did not move, so what was read before still stands.
	if got, want := tab.presentation().ClusterAt(0, 0, tab.frame.cells[0]), "e\u0301"; got != want {
		t.Errorf("clusterAt after a repaint = %q, want %q", got, want)
	}
}

// Only the rows that are going to be drawn again are asked for their clusters,
// so a cluster that scrolled off is forgotten rather than left pointing at a
// cell that now says something else.
func TestClustersFollowTheRowsThatChanged(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	feedTab(t, tab, "e\u0301\r\n")
	if tab.frame.clusters[0] == "" {
		t.Fatal("the cluster was not recorded")
	}
	feedTab(t, tab, "\x1b[H\x1b[2Kplain")
	if tab.frame.clusters[0] != "" {
		t.Error("the cluster outlived the text it belonged to")
	}
}

// Whether the cursor blinks is the terminal's answer to DECSCUSR, not a fixed
// policy of the renderer: a program that asks for a steady cursor gets one.
func TestCursorBlinkFollowsTheTerminal(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	feedTab(t, tab, "\x1b[1 q") // blinking block
	if !tab.frame.cursor.blinking {
		t.Error("cursor is steady after DECSCUSR 1, want blinking")
	}
	feedTab(t, tab, "\x1b[2 q") // steady block
	if tab.frame.cursor.blinking {
		t.Error("cursor blinks after DECSCUSR 2, want steady")
	}
	if tab.frame.cursor.style != gostty.CursorStyleBlock {
		t.Errorf("cursor style = %v, want a block", tab.frame.cursor.style)
	}
}

// A steady cursor is always drawn; a blinking one is drawn for half of each
// period, from the clock rather than from a frame counter.
func TestCursorLitHalfThePeriod(t *testing.T) {
	base := time.Unix(0, 0)
	if !frontend.CursorLit(false, base.Add(frontend.CursorBlinkPeriod*3/4)) {
		t.Error("a steady cursor went dark")
	}
	if !frontend.CursorLit(true, base) {
		t.Error("a blinking cursor is dark at the start of its period")
	}
	if frontend.CursorLit(true, base.Add(frontend.CursorBlinkPeriod*3/4)) {
		t.Error("a blinking cursor is lit in the second half of its period")
	}
	if !frontend.CursorLit(true, base.Add(frontend.CursorBlinkPeriod+1)) {
		t.Error("the next period did not start lit")
	}
}

// SGR 5 marks a cell as blinking and stops there: when it is dark is a
// question about a clock, so it is this side's to answer -- and to repaint for,
// since the terminal marks no row dirty when the phase turns over.
func TestBlinkingCellsRepaintOnThePhase(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	feedTab(t, tab, "\x1b[5mblinking\x1b[0m")
	if !tab.frame.cells[0].Flags.Blink {
		t.Fatal("the cell is not marked as blinking")
	}
	tab.frame.redraw.Clear()

	// Turning the phase over marks the row holding the blinking cell.
	tab.frame.blink = !frontend.BlinkLit(time.Now())
	tab.frame.tickBlink(time.Now(), tab.grid())
	if !tab.frame.redraw.marked(0) {
		t.Error("the row with the blinking cell was not repainted")
	}

	// A screen with nothing blinking costs nothing.
	feedTab(t, tab, "\x1b[2J\x1b[Hsteady")
	tab.frame.redraw.Clear()
	tab.frame.blink = !frontend.BlinkLit(time.Now())
	tab.frame.tickBlink(time.Now(), tab.grid())
	if tab.frame.redraw.marked(0) {
		t.Error("a row with nothing blinking was repainted for the phase")
	}
}

// OSC 12 sets the cursor's colour. Without reading it back the cursor is drawn
// in the foreground colour whatever the program asked for.
func TestCursorTakesTheColorTheProgramSet(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	if tab.frame.cursor.hasColor {
		t.Fatal("a cursor colour was reported before the program set one")
	}
	feedTab(t, tab, "\x1b]12;#ff0000\x07")
	if !tab.frame.cursor.hasColor {
		t.Fatal("no cursor colour after OSC 12")
	}
	if want := (color.RGBA{R: 0xff, A: 0xff}); tab.frame.cursor.color != want {
		t.Errorf("cursor colour = %v, want %v", tab.frame.cursor.color, want)
	}
}

// The cursor on the tail of a wide character covers both of its cells, so the
// glyph underneath is not left half-lit. The index it draws the character from
// is the head's, not the tail's.
func TestCursorOnAWideCharacter(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	feedTab(t, tab, "한\x1b[1D") // print it, then step back onto its tail
	if !tab.frame.cursor.wideTail {
		t.Fatal("cursor is not on the wide character's tail")
	}
	i := tab.presentation().CursorCellIndex()
	if i < 0 || tab.frame.cells[i].Codepoint != '한' {
		t.Errorf("cursorCellIndex() = %d, want the cell holding the character", i)
	}
}

func TestRedrawSetFollowsTheGrid(t *testing.T) {
	var r redrawSet
	r.resize(3)
	if !r.all {
		t.Fatal("a new grid should be redrawn whole")
	}
	r.Clear()
	r.mark(1)
	r.mark(7) // off the grid: ignored rather than a panic
	r.mark(-1)
	if r.all || !r.rows[1] || r.rows[0] || r.rows[2] {
		t.Fatalf("rows = %v all = %v", r.rows, r.all)
	}
	r.resize(3) // same size keeps its marks
	if r.all || !r.Take(1) || r.Take(1) {
		t.Fatal("resizing to the same size lost the marks, or take did not claim its row")
	}
	r.resize(4)
	if !r.all || len(r.rows) != 4 || len(r.scratch) != 4 {
		t.Fatal("resizing did not follow the grid")
	}
}

func TestMatchChangesMarkOnlyTheRowsThatChanged(t *testing.T) {
	tab := &terminal{cols: 4, rows: 3}
	// The highlight is per cell, so this needs the column count and nothing
	// about the font.
	g := grid{cols: 4, rows: 3}
	tab.frame.redraw.resize(3)
	tab.frame.redraw.Clear()
	tab.search.previous = []bool{false, false, false, false, true, false, false, false, false, false, false, false}
	tab.search.cells = []bool{false, false, false, false, true, false, false, false, false, false, true, false}
	tab.markMatchChanges(g)
	if tab.frame.redraw.rows[0] || tab.frame.redraw.rows[1] || !tab.frame.redraw.rows[2] {
		t.Fatalf("rows = %v, want only the third", tab.frame.redraw.rows)
	}
	// A cleared search leaves a shorter (empty) set; the old rows still repaint.
	tab.frame.redraw.Clear()
	tab.search.cells = nil
	tab.markMatchChanges(g)
	if tab.frame.redraw.rows[0] || !tab.frame.redraw.rows[1] || tab.frame.redraw.rows[2] {
		t.Fatalf("rows = %v, want only the second", tab.frame.redraw.rows)
	}
}
