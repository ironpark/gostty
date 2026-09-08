package main

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

type terminalTab struct {
	owner   *terminalApp
	offsetY int

	// The terminal side.
	vt     *gostty.Terminal
	stream *gostty.Stream
	state  *gostty.RenderState
	cells  []gostty.RenderCell
	// Kitty graphics: the placements for this frame and their textures.
	images *imageCache
	// Search matches in the viewport, one bool per cell, refreshed with the
	// cells so the highlight never lags the text under it.
	matchCells []bool
	// Scratch for the viewport matches read each frame, kept so a frame with
	// highlights on screen does not allocate.
	viewportMatches []gostty.Selection

	// Partial redraw. The grid is drawn into two layers, backgrounds and
	// glyphs, so Kitty images can sit between them; a row is redrawn only
	// when it is in the redraw set.
	bgLayer, textLayer *ebiten.Image
	redraw             redrawSet

	// The process side.
	shell *shellSession

	// The pixel side.
	fonts      *fonts.Set
	emoji      *fonts.Emoji
	cols, rows int
	// The terminal's resolved defaults and the themed colors used to draw
	// them. Explicit ANSI colors continue to come from the terminal.
	terminalBg, terminalFg color.RGBA
	bg, fg                 color.RGBA
	cursor                 cursorState
	bell                   int
	drawOp                 text.DrawOptions

	// Program metadata for the tab label and the active window title.
	title, pwd, progress string

	// The clipboard. The system one when it is available; otherwise a
	// process-local buffer, which at least lets OSC 52 and paste agree.
	*clipboardState

	sel selection

	// The search bar and the settings panel, which take the keyboard while
	// they are open.
	panels   ui.Panels
	settings tabSettings
	search   *gostty.Search
	// Set when the faces changed, so Layout redoes the grid even if the window
	// did not move.
	relayout bool
	// The display's scale factor. Every pixel in this program is a device
	// pixel; this is what the window's own units are converted with.
	dsf float64

	// The cat that walks on this tab's output.
	cat *thecat.Companion

	// Mouse and focus reporting: what the program is told about the pointer,
	// as opposed to what the window does with it itself.
	mouseCol, mouseRow int
	wheel              float64
	mouseGrabbed       bool
	focused            bool
	focusedFrames      int

	// Per-frame input scratch, reused so a keystroke allocates nothing. The
	// reader answers for the shell; `chars` is the search bar's, which wants
	// the runes themselves rather than key events. `out` is what the keys
	// encode to, `report` what the mouse and focus do.
	keys        keys.Reader
	chars       []rune
	out, report frameBuffer
}

// cursorState is what Draw needs to paint the cursor, read once per frame.
type cursorState struct {
	x, y    uint16
	visible bool
	style   gostty.CursorStyle
}

// readOutput services background tabs too, with a per-frame budget so a busy
// shell cannot starve the other tabs or window input.
func (tab *terminalTab) readOutput() (bool, error) {
	fed := false
	for remaining := 64; remaining > 0; remaining-- {
		select {
		case chunk, ok := <-tab.shell.output:
			if !ok {
				return fed, ebiten.Termination // the shell exited
			}
			if err := tab.stream.Feed(chunk); err != nil {
				return fed, fmt.Errorf("feed: %w", err)
			}
			fed = true
		default:
			remaining = 0
		}
	}

	if fed {
		if err := tab.drainEvents(); err != nil {
			return fed, err
		}
		// The terminal answers some sequences itself -- device status, size
		// reports, Kitty graphics acknowledgements -- and a program that asked
		// is blocked until the answer arrives. Nothing is written from inside
		// the feed, so the replies are drained right after it.
		if err := tab.stream.WriteReplies(tab.shell.pty); err != nil {
			return fed, fmt.Errorf("reply: %w", err)
		}
	}
	return fed, nil
}

func (tab *terminalTab) updateInput() error {
	// Input before the refresh, so a selection made this frame is drawn this
	// frame rather than one behind. The modifier state is read once and shared:
	// the mouse needs Alt for block selection and the keyboard needs all four.
	m := keys.Current()
	if tab.focused {
		// A panel takes the keyboard while it is open, so nothing typed into the
		// search bar reaches the shell. The mouse is left alone: a selection is
		// still worth being able to make.
		taken, err := tab.handleUI(m)
		if err != nil {
			return err
		}
		// The pointer above the grid is the tab bar's; a drag released there
		// ends. A click on the cat is the cat's, not the shell's: it must not
		// also start a selection or be reported to the program.
		_, y := tab.cursorPosition()
		onGrid := y >= 0
		if !onGrid && !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
			tab.sel.dragging = false
		}
		if onGrid && !tab.pokeCat() {
			if err := tab.handleMouse(m); err != nil {
				return err
			}
		}
		if onGrid {
			if err := tab.handleWheel(m); err != nil {
				return err
			}
		}
		if !taken {
			if err := tab.handleInput(m); err != nil {
				return err
			}
		}
	}
	if err := tab.refresh(); err != nil {
		return err
	}
	// After the refresh: the cat walks on the cells this frame is about to
	// draw, not the ones the last frame drew.
	tab.updateCat()
	return nil
}
