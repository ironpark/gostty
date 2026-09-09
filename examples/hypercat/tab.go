package main

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/shell"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// terminalTab is one shell in one grid: everything the window has more than one
// of. It holds no reference back to the window -- what a tab cannot decide
// alone, such as a settings step, it reports and the window applies -- so the
// dependencies run one way, from `terminalApp` down.
type terminalTab struct {
	offsetY int

	// The terminal side.
	vt     *gostty.Terminal
	stream *gostty.Stream
	state  *gostty.RenderState
	cells  []gostty.RenderCell
	// Kitty graphics: the placements for this frame and their textures.
	images *imageCache
	// The scrollback search and the cells it highlights.
	search tabSearch

	// Partial redraw: the layers the grid is drawn into, and the rows that
	// have to be drawn again on them.
	grid   gridCanvas
	redraw redrawSet

	// The process side.
	shell *shell.Session

	// The pixel side.
	cols, rows int
	// The terminal's resolved defaults and the themed colors used to draw
	// them. Explicit ANSI colors continue to come from the terminal.
	terminalBg, terminalFg color.RGBA
	bg, fg                 color.RGBA
	cursor                 cursorState
	bell                   int

	// What the program says it is, for the tab label and the window title.
	title windowTitle

	// The window's clipboard, shared with every other tab.
	clipboard *sharedClipboard

	sel selection

	// The search bar and the settings panel, which take the keyboard while
	// they are open.
	panels ui.Panels
	// The window's fonts, theme, and cat mode, shared with every other tab so
	// that a change made in the settings panel is a change to all of them.
	settings *appearance
	// Set when the faces changed, so Layout redoes the grid even if the window
	// did not move.
	relayout bool

	// The cat that walks on this tab's output.
	cat *thecat.Companion

	// Mouse and focus reporting: what the program is told about the pointer,
	// as opposed to what the window does with it itself.
	reports reportState

	// Per-frame input scratch, reused so a keystroke allocates nothing. The
	// reader answers for the shell; `chars` is the search bar's, which wants
	// the runes themselves rather than key events. `out` is what the keys
	// encode to, `report` what the mouse and focus do.
	keys        keys.Reader
	chars       []rune
	out, report frameBuffer
}

// fonts and emoji are the window's, shared with every other tab: what a tab
// draws with follows the settings panel wherever it was opened.
func (tab *terminalTab) fonts() *fonts.Set   { return tab.settings.fonts }
func (tab *terminalTab) emoji() *fonts.Emoji { return tab.settings.emoji }

// cursorState is what Draw needs to paint the cursor, read once per frame.
type cursorState struct {
	x, y    uint16
	visible bool
	style   gostty.CursorStyle
}

// activate and deactivate are what a tab does about being switched to and away
// from. The window says which tab is the visible one; what that means to the
// pointer, the selection and the cat is the tab's own business, so a new piece
// of per-tab state is reset here rather than in the window's loop.
func (tab *terminalTab) activate() {
	tab.reports.focusedFrames = 0
	tab.redraw.markAll()
}

func (tab *terminalTab) deactivate() {
	tab.sel.dragging = false
	tab.reports.mouseGrabbed = false
	tab.cat.ClearHover()
}

// themeChanged and fontsChanged are what a tab does about a settings change the
// window made for all of them.
func (tab *terminalTab) themeChanged() error {
	tab.redraw.markAll()
	// A program that subscribed with mode 2031 is told now, not the next time
	// it thinks to ask. The scheme is resolved per tab, since a theme that
	// defers to the terminal takes the colors that tab's program set.
	return tab.stream.ColorSchemeChanged(tab.colorScheme())
}

func (tab *terminalTab) fontsChanged() {
	tab.redraw.markAll()
	// The grid is measured in cells and the cell just changed shape, so the
	// window holds a different number of them. Layout is where that is worked
	// out; this only has to say that the answer it cached is stale, because the
	// column count can survive a size change while the pixel geometry the image
	// protocol measures in does not.
	tab.relayout = true
}

// readOutput services background tabs too, with a per-frame budget so a busy
// shell cannot starve the other tabs or window input.
func (tab *terminalTab) readOutput() (bool, error) {
	fed := false
	for remaining := 64; remaining > 0; remaining-- {
		select {
		case chunk, ok := <-tab.shell.Output:
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
		if err := tab.stream.WriteReplies(tab.shell.Pty); err != nil {
			return fed, fmt.Errorf("reply: %w", err)
		}
	}
	return fed, nil
}

// updateInput gives the tab this frame's pointer and keyboard, then brings what
// it draws up to date.
//
// The panels have already had their turn, because what they change is the
// window's: `panelTook` says an open one kept the keyboard, so nothing typed
// into the search bar reaches the shell. The mouse is left alone either way --
// a selection is still worth being able to make.
func (tab *terminalTab) updateInput(m keys.Mods, panelTook bool) error {
	if tab.reports.focused {
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
		if !panelTook {
			if err := tab.handleInput(m); err != nil {
				return err
			}
		}
	}
	// Input before the refresh, so a selection made this frame is drawn this
	// frame rather than one behind.
	if err := tab.refresh(); err != nil {
		return err
	}
	// After the refresh: the cat walks on the cells this frame is about to
	// draw, not the ones the last frame drew.
	tab.updateCat()
	return nil
}
