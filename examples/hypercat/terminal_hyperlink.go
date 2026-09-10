package main

import (
	"image"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/internal/desktop"
	"github.com/ironpark/gostty/examples/hypercat/keys"
)

// Hyperlinks are OSC 8: a program marks a run of cells as a link and gives the
// URI once, rather than printing it and hoping the terminal guesses. The
// guessing kind -- a regular expression over the text -- is what an emulator
// does for the programs that never learned about OSC 8; this is the half the
// terminal actually knows about, so the URI comes from it rather than from a
// pattern.
//
// The link a cell belongs to is not in `RenderCell`: a URI is a string and the
// cells are a flat array of fixed-size structs. `RenderState.HyperlinkAt` is
// where it is asked for, one cell at a time, which is why only the row under
// the pointer is ever scanned.

// hoveredLink is the link under the pointer, as of the last refresh.
type hoveredLink struct {
	uri string
	row int
	// The half-open run of columns in that row the link covers, so the whole
	// link underlines rather than only the cell being pointed at.
	start, end int
}

func (l hoveredLink) valid() bool { return l.uri != "" && l.end > l.start }

// refreshLink finds the link under the pointer.
//
// Only the hovered cell is asked in the common case, and its row is walked out
// from there only when there is a link to walk. A link that soft-wraps is
// underlined a row at a time, since this scan stops at the row's edges; what
// opens is the whole URI either way, because that is what the terminal stored.
func (tab *terminal) refreshLink() error {
	px, py := tab.cursorPosition()
	pointer := image.Pt(px, py)
	if !tab.reports.focused {
		// Nothing is under a pointer this window is not being shown.
		pointer = image.Pt(-1, -1)
	}
	if err := tab.frame.readLink(tab.state, tab.grid(), pointer); err != nil {
		return err
	}

	return nil
}

// readLink finds the link under a pointer position, which is negative when the
// pointer is not over the grid at all.
//
// The cell under the pointer is asked every frame, since that is one call and
// the answer is what decides everything else. The run it belongs to is only
// walked when the answer is a link the last frame did not already have: a
// pointer resting on a seventy-cell link would otherwise cost seventy calls
// and seventy strings a frame to arrive at the same two numbers.
func (f *frame) readLink(state *gostty.RenderState, g grid, pointer image.Point) error {
	previous := f.link
	f.link = hoveredLink{}
	if pointer.X < 0 || pointer.Y < 0 || g.cols == 0 || g.rows == 0 {
		return nil
	}
	col, row := g.cellAt(pointer.X, pointer.Y)
	uri, ok, err := state.HyperlinkAt(uint16(col), uint16(row))
	if err != nil || !ok || uri == "" {
		return err
	}
	// The same link, in the same row, still covering this cell: the run cannot
	// have changed without the row being rewritten, and a rewritten row is one
	// the caller marked for redraw.
	if previous.uri == uri && previous.row == row &&
		col >= previous.start && col < previous.end && !f.redraw.rewritten(row) {
		f.link = previous
		return nil
	}

	link := hoveredLink{uri: uri, row: row, start: col, end: col + 1}
	for x := col - 1; x >= 0; x-- {
		if same, err := linkAt(state, x, row, uri); err != nil {
			return err
		} else if !same {
			break
		}
		link.start = x
	}
	for x := col + 1; x < g.cols; x++ {
		if same, err := linkAt(state, x, row, uri); err != nil {
			return err
		} else if !same {
			break
		}
		link.end = x + 1
	}
	f.link = link
	return nil
}

// linkAt reports whether a cell belongs to the same link. Two runs of the same
// URI printed side by side are one underline here, which is what they look
// like; telling them apart would mean the link id, which OSC 8 makes optional.
func linkAt(state *gostty.RenderState, col, row int, uri string) (bool, error) {
	at, ok, err := state.HyperlinkAt(uint16(col), uint16(row))
	return ok && at == uri, err
}

// openLink opens the hovered link, if the shortcut modifier is held. A plain
// click keeps starting a selection: a link under the pointer must not turn a
// click into navigating away.
func (tab *terminal) openLink(m keys.Mods) bool {
	if !m.Shortcut() || !tab.frame.link.valid() {
		return false
	}
	desktop.OpenURL(tab.frame.link.uri)
	return true
}
