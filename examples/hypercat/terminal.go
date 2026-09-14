package main

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/shell"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// terminal is one tab: a gostty terminal, the shell feeding it, and the
// state read out of it for drawing. Fonts, theme and clipboard are the
// window's, shared by pointer.
type terminal struct {
	// core owns vt, stream and the gesture behind one Close. The aliases keep
	// the hot paths readable; they own nothing.
	core   *gostty.Session
	vt     *gostty.Terminal
	stream *gostty.Stream
	state  *gostty.RenderState
	shell  *shell.Session

	// What Draw paints. Read from the terminal during update, where errors
	// can be returned; Draw only touches these and the GPU.
	frame  frame
	layers gridCanvas
	bell   int // frames of visual bell left to draw
	images *kittyCache

	// Grid size in cells, and where the grid sits below the tab bar.
	cols, rows int
	offsetY    int
	relayout   bool // a font change needs Layout even at the same window size

	sel     selection
	search  tabSearch
	panels  ui.Panels
	reports reportState
	title   windowTitle

	settings  *settings
	clipboard *clipboard

	// This update's host input, and reusable buffers for the encoders.
	input       hostInput
	out, report frameBuffer
}

// The scrollback bound per tab; native pruning happens at page boundaries.
const (
	scrollbackMaxLines = 10_000
	scrollbackMaxBytes = 16 << 20
)

// Lifecycle ------------------------------------------------------------------

// start acquires a tab's resources. Any failure releases what was created.
func (tab *terminal) start() (err error) {
	defer func() {
		if err != nil {
			tab.close()
		}
	}()
	if tab.core, err = gostty.New(
		uint16(tab.cols), uint16(tab.rows),
		gostty.WithTerminalConfig(tab.terminalConfig()),
		gostty.WithStreamConfig(tab.streamConfig()),
	); err != nil {
		return err
	}
	tab.vt, tab.stream = tab.core.Terminal(), tab.core.Stream()
	// OSC 72 registrations tell the host which dropped file types to offer.
	if err := tab.stream.OnDrag(tab.onDrag); err != nil {
		return err
	}
	if tab.state, err = gostty.NewRenderState(); err != nil {
		return err
	}
	if tab.images, err = newKittyCache(tab.vt); err != nil {
		return err
	}
	if err := tab.startGesture(); err != nil {
		return err
	}
	// A tab opens in the window's theme, palette included.
	if err := tab.applyPalette(); err != nil {
		return err
	}
	tab.shell, err = shell.Start(tab.cols, tab.rows)
	return err
}

// terminalConfig sets the size, scrollback limits, and the modes that
// survive a reset.
func (tab *terminal) terminalConfig() gostty.TerminalConfig {
	lines, bytes := uint(scrollbackMaxLines), uint(scrollbackMaxBytes)
	return gostty.TerminalConfig{
		Cols: uint16(tab.cols), Rows: uint16(tab.rows),
		ScrollbackMaxLines: &lines, ScrollbackMaxBytes: &bytes,
		// The renderer draws clusters, so mode 2027 stays on after a reset.
		ModeDefaults: []gostty.ModeDefault{{Mode: gostty.ModeGraphemeCluster, Enabled: true}},
	}
}

// streamConfig sets parser limits, what the terminal answers to XTVERSION and
// colour-scheme queries, and who answers OSC 52.
func (tab *terminal) streamConfig() gostty.StreamConfig {
	scheme := tab.colorScheme()
	return gostty.StreamConfig{
		UnknownMaxBytes: 256, // log a bounded sample of unsupported sequences
		Version:         &gostty.VersionReport{Name: reportName, Version: reportVersion},
		ColorScheme:     &scheme,
		ClipboardWrite:  tab.writeClipboard,
		ClipboardRead:   tab.readClipboard,
	}
}

// readOutput feeds what the shell wrote, acts on the events that produced,
// and returns the terminal's replies to the shell. The budget lets background
// tabs and window input make progress while a program floods the pty.
func (tab *terminal) readOutput() (bool, error) {
	fed := false
	for remaining := 64; remaining > 0; remaining-- {
		select {
		case chunk, ok := <-tab.shell.Output:
			if !ok {
				return fed, io.EOF // the shell exited
			}
			if _, err := tab.stream.Write(chunk); err != nil {
				return fed, fmt.Errorf("feed: %w", err)
			}
			fed = true
		default:
			remaining = 0
		}
	}
	if !fed {
		return false, nil
	}
	if err := tab.drainEvents(); err != nil {
		return true, err
	}
	// The terminal answers some sequences itself -- device status, size
	// reports, Kitty acknowledgements -- and the program that asked is
	// blocked until the answer arrives. Nothing is written from inside Feed,
	// so the replies are drained right after it.
	if err := tab.stream.WriteReplies(tab.shell.Pty); err != nil {
		return true, fmt.Errorf("reply: %w", err)
	}
	return true, nil
}

// refreshSnapshot reads the terminal into the frame, then everything derived
// from the cells: search highlights, images, the hovered link, the scrollbar.
func (tab *terminal) refreshSnapshot() error {
	g := tab.grid()
	if err := tab.frame.read(tab.state, tab.vt, g, tab.currentTheme()); err != nil {
		return err
	}
	for _, step := range []func() error{
		tab.tickSearch, tab.refreshMatches, tab.images.refresh, tab.refreshLink, tab.refreshScrollbar,
	} {
		if err := step(); err != nil {
			return err
		}
	}
	tab.frame.tickBlink(time.Now(), g)
	return nil
}

// resize moves the emulated screen and the pty together. They have to agree:
// the program asks the pty how big it is and writes for the terminal.
func (tab *terminal) resize(cols, rows int) error {
	tab.cols, tab.rows = cols, rows
	g := tab.grid()
	// ResizeCells rather than Resize: a Kitty image sized in cells is
	// measured in pixels through the cell size, so the terminal has to know
	// it.
	if err := tab.vt.ResizeCells(uint16(cols), uint16(rows), uint32(g.cellW), uint32(g.cellH)); err != nil {
		return err
	}
	// The selection gesture measures the pointer in pixels, so it is told too.
	if err := tab.syncGestureGeometry(); err != nil {
		return err
	}
	return tab.shell.Pty.Resize(cols, rows)
}

// layout fits the grid to a content area and resizes when the cell count or
// the cell size changed.
func (tab *terminal) layout(width, height float64) {
	g := tab.grid()
	cols, rows := max(int(width/g.cellW), 1), max(int(height/g.cellH), 1)
	if cols == tab.cols && rows == tab.rows && !tab.relayout {
		return
	}
	if err := tab.resize(cols, rows); err != nil {
		log.Printf("resize to %dx%d: %v", cols, rows, err)
		return
	}
	tab.relayout = false
}

func (tab *terminal) close() {
	tab.shell.Close()
	// Reverse construction order. The search is a child of a screen borrowed
	// from the terminal, so it goes first; the stream is a child of the
	// terminal, and closing the terminal first would be refused.
	tab.closeSearch()
	tab.layers.close()
	tab.images.close()
	_ = tab.state.Close()
	if tab.core != nil {
		_ = tab.core.Close()
	} else {
		// A tab assembled only part-way, including tests that inject the
		// low-level handles directly.
		_ = tab.sel.gesture.Close()
		_ = tab.stream.Close()
		_ = tab.vt.Close()
	}
	tab.sel.gesture = nil
}

// onScreen borrows the active screen for one operation. It is fetched each
// time because programs switch between the primary and alternate screens.
func (tab *terminal) onScreen(f func(*gostty.Screen) error) error {
	screen, err := tab.vt.ActiveScreen()
	if err != nil {
		return err
	}
	return f(screen)
}

// Tab switches reset interaction state and repaint the whole grid.
func (tab *terminal) activate() {
	tab.reports.focusedFrames = 0
	tab.frame.redraw.markAll()
}

func (tab *terminal) deactivate() {
	tab.endGesture()
	tab.reports.mouseGrabbed = false
}

// Events -----------------------------------------------------------------------

// drainEvents acts on what the program asked of the emulator rather than of
// the screen. libghostty-vt parses the OSC; doing something about it is ours.
func (tab *terminal) drainEvents() error {
	for event, err := range tab.stream.EventValues() {
		if err != nil {
			return err
		}
		switch event.Kind {
		case gostty.StreamEventBell:
			tab.bell = 6 // frames of visual bell
		case gostty.StreamEventTitleChanged:
			tab.title.program = event.Title
		case gostty.StreamEventPwdChanged:
			tab.title.pwd = event.Pwd
		case gostty.StreamEventProgressReport:
			switch {
			case event.ProgressState == gostty.ProgressStateRemove:
				tab.title.progress = ""
			case event.HasProgress:
				tab.title.progress = fmt.Sprintf("%d%%", event.Progress)
			default:
				tab.title.progress = event.ProgressState.String()
			}
		case gostty.StreamEventDesktopNotification:
			log.Printf("notification: %s %s", event.Title, event.Body)
		case gostty.StreamEventUnknownSequence:
			log.Printf("unhandled APC: %q", event.Sequence)
		}
	}
	return nil
}

// windowTitle is what a program has said about itself. The parts are kept
// apart because they arrive separately: a program that sets a title should
// not lose the directory a previous OSC 7 reported.
type windowTitle struct {
	program  string // OSC 0/2, also the tab's label
	pwd      string // OSC 7
	progress string // OSC 9;4
}

func (t windowTitle) String() string {
	title := appName
	if t.program != "" {
		title += " - " + t.program
	}
	if t.pwd != "" {
		title += " (" + t.pwd + ")"
	}
	if t.progress != "" {
		title += " [" + t.progress + "]"
	}
	return title
}

// Theme ------------------------------------------------------------------------

func (tab *terminal) currentTheme() ui.Theme { return ui.ThemeAt(tab.settings.theme) }

func (tab *terminal) colorScheme() gostty.ColorScheme {
	if tab.currentTheme().Light(tab.frame.colors.terminalBg) {
		return gostty.ColorSchemeLight
	}
	return gostty.ColorSchemeDark
}

// themeChanged and fontsChanged are what a tab does about a settings change
// the window made for all of them.
func (tab *terminal) themeChanged() error {
	tab.frame.redraw.markAll()
	if err := tab.applyPalette(); err != nil {
		return err
	}
	// A program that subscribed with mode 2031 is told now. The scheme is
	// resolved per tab, since the terminal theme takes that tab's colours.
	return tab.stream.ColorSchemeChanged(tab.colorScheme())
}

func (tab *terminal) fontsChanged() {
	tab.frame.redraw.markAll()
	// The cell changed shape, so the window holds a different number of
	// them; layout works that out, this only says its cached answer is stale.
	tab.relayout = true
}

// applyPalette hands the theme's sixteen ANSI colours to the terminal.
//
// Replacing only the default foreground and background would leave everything
// a program coloured by name -- every ls, every prompt -- in the old colours.
// The palette belongs to the terminal: a program can set it too (OSC 4), and
// it is resolved per cell as the screen is read. The theme sets the defaults
// so a reset comes back to them, then ResetPalette makes them current.
func (tab *terminal) applyPalette() error {
	palette := tab.currentTheme().Palette
	if palette == nil {
		// The terminal theme: put back the terminal's own colours.
		if err := tab.vt.ResetDefaultPalette(); err != nil {
			return err
		}
		return tab.vt.ResetPalette()
	}
	for i, c := range palette {
		if err := tab.vt.SetDefaultPaletteColor(uint8(i), gostty.RGBFromUint32(ui.Packed(c))); err != nil {
			return err
		}
	}
	if err := tab.vt.ResetPalette(); err != nil {
		return err
	}
	// A cell keeps the palette index it was written with and the colour is
	// resolved as the state is read, but nothing on screen changed, so an
	// existing render state would report every row clean and hand back the
	// colours it resolved last time. A fresh one resolves them again.
	if tab.state == nil {
		return nil
	}
	state, err := gostty.NewRenderState()
	if err != nil {
		return err
	}
	_ = tab.state.Close()
	tab.state = state
	tab.frame.redraw.markAll()
	return nil
}
