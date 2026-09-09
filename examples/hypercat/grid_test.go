package main

import "testing"

// A 10x20 cell, which is what the tab tests are built on.
var testGrid = grid{cols: 80, rows: 24, cellW: 10, cellH: 20}

func TestGridPixelsAndCells(t *testing.T) {
	if got, want := testGrid.x(3), 30.0; got != want {
		t.Errorf("x(3) = %v, want %v", got, want)
	}
	if got, want := testGrid.y(2), 40.0; got != want {
		t.Errorf("y(2) = %v, want %v", got, want)
	}
	if got, want := testGrid.width(), 800.0; got != want {
		t.Errorf("width() = %v, want %v", got, want)
	}
	if got, want := testGrid.height(), 480.0; got != want {
		t.Errorf("height() = %v, want %v", got, want)
	}
	if got, want := testGrid.index(3, 2), 163; got != want {
		t.Errorf("index(3, 2) = %d, want %d", got, want)
	}
}

// A pointer outside the grid still points at a cell: a drag that runs off the
// window selects to the edge rather than to nowhere.
func TestGridCellAtClampsToTheGrid(t *testing.T) {
	for _, test := range []struct{ px, py, col, row int }{
		{0, 0, 0, 0},
		{15, 25, 1, 1},
		{9, 19, 0, 0},        // inside the first cell
		{-40, -40, 0, 0},     // above and left of the grid
		{5000, 5000, 79, 23}, // past the last cell
	} {
		col, row := testGrid.cellAt(test.px, test.py)
		if col != test.col || row != test.row {
			t.Errorf("cellAt(%d, %d) = (%d, %d), want (%d, %d)",
				test.px, test.py, col, row, test.col, test.row)
		}
	}
}

// The size the input encoders are told is the same grid, so a mouse report
// lands on the cell the pointer is over.
func TestGridRenderSize(t *testing.T) {
	size := testGrid.renderSize()
	if size.CellWidth != 10 || size.CellHeight != 20 {
		t.Errorf("renderSize() cell = %dx%d, want 10x20", size.CellWidth, size.CellHeight)
	}
	if size.ScreenWidth != 800 || size.ScreenHeight != 480 {
		t.Errorf("renderSize() screen = %dx%d, want 800x480", size.ScreenWidth, size.ScreenHeight)
	}
}

// The tab builds its grid from its own column count and the window's font, so
// a resize or a font change is carried by both halves at once.
func TestTabGridFollowsTheTabAndTheFont(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()

	g := tab.grid()
	if g.cols != tab.cols || g.rows != tab.rows {
		t.Errorf("grid() = %dx%d cells, want the tab's %dx%d", g.cols, g.rows, tab.cols, tab.rows)
	}
	if g.cellW != tab.fonts().CellWidth || g.cellH != tab.fonts().CellHeight {
		t.Errorf("grid() cell = %vx%v, want the font set's %vx%v",
			g.cellW, g.cellH, tab.fonts().CellWidth, tab.fonts().CellHeight)
	}
	if err := tab.resize(40, 12); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if g := tab.grid(); g.cols != 40 || g.rows != 12 {
		t.Errorf("grid() after a resize = %dx%d, want 40x12", g.cols, g.rows)
	}
}
