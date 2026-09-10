package main

import (
	"fmt"
	"image/color"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/internal/frontend"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// frame holds the data Draw needs. Read gostty during Update, where errors
// can be returned; drawing uses the saved cells, clusters, colors, and cursor.
type frame struct {
	// The viewport, row-major, and the text of the cells that hold more than
	// one codepoint, in the same order. The clusters are kept apart from the
	// cells because they are the rare case -- almost every entry is empty,
	// meaning "the codepoint in the cell is the whole of it" -- and because
	// reading one costs a call into the terminal.
	cells    []gostty.RenderCell
	clusters []string

	// The cursor, and the colours the grid is drawn in.
	cursor cursorState
	colors frameColors

	// The OSC 8 link under the pointer, which is underlined and can be opened.
	link hoveredLink
	// Where the viewport sits in the scrollback, and how long the bar showing
	// it stays up.
	scrollbar scrollbarState
	// The lit half of the blink phase, which SGR 5 cells and the cursor are
	// drawn from. Held rather than read per cell so one frame is one phase.
	blink bool

	// The rows the next Draw has to repaint.
	redraw redrawSet
}

// frameColors is what the grid's default colours resolve to.
//
// Two pairs, because a theme is applied on this side: the terminal's own
// defaults are what a program means by "the default colour", and the themed
// pair is what that is drawn as. Explicit colours are resolved per cell
// against the terminal's pair, so both are kept.
type frameColors struct {
	terminalBg, terminalFg color.RGBA
	bg, fg                 color.RGBA
}

// cursorState is the terminal-provided cursor appearance for this frame.
type cursorState struct {
	x, y    uint16
	visible bool
	style   gostty.CursorStyle
	// Blinking is the terminal's answer, not a fixed policy: a program that
	// asks for a steady cursor gets one.
	blinking bool
	// wideTail is set when the cursor is on the second half of a wide
	// character, which is drawn two cells wide so it covers the whole glyph.
	wideTail bool
	// password is set while the program is reading a secret (OSC 133 / mode
	// 2026 password input). The block cursor then draws without the character
	// under it, which would otherwise be shown in the background colour.
	password bool
	// color is the cursor colour the program set, when it set one.
	color    color.RGBA
	hasColor bool
}

// read captures the terminal snapshot: cells, dirty rows, colors, cursor, and text.
// Viewport decorations can then mark extra rows without re-reading their clusters.
func (f *frame) read(state *gostty.RenderState, term *gostty.Terminal, g grid, theme ui.Theme) error {
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
	if err := f.redraw.pull(state); err != nil {
		return err
	}
	if err := f.readColors(state, theme); err != nil {
		return err
	}
	if err := f.readCursor(state); err != nil {
		return err
	}
	return f.readClusters(state, g)
}

// tickBlink repaints blinking cells when the host clock changes phase.
func (f *frame) tickBlink(now time.Time, g grid) {
	lit := frontend.BlinkLit(now)
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

// readClusters fetches the full text behind each cell’s base codepoint.
// Only terminal-rewritten rows need these per-cell calls; theme, selection,
// and blink changes can reuse the previous text.
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

// readColors resolves terminal defaults through the theme and invalidates
// the grid when its displayed default colors change.
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
		f.redraw.MarkAll()
	}
	return nil
}

// readCursor captures cursor state before Draw.
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

func (r *redrawSet) MarkAll() { r.all = true }

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
func (r *redrawSet) Take(row int) bool {
	if row < 0 || row >= len(r.rows) || !r.rows[row] {
		return false
	}
	r.rows[row] = false
	return true
}

// clear is called once the grid has been repainted whole.
func (r *redrawSet) Clear() {
	r.all = false
	clear(r.rows)
}

// presentation borrows the slices collected by refreshSnapshot. Native read
// bookkeeping remains in frame; frontend receives only what it needs to draw.
func (tab *terminal) presentation() frontend.Frame {
	f := &tab.frame
	return frontend.Frame{
		Cols: tab.cols, Rows: tab.rows, Cells: f.cells, Clusters: f.clusters,
		Matches: tab.search.cells, Blink: f.blink, Damage: &f.redraw,
		Colors: frontend.Colors{
			TerminalBg: f.colors.terminalBg, TerminalFg: f.colors.terminalFg,
			Bg: f.colors.bg, Fg: f.colors.fg,
		},
		Cursor: frontend.Cursor{
			X: f.cursor.x, Y: f.cursor.y, Visible: f.cursor.visible,
			Blinking: f.cursor.blinking, WideTail: f.cursor.wideTail,
			Password: f.cursor.password, Style: f.cursor.style,
			Color: f.cursor.color, HasColor: f.cursor.hasColor,
		},
		Link:      frontend.Link{URI: f.link.uri, Row: f.link.row, Start: f.link.start, End: f.link.end},
		Scrollbar: frontend.Scrollbar{Bar: f.scrollbar.bar, Visible: f.scrollbar.visible},
		Fonts:     tab.fonts(), Emoji: tab.emoji(), Theme: tab.currentTheme(), Scale: tab.settings.Scale(),
		Images: tab.images, Cat: tab.cat,
	}
}

func (r *redrawSet) Full() bool { return r.all }
