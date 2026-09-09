package main

import "github.com/ironpark/gostty/input"

// grid is the shape of the cell grid: how many cells there are and how big one
// is in pixels.
//
// The two halves of that answer come from different places -- the column and
// row count are the tab's, the cell size is the font set's -- and turning them
// into pixels is arithmetic a terminal does everywhere: to draw a glyph, to
// place a Kitty image, to say which cell the pointer is over, to tell the
// selection gesture the shape of the window, and to tell a program how big its
// screen is. Written out at each of those, the copies drift, and a copy that
// drifts puts the selection somewhere other than where the text is.
//
// It is a value built for the moment it is used rather than state kept in step:
// there is one source for each half, and this is only their product.
type grid struct {
	cols, rows   int
	cellW, cellH float64
}

// grid is this tab's, in the window's current font.
func (tab *terminalTab) grid() grid {
	return grid{
		cols: tab.cols, rows: tab.rows,
		cellW: tab.fonts().CellWidth, cellH: tab.fonts().CellHeight,
	}
}

// x and y are the top-left corner of a cell.
func (g grid) x(col int) float64 { return float64(col) * g.cellW }
func (g grid) y(row int) float64 { return float64(row) * g.cellH }

// width and height are the whole grid, in pixels.
func (g grid) width() float64  { return g.x(g.cols) }
func (g grid) height() float64 { return g.y(g.rows) }

// cellAt maps a pixel position to a cell, clamped to the grid so a drag that
// runs off the window still points at the edge cell rather than at nothing.
func (g grid) cellAt(px, py int) (int, int) {
	col := min(max(int(float64(px)/g.cellW), 0), g.cols-1)
	row := min(max(int(float64(py)/g.cellH), 0), g.rows-1)
	return col, row
}

// index is the offset of a cell in a row-major grid of them, which is how the
// render state hands cells over.
func (g grid) index(col, row int) int { return row*g.cols + col }

// renderSize describes the window to the input encoders, which need it to turn
// a pixel position into a cell of their own. There is no padding around the
// grid here, so the only interesting fields are the cell size.
func (g grid) renderSize() input.RenderSize {
	return input.RenderSize{
		ScreenWidth:  uint32(g.width()),
		ScreenHeight: uint32(g.height()),
		CellWidth:    uint32(g.cellW),
		CellHeight:   uint32(g.cellH),
	}
}
