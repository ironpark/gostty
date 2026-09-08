package main

import (
	"fmt"
	"image/color"

	"github.com/ironpark/gostty"
)

// redrawSet is what the next drawGrid has to repaint. The render state says
// which rows the terminal changed; everything drawn from this side -- theme,
// font, match highlight -- has to mark itself, because the terminal cannot
// know about it.
type redrawSet struct {
	all  bool
	rows []bool
	// Scratch for RenderState.DirtyRows, sized with the grid.
	scratch []uint16
}

func (r *redrawSet) markAll() { r.all = true }

func (r *redrawSet) mark(row int) {
	if row >= 0 && row < len(r.rows) {
		r.rows[row] = true
	}
}

// resize follows the grid; a grid of a new size is redrawn whole.
func (r *redrawSet) resize(rows int) {
	if len(r.rows) != rows {
		r.rows = make([]bool, rows)
		r.scratch = make([]uint16, rows)
		r.all = true
	}
}

// pull takes the render state's dirty rows and marks the state clean, so the
// next Update reports only what changes from here. Full dirt -- colors or size
// changed -- redraws every row.
func (r *redrawSet) pull(state *gostty.RenderState) error {
	dirty, err := state.Dirty()
	if err != nil {
		return err
	}
	if dirty == gostty.RenderDirtyFull {
		r.all = true
	}
	n, err := state.DirtyRows(r.scratch)
	if err != nil {
		return err
	}
	for _, y := range r.scratch[:n] {
		r.mark(int(y))
	}
	return state.Clean()
}

// take reports whether a row needs repainting, and claims it.
func (r *redrawSet) take(row int) bool {
	if row < 0 || row >= len(r.rows) || !r.rows[row] {
		return false
	}
	r.rows[row] = false
	return true
}

// clear is called once the grid has been repainted whole.
func (r *redrawSet) clear() {
	r.all = false
	clear(r.rows)
}

// refresh pulls the viewport out of the render state. This is the only place
// cell data crosses the boundary, and it is one call for the whole grid.
func (tab *terminalTab) refresh() error {
	if err := tab.state.Update(tab.vt); err != nil {
		return fmt.Errorf("render update: %w", err)
	}
	n, err := tab.state.CellCount()
	if err != nil {
		return err
	}
	if uint(cap(tab.cells)) < n {
		tab.cells = make([]gostty.RenderCell, n)
	}
	tab.cells = tab.cells[:n]
	if _, err := tab.state.Cells(tab.cells); err != nil {
		return fmt.Errorf("render cells: %w", err)
	}
	if err := tab.tickSearch(); err != nil {
		return err
	}
	tab.redraw.resize(tab.rows)
	if err := tab.refreshMatches(); err != nil {
		return err
	}
	if err := tab.redraw.pull(tab.state); err != nil {
		return err
	}
	if err := tab.images.refresh(); err != nil {
		return err
	}
	if err := tab.refreshColors(); err != nil {
		return err
	}
	return tab.refreshCursor()
}

// refreshColors reads the terminal's default colors and resolves them through
// the theme. A change to either repaints the whole grid, since every cell with
// default colors is drawn from them.
func (tab *terminalTab) refreshColors() error {
	prevBg, prevFg := tab.bg, tab.fg
	bg, err := tab.state.Background()
	if err != nil {
		return err
	}
	tab.terminalBg = rgb(bg)
	fg, err := tab.state.Foreground()
	if err != nil {
		return err
	}
	tab.terminalFg = rgb(fg)
	theme := tab.currentTheme()
	if theme.Terminal {
		tab.bg, tab.fg = tab.terminalBg, tab.terminalFg
	} else {
		tab.bg, tab.fg = theme.Background, theme.Foreground
	}
	if tab.bg != prevBg || tab.fg != prevFg {
		tab.redraw.markAll()
	}
	return nil
}

// refreshCursor reads the cursor out of the render state, so Draw does not have
// to reach across the boundary from a place that cannot report a failure.
func (tab *terminalTab) refreshCursor() error {
	visible, err := tab.state.CursorVisible()
	if err != nil {
		return err
	}
	x, onScreen, err := tab.state.CursorX()
	if err != nil {
		return err
	}
	y, _, err := tab.state.CursorY()
	if err != nil {
		return err
	}
	style, err := tab.state.CursorStyle()
	if err != nil {
		return err
	}
	// `onScreen` is false when the viewport has been scrolled away from it.
	tab.cursor = cursorState{x: x, y: y, visible: visible && onScreen, style: style}
	return nil
}

func rgb(v uint32) color.RGBA {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}
