package main

import (
	"context"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty/examples/hypercat/keys"
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
var wordBoundaries = []rune{
	0, ' ', '\t', '\'', '"', '`', '|', ':', ';', ',',
	'(', ')', '[', ']', '{', '}', '<', '>', '$', '\u2502',
}

// handleMouse gives the mouse to the running program if it has asked for it,
// and otherwise turns a drag into a selection on the screen.
//
// The selection itself is ghostty's: we hand it two viewport positions and it
// works out what that means for wrapped lines, wide characters and the
// scrollback. What comes back is the selected text and, through the render
// state, a per-cell flag to draw with.
func (tab *terminalTab) handleMouse(m keys.Mods) error {
	// The program gets first refusal. When it has asked for the mouse, the
	// pointer is its input device and not this window's selection tool.
	if reported, err := tab.reportMouse(m); err != nil || reported {
		return err
	}

	pressed := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	switch {
	case inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft):
		col, row := tab.cellAt(tab.cursorPosition())
		tab.countClick(col, row)
		switch tab.sel.clicks {
		case 1:
			tab.sel.dragging = true
			tab.sel.ax, tab.sel.ay = col, row
			return tab.clearSelection()
		default:
			return tab.selectAt(col, row, tab.sel.clicks)
		}

	case tab.sel.dragging:
		tab.sel.dragging = pressed
		col, row := tab.cellAt(tab.cursorPosition())
		// A press and release on one cell is a click, not a one-cell selection.
		if col == tab.sel.ax && row == tab.sel.ay {
			return tab.clearSelection()
		}
		screen, err := tab.vt.ActiveScreen()
		if err != nil {
			return err
		}
		// Alt selects the block between the corners instead of the flow of text.
		_, err = screen.SelectRange(
			uint16(tab.sel.ax), uint16(tab.sel.ay),
			uint16(col), uint16(row),
			m.Alt,
		)
		return err
	}
	return nil
}

// countClick turns a press into a click count: one starts a drag, two selects
// the word, three the line.
func (tab *terminalTab) countClick(col, row int) {
	now := time.Now()
	repeat := now.Sub(tab.sel.lastAt) < multiClickInterval && col == tab.sel.lastX && row == tab.sel.lastY
	if repeat && tab.sel.clicks < 3 {
		tab.sel.clicks++
	} else {
		tab.sel.clicks = 1
	}
	tab.sel.lastAt, tab.sel.lastX, tab.sel.lastY = now, col, row
}

// selectAt hands the position to ghostty, which owns what a word and a line
// are: soft wraps, whitespace trimming and semantic prompt boundaries are all
// its business, not this program's.
func (tab *terminalTab) selectAt(col, row, clicks int) error {
	screen, err := tab.vt.ActiveScreen()
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
func (tab *terminalTab) cellAt(px, py int) (int, int) {
	col := min(max(int(float64(px)/tab.fonts.CellWidth), 0), tab.cols-1)
	row := min(max(int(float64(py)/tab.fonts.CellHeight), 0), tab.rows-1)
	return col, row
}

func (tab *terminalTab) clearSelection() error {
	screen, err := tab.vt.ActiveScreen()
	if err != nil {
		return err
	}
	return screen.ClearSelection()
}

// copySelection puts the selected text on the system clipboard, falling back to
// the process-local one when there is no system clipboard to write to.
func (tab *terminalTab) copySelection() error {
	screen, err := tab.vt.ActiveScreen()
	if err != nil {
		return err
	}
	text, ok, err := screen.SelectionString()
	if err != nil || !ok || len(text) == 0 {
		return err
	}
	// The clipboard keeps bytes: that is what the system clipboard and OSC 52
	// both deal in, and the selection is the only place a string arrives.
	tab.clipboard = append(tab.clipboard[:0], text...)
	if tab.systemClipboard {
		if _, err := clipboard.Write(context.Background(), clipboard.FmtText, tab.clipboard); err != nil {
			return err
		}
	}
	return nil
}

// pasteText reads the system clipboard if there is one, otherwise whatever the
// program last wrote through OSC 52.
func (tab *terminalTab) pasteText() []byte {
	if tab.systemClipboard {
		if text, err := clipboard.Read(context.Background(), clipboard.FmtText); err == nil && len(text) > 0 {
			return text
		}
	}
	return tab.clipboard
}
