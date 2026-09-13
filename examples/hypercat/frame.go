package main

import (
	"fmt"
	"image/color"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/input"
)

// The grid -----------------------------------------------------------------------

// grid is the shape of the cell grid: how many cells, and how big one is in
// pixels. The count is the tab's and the cell size is the font's, and every
// place that turns one into the other -- drawing a glyph, placing an image,
// finding the cell under the pointer, sizing the pty -- goes through this so
// the copies cannot drift.
type grid struct {
	cols, rows   int
	cellW, cellH float64
}

func (tab *terminal) grid() grid {
	return grid{cols: tab.cols, rows: tab.rows, cellW: tab.settings.fonts.CellWidth, cellH: tab.settings.fonts.CellHeight}
}

// cursorPosition is the pointer relative to the grid, below the tab bar.
func (tab *terminal) cursorPosition() (int, int) { return tab.input.X, tab.input.Y - tab.offsetY }

func (g grid) x(col int) float64 { return float64(col) * g.cellW } // top-left corner of a cell
func (g grid) y(row int) float64 { return float64(row) * g.cellH }
func (g grid) width() float64    { return g.x(g.cols) }
func (g grid) height() float64   { return g.y(g.rows) }

// cellAt maps a pixel position to a cell, clamped so a drag that runs off the
// window still points at the edge cell.
func (g grid) cellAt(px, py int) (int, int) {
	col := min(max(int(float64(px)/g.cellW), 0), g.cols-1)
	row := min(max(int(float64(py)/g.cellH), 0), g.rows-1)
	return col, row
}

// index is a cell's offset in the row-major slice RenderState hands over.
func (g grid) index(col, row int) int { return row*g.cols + col }
func (g grid) row(index int) int      { return index / g.cols }
func (g grid) holds(cells int) bool   { return cells >= g.cols*g.rows }

// renderSize describes the window to the input encoders, which turn a pixel
// position into a cell of their own. There is no padding around the grid.
func (g grid) renderSize() input.RenderSize {
	return input.RenderSize{
		ScreenWidth: uint32(g.width()), ScreenHeight: uint32(g.height()),
		CellWidth: uint32(g.cellW), CellHeight: uint32(g.cellH),
	}
}

// The frame --------------------------------------------------------------------

// frame is what Draw paints. It is read from gostty during update, where
// errors can be returned; Draw uses the saved cells, clusters, colours and
// cursor and never touches a native handle.
type frame struct {
	// The viewport, row-major, and the text of the cells that hold more than
	// one codepoint, in the same order. Almost every cluster entry is empty,
	// and reading one costs a call into the terminal, so they are kept apart.
	cells    []gostty.RenderCell
	clusters []string

	cursor cursorState
	colors frameColors

	link      hoveredLink    // the OSC 8 link under the pointer
	scrollbar scrollbarState // where the viewport sits, and how long to show it
	blink     bool           // the lit half of the blink phase, one per frame

	redraw redrawSet // the rows the next Draw has to repaint
}

// frameColors is what the grid's default colours resolve to. Two pairs,
// because a theme is applied on this side: the terminal's own defaults are
// what a program means by "the default colour", and the themed pair is what
// that is drawn as.
type frameColors struct {
	terminalBg, terminalFg color.RGBA
	bg, fg                 color.RGBA
}

type cursorState struct {
	x, y     uint16
	visible  bool
	style    gostty.CursorStyle
	blinking bool // the terminal's answer to DECSCUSR, not a fixed policy
	wideTail bool // on the second half of a wide character
	password bool // the program is reading a secret: draw no glyph under the block
	color    color.RGBA
	hasColor bool // a program set one with OSC 12
}

// read captures the snapshot: cells, dirty rows, colours, cursor and clusters.
func (f *frame) read(state *gostty.RenderState, term *gostty.Terminal, g grid, theme ui.Theme) error {
	if err := state.Update(term); err != nil {
		return fmt.Errorf("render update: %w", err)
	}
	n, err := state.CellCount()
	if err != nil {
		return err
	}
	f.cells = grow(f.cells, int(n))
	if _, err := state.Cells(f.cells); err != nil {
		return fmt.Errorf("render cells: %w", err)
	}
	f.redraw.resize(g.rows)
	if err := f.redraw.pull(state); err != nil {
		return err
	}
	colors, err := f.readColors(state, theme)
	if err != nil {
		return err
	}
	if err := f.readCursor(state, colors); err != nil {
		return err
	}
	return f.readClusters(state, g)
}

// readColors resolves the terminal defaults through the theme and repaints
// the grid when the displayed defaults change.
func (f *frame) readColors(state *gostty.RenderState, theme ui.Theme) (gostty.RenderColors, error) {
	previous := f.colors
	colors, err := state.Colors()
	if err != nil {
		return gostty.RenderColors{}, err
	}
	f.colors.terminalBg, f.colors.terminalFg = ui.FromColor(colors.Background), ui.FromColor(colors.Foreground)
	if theme.Terminal {
		f.colors.bg, f.colors.fg = f.colors.terminalBg, f.colors.terminalFg
	} else {
		f.colors.bg, f.colors.fg = theme.Background, theme.Foreground
	}
	if f.colors.bg != previous.bg || f.colors.fg != previous.fg {
		f.redraw.markAll()
	}
	return colors, nil
}

func (f *frame) readCursor(state *gostty.RenderState, colors gostty.RenderColors) error {
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
		// A program that set no cursor colour leaves it to be drawn in the
		// foreground, which is this window's decision rather than the terminal's.
		color: ui.FromColor(colors.Cursor), hasColor: colors.CursorHasValue,
	}
	return nil
}

// The longest cluster a cell is read back as. A family of four with skin
// tones is nine codepoints.
const maxClusterRunes = 32

// readClusters fetches the full text behind each cell's base codepoint, for
// the rows the terminal rewrote. A theme, selection or blink change repaints
// rows without changing their text, and reuses what was read before.
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
			// Blanks and the spacers of a wide cell have no text of their own.
			if cell.Codepoint <= ' ' || cell.Flags.Wide == gostty.CellWidthSpacerTail ||
				cell.Flags.Wide == gostty.CellWidthSpacerHead || !cell.Flags.HasGrapheme {
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

// clusterAt is a cell's text: its cluster when it has one, otherwise its
// codepoint.
func (f *frame) clusterAt(i int) string {
	if i < len(f.clusters) && f.clusters[i] != "" {
		return f.clusters[i]
	}
	if i < 0 || i >= len(f.cells) {
		return ""
	}
	return glyphString(f.cells[i].Codepoint)
}

// cursorCellIndex is the cell the cursor's glyph is drawn from: the head of a
// wide character when the cursor sits on its tail, or -1 off the grid.
func (f *frame) cursorCellIndex(g grid) int {
	x, y := int(f.cursor.x), int(f.cursor.y)
	if f.cursor.wideTail {
		x--
	}
	i := g.index(x, y)
	if x < 0 || x >= g.cols || i < 0 || i >= len(f.cells) {
		return -1
	}
	return i
}

// Blinking: the terminal only records that a cell or the cursor blinks. When
// it is dark is a question about a clock, so it is answered here, from the
// clock rather than a frame counter so it does not speed up with the frame
// rate. Half a second each way is what the hardware terminals did.
const cursorBlinkPeriod = time.Second

func blinkLit(now time.Time) bool {
	return now.UnixNano()%int64(cursorBlinkPeriod) < int64(cursorBlinkPeriod/2)
}

func cursorLit(blinking bool, now time.Time) bool { return !blinking || blinkLit(now) }

// tickBlink repaints the rows holding SGR 5 cells when the phase turns over,
// since the terminal marks no row dirty for it.
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

// Damage --------------------------------------------------------------------------

// redrawSet is what the next Draw has to repaint. The render state says which
// rows the terminal changed; everything drawn from this side -- theme, font,
// match highlight, blink -- marks itself, because the terminal cannot know.
type redrawSet struct {
	all  bool
	rows []bool
	// The rows the terminal itself changed, kept apart from the ones marked
	// here: a row's text is only stale when the terminal wrote to it, and a
	// theme change repaints every row without changing a character.
	changed    []bool
	changedAll bool
	scratch    []uint16 // for RenderState.DirtyRows
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
		r.rows, r.changed, r.scratch = make([]bool, rows), make([]bool, rows), make([]uint16, rows)
		r.all, r.changedAll = true, true
	}
}

// pull takes the render state's dirty rows and marks it clean, so the next
// Update reports only what changes from here.
func (r *redrawSet) pull(state *gostty.RenderState) error {
	clear(r.changed)
	dirty, err := state.Dirty()
	if err != nil {
		return err
	}
	r.changedAll = dirty == gostty.RenderDirtyFull // colours or size changed
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
// frame, a narrower question than whether it has to be repainted.
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

// grow returns s resliced to n, reallocating only when it will not fit. The
// contents are not preserved; callers overwrite or clear them.
func grow[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}
