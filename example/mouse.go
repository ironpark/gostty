package main

import (
	"context"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"golang.design/x/clipboard"
)

type selection struct {
	dragging bool
	ax, ay   int // the anchor, in cells

	// Ebitengine reports presses, not click counts, so they are counted here.
	clicks       int
	lastAt       time.Time
	lastX, lastY int
}

// How close together two presses have to be to count as a double click.
const multiClickInterval = 400 * time.Millisecond

// The codepoints that end a word for double-click selection.
//
// ghostty has no default for these on purpose: its own UI reads the set from
// configuration, so the choice belongs to whoever embeds it. This is that
// configuration for this program.
var wordBoundaries = []uint32{
	0, ' ', '\t', '\'', '"', '`', '|', ':', ';', ',',
	'(', ')', '[', ']', '{', '}', '<', '>', '$', '\u2502',
}

// handleMouse turns a drag into a selection on the screen.
//
// The selection itself is ghostty's: we hand it two viewport positions and it
// works out what that means for wrapped lines, wide characters and the
// scrollback. What comes back is the selected text and, through the render
// state, a per-cell flag to draw with.
func (g *game) handleMouse(m mods) error {
	pressed := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	switch {
	case inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft):
		col, row := g.cellAt(ebiten.CursorPosition())
		g.countClick(col, row)
		switch g.sel.clicks {
		case 1:
			g.sel.dragging = true
			g.sel.ax, g.sel.ay = col, row
			return g.clearSelection()
		default:
			return g.selectAt(col, row, g.sel.clicks)
		}

	case g.sel.dragging:
		g.sel.dragging = pressed
		col, row := g.cellAt(ebiten.CursorPosition())
		// A press and release on one cell is a click, not a one-cell selection.
		if col == g.sel.ax && row == g.sel.ay {
			return g.clearSelection()
		}
		screen, err := g.vt.ActiveScreen()
		if err != nil {
			return err
		}
		// Alt selects the block between the corners instead of the flow of text.
		_, err = screen.SelectRange(
			uint16(g.sel.ax), uint16(g.sel.ay),
			uint16(col), uint16(row),
			m.alt,
		)
		return err
	}
	return nil
}

// countClick turns a press into a click count: one starts a drag, two selects
// the word, three the line.
func (g *game) countClick(col, row int) {
	now := time.Now()
	repeat := now.Sub(g.sel.lastAt) < multiClickInterval && col == g.sel.lastX && row == g.sel.lastY
	if repeat && g.sel.clicks < 3 {
		g.sel.clicks++
	} else {
		g.sel.clicks = 1
	}
	g.sel.lastAt, g.sel.lastX, g.sel.lastY = now, col, row
}

// selectAt hands the position to ghostty, which owns what a word and a line
// are: soft wraps, whitespace trimming and semantic prompt boundaries are all
// its business, not this program's.
func (g *game) selectAt(col, row, clicks int) error {
	screen, err := g.vt.ActiveScreen()
	if err != nil {
		return err
	}
	if clicks == 2 {
		_, err = screen.SelectWord(uint16(col), uint16(row), wordBoundaries)
	} else {
		_, err = screen.SelectLine(uint16(col), uint16(row))
	}
	return err
}

// cellAt maps a pixel position to a cell, clamped to the viewport so a drag
// that runs off the window still selects to the edge.
func (g *game) cellAt(px, py int) (int, int) {
	col := min(max(int(float64(px)/g.fonts.cellW), 0), g.cols-1)
	row := min(max(int(float64(py)/g.fonts.cellH), 0), g.rows-1)
	return col, row
}

func (g *game) clearSelection() error {
	screen, err := g.vt.ActiveScreen()
	if err != nil {
		return err
	}
	return screen.ClearSelection()
}

// copySelection puts the selected text on the system clipboard, falling back to
// the process-local one when there is no system clipboard to write to.
func (g *game) copySelection() error {
	screen, err := g.vt.ActiveScreen()
	if err != nil {
		return err
	}
	text, ok, err := screen.SelectionString()
	if err != nil || !ok || len(text) == 0 {
		return err
	}
	// The clipboard keeps bytes: that is what the system clipboard and OSC 52
	// both deal in, and the selection is the only place a string arrives.
	g.clipboard = append(g.clipboard[:0], text...)
	if g.systemClipboard {
		if _, err := clipboard.Write(context.Background(), clipboard.FmtText, g.clipboard); err != nil {
			return err
		}
	}
	return nil
}

// pasteText reads the system clipboard if there is one, otherwise whatever the
// program last wrote through OSC 52.
func (g *game) pasteText() []byte {
	if g.systemClipboard {
		if text, err := clipboard.Read(context.Background(), clipboard.FmtText); err == nil && len(text) > 0 {
			return text
		}
	}
	return g.clipboard
}
