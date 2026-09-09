package main

import (
	"fmt"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// redrawSet is what the next drawGrid has to repaint. The render state says
// which rows the terminal changed; everything drawn from this side -- theme,
// font, match highlight -- has to mark itself, because the terminal cannot
// know about it.
type redrawSet struct {
	all  bool
	rows []bool
	// The rows the terminal itself changed, as opposed to the ones marked from
	// this side. Kept apart because what a row says is only stale when the
	// terminal wrote to it: a theme change repaints every row without changing
	// a single character on any of them.
	changed    []bool
	changedAll bool
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
		r.changed = make([]bool, rows)
		r.scratch = make([]uint16, rows)
		r.all, r.changedAll = true, true
	}
}

// pull takes the render state's dirty rows and marks the state clean, so the
// next Update reports only what changes from here. Full dirt -- colors or size
// changed -- redraws every row.
func (r *redrawSet) pull(state *gostty.RenderState) error {
	clear(r.changed)
	dirty, err := state.Dirty()
	if err != nil {
		return err
	}
	r.changedAll = dirty == gostty.RenderDirtyFull
	if r.changedAll {
		r.all = true
	}
	n, err := state.DirtyRows(r.scratch)
	if err != nil {
		return err
	}
	for _, y := range r.scratch[:n] {
		r.mark(int(y))
		if int(y) < len(r.changed) {
			r.changed[int(y)] = true
		}
	}
	return state.Clean()
}

// rewritten reports whether the terminal changed this row since the last
// frame, which is a narrower question than whether it has to be repainted.
func (r *redrawSet) rewritten(row int) bool {
	return r.changedAll || (row >= 0 && row < len(r.changed) && r.changed[row])
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

// viewportTop is the scrollback row the top of the screen is showing, which is
// what turns a position in the scrollback into a row on the grid.
func (tab *terminalTab) viewportTop() (uint32, error) {
	var top uint32
	err := tab.onScreen(func(screen *gostty.Screen) error {
		var err error
		top, err = screen.ViewportTop()
		return err
	})
	return top, err
}

// revealRow brings a scrollback row onto the screen, and leaves the viewport
// alone when it is already there.
//
// Both the search and a keyboard selection move to somewhere in the
// scrollback and want to see it. Scrolling unconditionally would move the page
// under the user every time they stepped between two things already in front
// of them.
func (tab *terminalTab) revealRow(row uint32) error {
	top, err := tab.viewportTop()
	if err != nil {
		return err
	}
	if row >= top && row < top+uint32(tab.rows) {
		return nil
	}
	return tab.vt.ScrollViewport(gostty.ScrollViewportRow(uint(row)))
}

// refresh pulls the viewport out of the render state. This is the only place
// cell data crosses the boundary, and it is one call for the whole grid.
//
// It runs in three phases, and the boundaries between them are the only order
// that matters. Everything reads the cells the first phase took, so nothing
// can come before it; everything that marks a row for redraw has to have done
// so before the last phase, which reads back the text of the rows that are
// going to be drawn again. A step put in the wrong phase is wrong quietly --
// the frame is one behind rather than broken -- so the phases are marked.
func (tab *terminalTab) refresh() error {
	g := tab.grid()
	if err := tab.frame.read(tab.state, tab.vt, g); err != nil {
		return err
	}
	// Derived from the cells: none of these reads what another one writes, so
	// the order between them is free.
	if err := tab.tickSearch(); err != nil {
		return err
	}
	if err := tab.refreshMatches(); err != nil {
		return err
	}
	if err := tab.images.refresh(); err != nil {
		return err
	}
	if err := tab.frame.readColors(tab.state, tab.currentTheme()); err != nil {
		return err
	}
	if err := tab.frame.readCursor(tab.state); err != nil {
		return err
	}
	if err := tab.refreshLink(); err != nil {
		return err
	}
	if err := tab.refreshScrollbar(); err != nil {
		return err
	}
	tab.frame.tickBlink(time.Now(), g)
	// Last: the rows to be redrawn are settled, and these are read per cell in
	// those rows alone.
	return tab.frame.readClusters(tab.state, g)
}

// read takes this frame's grid out of the terminal and settles which rows have
// to be drawn again, which is what the rest of the refresh works from.
func (f *frame) read(state *gostty.RenderState, term *gostty.Terminal, g grid) error {
	if err := state.Update(term); err != nil {
		return fmt.Errorf("render update: %w", err)
	}
	n, err := state.CellCount()
	if err != nil {
		return err
	}
	if uint(cap(f.cells)) < n {
		f.cells = make([]gostty.RenderCell, n)
	}
	f.cells = f.cells[:n]
	if _, err := state.Cells(f.cells); err != nil {
		return fmt.Errorf("render cells: %w", err)
	}
	f.redraw.resize(g.rows)
	return f.redraw.pull(state)
}

// tickBlink advances the blink phase and marks the rows that have to be
// drawn again because of it.
//
// The terminal reports a cell as blinking and stops there, since when it is
// dark is a question about a clock rather than about the screen. That makes it
// this side's business to repaint: the rows holding blinking cells are marked
// on each half of the phase, and no others, so a screen with nothing blinking
// costs nothing.
func (f *frame) tickBlink(now time.Time, g grid) {
	lit := blinkLit(now)
	if lit == f.blink {
		return
	}
	f.blink = lit
	for row := range g.rows {
		for col := range g.cols {
			i := g.index(col, row)
			if i >= len(f.cells) {
				break
			}
			if f.cells[i].Flags.Blink {
				f.redraw.mark(row)
				break
			}
		}
	}
}

// The longest cluster a cell is read back as. Ghostty caps what it stores; this
// only has to be longer than anything worth drawing, and a family with four
// people and skin tones is nine.
const maxClusterRunes = 32

// readClusters reads back the cells that hold more than one codepoint.
//
// A `RenderCell` carries one codepoint, which is the base of the cluster: the
// combining acute on an "e", the second half of a flag and the joiners in a
// family are all still in the terminal. `Graphemes` is what hands them over.
//
// It is per cell, and a cell is a cgo call, so the rows asked about are the
// ones the terminal rewrote rather than the ones about to be repainted. The
// two are usually the same set; where they differ -- a theme change, a font
// change, a search highlight, the blink phase, a rebuilt canvas -- the text
// has not moved, so what was read last time still stands. What is left is
// proportional to what the shell actually printed, which is the only part
// that cannot be avoided from here: a batched, row-at-a-time read in the
// binding is what would fix the rest.
func (f *frame) readClusters(state *gostty.RenderState, g grid) error {
	if len(f.clusters) != len(f.cells) {
		f.clusters = make([]string, len(f.cells))
	}
	var cluster [maxClusterRunes]rune
	for row := range g.rows {
		if !f.redraw.rewritten(row) {
			continue
		}
		for col := range g.cols {
			i := g.index(col, row)
			if i >= len(f.cells) {
				break
			}
			f.clusters[i] = ""
			cell := f.cells[i]
			// Blanks and the spacers of a wide cell have no text of their own:
			// skipping them is most of the grid.
			if cell.Codepoint <= ' ' || cell.Flags.Wide == gostty.CellWidthSpacerTail ||
				cell.Flags.Wide == gostty.CellWidthSpacerHead {
				continue
			}
			n, err := state.Graphemes(uint16(col), uint16(row), cluster[:])
			if err != nil {
				return err
			}
			if n > 1 {
				f.clusters[i] = string(cluster[:min(n, uint(len(cluster)))])
			}
		}
	}
	return nil
}

// clusterAt is the text of one cell: its cluster where it has one, and its
// codepoint otherwise. Draw calls it, so it reads what readClusters left rather
// than the terminal.
func (f *frame) clusterAt(g grid, col, row int, cell gostty.RenderCell) string {
	if i := g.index(col, row); i < len(f.clusters) && f.clusters[i] != "" {
		return f.clusters[i]
	}
	return glyphString(cell.Codepoint)
}

// readColors reads the terminal's default colors and resolves them through
// the theme. A change to either repaints the whole grid, since every cell with
// default colors is drawn from them.
func (f *frame) readColors(state *gostty.RenderState, theme ui.Theme) error {
	previous := f.colors
	colors, err := state.Colors()
	if err != nil {
		return err
	}
	f.colors.terminalBg = ui.RGB(colors.Background)
	f.colors.terminalFg = ui.RGB(colors.Foreground)
	if theme.Terminal {
		f.colors.bg, f.colors.fg = f.colors.terminalBg, f.colors.terminalFg
	} else {
		f.colors.bg, f.colors.fg = theme.Background, theme.Foreground
	}
	if f.colors.bg != previous.bg || f.colors.fg != previous.fg {
		f.redraw.markAll()
	}
	return nil
}

// readCursor reads the cursor out of the render state, so Draw does not have
// to reach across the boundary from a place that cannot report a failure.
func (f *frame) readCursor(state *gostty.RenderState) error {
	cursor, err := state.Cursor()
	if err != nil {
		return err
	}
	f.cursor = cursorState{
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
	rgba, ok, err := state.CursorColor()
	if err != nil {
		return err
	}
	f.cursor.color, f.cursor.hasColor = ui.RGB(rgba), ok
	return nil
}
