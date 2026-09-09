package main

import (
	"testing"

	"github.com/ironpark/gostty"
)

// A cell holds a grapheme cluster, not a codepoint. The cell says "e" and the
// combining acute is still in the terminal, so a renderer that draws the
// codepoint draws the wrong word.
func TestCellsCarryTheirWholeCluster(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()

	feedTab(t, tab, "e\u0301tude") // decomposed, so the acute is a cell of its own to a naive reader
	if got := tab.frame.cells[0].Codepoint; got != 'e' {
		t.Fatalf("first cell codepoint = %q, want the cluster's base", got)
	}
	if got, want := tab.frame.clusterAt(tab.grid(), 0, 0, tab.frame.cells[0]), "e\u0301"; got != want {
		t.Errorf("clusterAt(0, 0) = %q, want %q", got, want)
	}
	// A cell with nothing but its codepoint is not remembered as a cluster, so
	// the map stays the size of the rare case rather than of the grid.
	if got, want := tab.frame.clusterAt(tab.grid(), 1, 0, tab.frame.cells[1]), "t"; got != want {
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
	app := newTabTestApp(t)
	tab := app.current()

	if enabled, err := tab.vt.ModeEnabled(gostty.ModeGraphemeCluster); err != nil || !enabled {
		t.Fatalf("ModeEnabled(grapheme cluster) = %v, %v; want true", enabled, err)
	}

	feedTab(t, tab, "\U0001F1F0\U0001F1F7|")
	if got := tab.frame.cells[0].Flags.Wide; got != gostty.CellWidthWide {
		t.Errorf("the flag's cell is %v, want a wide one", got)
	}
	if got, want := tab.frame.clusterAt(tab.grid(), 0, 0, tab.frame.cells[0]), "\U0001F1F0\U0001F1F7"; got != want {
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
	app := newTabTestApp(t)
	tab := app.current()

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
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "e\u0301tude")

	// A repaint with no new output: every row is marked, none was rewritten.
	tab.frame.redraw.markAll()
	if err := tab.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !tab.frame.redraw.all {
		t.Fatal("the repaint was dropped")
	}
	if tab.frame.redraw.rewritten(0) {
		t.Error("a row nothing was printed to counts as rewritten")
	}
	// The text did not move, so what was read before still stands.
	if got, want := tab.frame.clusterAt(tab.grid(), 0, 0, tab.frame.cells[0]), "e\u0301"; got != want {
		t.Errorf("clusterAt after a repaint = %q, want %q", got, want)
	}
}

// Only the rows that are going to be drawn again are asked for their clusters,
// so a cluster that scrolled off is forgotten rather than left pointing at a
// cell that now says something else.
func TestClustersFollowTheRowsThatChanged(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()

	feedTab(t, tab, "e\u0301\r\n")
	if tab.frame.clusters[0] == "" {
		t.Fatal("the cluster was not recorded")
	}
	feedTab(t, tab, "\x1b[H\x1b[2Kplain")
	if tab.frame.clusters[0] != "" {
		t.Error("the cluster outlived the text it belonged to")
	}
}
