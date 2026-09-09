package main

import (
	"image/color"

	"github.com/ironpark/gostty"
)

// frame is what the last refresh read out of the terminal, and everything the
// window drew from it.
//
// Drawing cannot report a failure -- Ebitengine's Draw returns nothing -- and
// every call into the terminal can fail, so the two are kept apart: the read
// happens in Update, where an error can be returned, and Draw works from what
// it left here. That rule used to be a comment; this is the same rule as a
// type, so a drawing routine reaching for the terminal has to go looking for
// it rather than find it on hand.
//
// It is a snapshot and not a cache: everything in it is replaced or refreshed
// each frame, and nothing in it outlives the terminal it came from.
type frame struct {
	// The viewport, row-major, and the cells whose text is more than one
	// codepoint, keyed by cell index. The clusters are kept apart because they
	// are the rare case: reading them costs a call per cell, so only the rows
	// that changed are asked.
	cells      []gostty.RenderCell
	clusters   map[int]string
	clusterBuf []rune

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
