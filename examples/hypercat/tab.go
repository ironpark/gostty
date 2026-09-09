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
	// The cells whose text is more than one codepoint, keyed by cell index,
	// and the scratch the terminal writes them into. Kept apart from `cells`
	// because they are the rare case: reading them costs a call per cell, so
	// only the rows that changed are asked.
	clusters   map[int]string
	clusterBuf []rune
	// The scrollback search and the cells it highlights.
	search tabSearch
	// The OSC 8 link under the pointer, which is underlined and can be opened.
	link hoveredLink
	// Where the viewport sits in the scrollback, and how long the bar showing
	// it stays up.
	scrollbar scrollbarState

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
	// The lit half of the blink phase, which SGR 5 cells and the cursor are
	// drawn from. Held rather than read per cell so one frame is one phase.
	blink bool

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
//
// All of it comes from the render state, including the parts a naive renderer
// invents for itself: whether the cursor blinks (DECSCUSR's odd styles and
// mode 12), what colour it was given (OSC 12), whether it sits on the tail of
// a wide character, and whether the program has said it is reading a password.
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

// activate and deactivate are what a tab does about being switched to and away
// from. The window says which tab is the visible one; what that means to the
// pointer, the selection and the cat is the tab's own business, so a new piece
// of per-tab state is reset here rather than in the window's loop.
func (tab *terminalTab) activate() {
	tab.reports.focusedFrames = 0
	tab.redraw.markAll()
}

func (tab *terminalTab) deactivate() {
	tab.endGesture()
	tab.reports.mouseGrabbed = false
	tab.cat.ClearHover()
}

// themeChanged and fontsChanged are what a tab does about a settings change the
// window made for all of them.
func (tab *terminalTab) themeChanged() error {
	tab.redraw.markAll()
	if err := tab.applyPalette(); err != nil {
		return err
	}
	// A program that subscribed with mode 2031 is told now, not the next time
	// it thinks to ask. The scheme is resolved per tab, since a theme that
	// defers to the terminal takes the colors that tab's program set.
	return tab.stream.ColorSchemeChanged(tab.colorScheme())
}

// applyPalette hands the theme's sixteen ANSI colours to the terminal.
//
// A theme that only replaced the default foreground and background would leave
// everything a program coloured by name -- every ls, every prompt, every diff
// -- in the colours of whatever theme it was not using. The palette is where
// those live, and it belongs to the terminal: a program can set it too, with
// OSC 4, and it is resolved per cell as the screen is read.
//
// The theme sets the defaults rather than the current values, so what a
// program asked for is what a reset comes back to. `ResetPalette` then makes
// the new defaults the current colours, which does drop an OSC 4 palette a
// program set for itself -- the same thing every emulator does when its
// configuration is reloaded.
func (tab *terminalTab) applyPalette() error {
	palette := tab.currentTheme().Palette
	if palette == nil {
		// The terminal theme, which is the terminal's own colours: put back
		// whatever the defaults were before a theme was applied.
		if err := tab.vt.ResetDefaultPalette(); err != nil {
			return err
		}
		return tab.vt.ResetPalette()
	}
	for i, c := range palette {
		rgb := uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		if err := tab.vt.SetDefaultPaletteColor(uint8(i), rgb); err != nil {
			return err
		}
	}
	if err := tab.vt.ResetPalette(); err != nil {
		return err
	}
	return tab.restyle()
}

// restyle rebuilds the render state after a change the terminal does not mark
// any row dirty for.
//
// A cell keeps the palette entry it was written with and the colour is
// resolved as the state is read, so every cell on screen changes colour when
// the palette does -- but nothing about the screen changed, so an existing
// state reports the rows it has as clean and hands back the colours it
// resolved last time. A fresh one resolves them again.
func (tab *terminalTab) restyle() error {
	if tab.state == nil {
		return nil
	}
	state, err := gostty.NewRenderState()
	if err != nil {
		return err
	}
	_ = tab.state.Close()
	tab.state = state
	tab.redraw.markAll()
	return nil
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
			// A drop lands on the tab under the pointer, whichever half of the
			// conversation takes it.
			if err := tab.handleDrop(); err != nil {
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
