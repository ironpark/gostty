package main

import (
	"context"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty"
	"golang.design/x/clipboard"
)

type selection struct {
	dragging bool
	ax, ay   int // the anchor, in cells
	active   bool
}

// handleMouse turns a drag into a selection on the screen.
//
// The selection itself is ghostty's: we hand it two viewport positions and it
// works out what that means for wrapped lines, wide characters and the
// scrollback. What comes back is the selected text and, through the render
// state, a per-cell flag to draw with.
func (g *game) handleMouse() error {
	screen, err := g.vt.ActiveScreen()
	if err != nil {
		return err
	}
	col, row := g.cellAt(ebiten.CursorPosition())

	switch {
	case inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft):
		g.sel.dragging = true
		g.sel.ax, g.sel.ay = col, row
		return g.clearSelection(screen)

	case g.sel.dragging && ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft):
		return g.selectTo(screen, col, row)

	case g.sel.dragging: // released
		g.sel.dragging = false
		return g.selectTo(screen, col, row)
	}
	return nil
}

// cellAt maps a pixel position to a cell, clamped to the viewport so a drag
// that runs off the window still selects to the edge.
func (g *game) cellAt(px, py int) (int, int) {
	col := min(max(int(float64(px)/g.fonts.cellW), 0), g.cols-1)
	row := min(max(int(float64(py)/g.fonts.cellH), 0), g.rows-1)
	return col, row
}

func (g *game) clearSelection(screen *gostty.Screen) error {
	if !g.sel.active {
		return nil
	}
	g.sel.active = false
	return screen.ClearSelection()
}

func (g *game) selectTo(screen *gostty.Screen, col, row int) error {
	// A press and release on one cell is a click, not a one-cell selection.
	if col == g.sel.ax && row == g.sel.ay {
		return g.clearSelection(screen)
	}
	// Alt selects the block between the corners instead of the flow of text.
	rectangle := ebiten.IsKeyPressed(ebiten.KeyAlt)
	ok, err := screen.SelectRange(
		uint16(g.sel.ax), uint16(g.sel.ay),
		uint16(col), uint16(row),
		rectangle,
	)
	if err != nil {
		return err
	}
	g.sel.active = ok
	return nil
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
	g.clipboard = append(g.clipboard[:0], text...)
	if g.systemClipboard {
		if _, err := clipboard.Write(context.Background(), clipboard.FmtText, text); err != nil {
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
