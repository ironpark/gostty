package main

import (
	"fmt"
	"image/color"
	"time"

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

// marked reports whether a row needs repainting, without claiming it.
func (r *redrawSet) marked(row int) bool {
	return r.all || (row >= 0 && row < len(r.rows) && r.rows[row])
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
	if err := tab.refreshCursor(); err != nil {
		return err
	}
	if err := tab.refreshLink(); err != nil {
		return err
	}
	if err := tab.refreshScrollbar(); err != nil {
		return err
	}
	tab.refreshBlink()
	// Last, because it follows the rows the steps above marked for redraw.
	return tab.refreshClusters()
}

// refreshBlink advances the blink phase and marks the rows that have to be
// drawn again because of it.
//
// The terminal reports a cell as blinking and stops there, since when it is
// dark is a question about a clock rather than about the screen. That makes it
// this side's business to repaint: the rows holding blinking cells are marked
// on each half of the phase, and no others, so a screen with nothing blinking
// costs nothing.
func (tab *terminalTab) refreshBlink() {
	lit := blinkLit(time.Now())
	if lit == tab.blink {
		return
	}
	tab.blink = lit
	for row := 0; row < tab.rows; row++ {
		for col := 0; col < tab.cols; col++ {
			i := row*tab.cols + col
			if i >= len(tab.cells) {
				break
			}
			if tab.cells[i].Flags.Blink {
				tab.redraw.mark(row)
				break
			}
		}
	}
}

// The longest cluster a cell is read back as. Ghostty caps what it stores; this
// only has to be longer than anything worth drawing, and a family with four
// people and skin tones is nine.
const maxClusterRunes = 32

// refreshClusters reads back the cells that hold more than one codepoint.
//
// A `RenderCell` carries one codepoint, which is the base of the cluster: the
// combining acute on an "e", the second half of a flag and the joiners in a
// family are all still in the terminal. `Graphemes` is what hands them over,
// and it is per cell, so it is asked only about the rows that are going to be
// drawn again -- the same rows `drawGrid` repaints, for the same reason.
func (tab *terminalTab) refreshClusters() error {
	if tab.clusters == nil {
		tab.clusters = make(map[int]string)
		tab.clusterBuf = make([]rune, maxClusterRunes)
	}
	for row := 0; row < tab.rows; row++ {
		if !tab.redraw.marked(row) {
			continue
		}
		for col := 0; col < tab.cols; col++ {
			i := row*tab.cols + col
			if i >= len(tab.cells) {
				break
			}
			delete(tab.clusters, i)
			cell := tab.cells[i]
			// Blanks and the spacers of a wide cell have no text of their own,
			// and an ASCII letter cannot be the base of anything: skipping them
			// is most of the grid.
			if cell.Codepoint <= ' ' || cell.Flags.Wide == gostty.CellWidthSpacerTail ||
				cell.Flags.Wide == gostty.CellWidthSpacerHead {
				continue
			}
			n, err := tab.state.Graphemes(uint16(col), uint16(row), tab.clusterBuf)
			if err != nil {
				return err
			}
			if n > 1 {
				tab.clusters[i] = string(tab.clusterBuf[:min(n, uint(len(tab.clusterBuf)))])
			}
		}
	}
	return nil
}

// clusterAt is the text of one cell: its cluster where it has one, and its
// codepoint otherwise. Draw calls it, so it reads what refreshClusters left
// rather than the terminal.
func (tab *terminalTab) clusterAt(col, row int, cell gostty.RenderCell) string {
	if cluster, ok := tab.clusters[row*tab.cols+col]; ok {
		return cluster
	}
	return glyphString(cell.Codepoint)
}

// refreshColors reads the terminal's default colors and resolves them through
// the theme. A change to either repaints the whole grid, since every cell with
// default colors is drawn from them.
func (tab *terminalTab) refreshColors() error {
	prevBg, prevFg := tab.bg, tab.fg
	colors, err := tab.state.Colors()
	if err != nil {
		return err
	}
	tab.terminalBg = rgb(colors.Background)
	tab.terminalFg = rgb(colors.Foreground)
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
	cursor, err := tab.state.Cursor()
	if err != nil {
		return err
	}
	tab.cursor = cursorState{
		x: cursor.X, y: cursor.Y,
		visible:  cursor.Visible && cursor.ViewportHasValue,
		style:    cursor.Style,
		blinking: cursor.Blinking,
		wideTail: cursor.WideTail,
		password: cursor.PasswordInput,
	}
	// The colour is separate because it is optional: a program that has not
	// set one leaves the cursor to be drawn in the foreground colour, which is
	// this window's decision rather than the terminal's.
	rgba, ok, err := tab.state.CursorColor()
	if err != nil {
		return err
	}
	tab.cursor.color, tab.cursor.hasColor = rgb(rgba), ok
	return nil
}

func rgb(v uint32) color.RGBA {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}
