package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/input"
)

func (app *terminalApp) barHeight() float64 { return float64(int(ui.TabBarHeight * app.dsf)) }

func (app *terminalApp) LayoutF(width, height float64) (float64, float64) {
	app.dsf = deviceScale()
	app.width, app.height = width*app.dsf, height*app.dsf
	app.layoutTabs()
	return app.width, app.height
}

func (app *terminalApp) Layout(width, height int) (int, int) {
	w, h := app.LayoutF(float64(width), float64(height))
	return int(w), int(h)
}

func (app *terminalApp) layoutTabs() {
	// The faces are built in device pixels, so a window that moved to a display
	// with a different scale factor needs them rebuilt -- once, for every tab.
	if app.settings.dsf != app.dsf {
		app.applyFont()
	}
	for _, tab := range app.tabs {
		tab.offsetY = int(app.barHeight())
		if app.width > 0 {
			tab.layout(app.width, max(app.height-app.barHeight(), 1))
		}
	}
}

// layout resizes a tab to the content area below the tab bar.
func (tab *terminalTab) layout(width, height float64) {
	g := tab.grid()
	cols := max(int(width/g.cellW), 1)
	rows := max(int(height/g.cellH), 1)
	if cols != tab.cols || rows != tab.rows || tab.relayout {
		if err := tab.resize(cols, rows); err != nil {
			log.Printf("resize to %dx%d: %v", cols, rows, err)
			return
		}
		tab.relayout = false
	}
}

func (tab *terminalTab) cursorPosition() (int, int) {
	x, y := ebiten.CursorPosition()
	return x, y - tab.offsetY
}

// resize moves the emulated screen and the pty together. They have to agree:
// the program asks the pty how big it is and writes for the terminal.
func (tab *terminalTab) resize(cols, rows int) error {
	tab.cols, tab.rows = cols, rows
	g := tab.grid()
	// `ResizeCells` rather than `Resize`: a Kitty image sized in cells is
	// measured in pixels through the cell size, and the terminal stores the
	// pixel size of the whole grid, so it goes stale on every column change.
	if err := tab.vt.ResizeCells(uint16(cols), uint16(rows), uint32(g.cellW), uint32(g.cellH)); err != nil {
		return err
	}
	// The selection gesture measures the pointer in pixels, so it is told the
	// new geometry rather than left to work from a stale cell size.
	if err := tab.syncGestureGeometry(); err != nil {
		return err
	}
	// The pty carries the same size, which is where a program that has not
	// asked the terminal directly reads it from.
	return tab.shell.Pty.Resize(cols, rows)
}

// deviceScale is how many pixels the display has per device-independent pixel.
//
// One before there is a window to ask about, which is the case for the first
// font load: the first LayoutF picks up the real answer and reloads, so the
// only cost of guessing is one frame at the wrong size.
func deviceScale() float64 {
	monitor := ebiten.Monitor()
	if monitor == nil {
		return 1
	}
	if scale := monitor.DeviceScaleFactor(); scale > 0 {
		return scale
	}
	return 1
}

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
// render state hands cells over, and row is the way back.
func (g grid) index(col, row int) int { return row*g.cols + col }
func (g grid) row(index int) int      { return index / g.cols }

// holds reports whether a slice of cells covers the whole grid, which is what
// anything indexing into one has to know before it does.
func (g grid) holds(cells int) bool { return cells >= g.cols*g.rows }

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
