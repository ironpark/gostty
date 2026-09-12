package main

import (
	"fmt"
	"io"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/internal/appearance"
	"github.com/ironpark/gostty/examples/hypercat/internal/desktop"
	"github.com/ironpark/gostty/examples/hypercat/internal/frontend"
	"github.com/ironpark/gostty/examples/hypercat/internal/graphics"
	"github.com/ironpark/gostty/examples/hypercat/shell"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// terminal owns one terminal, shell, and viewport. Shared appearance and
// clipboard come from the window; UI actions flow back without a window reference.
type terminal struct {
	// gostty resources and the shell producing/consuming terminal bytes.
	vt     *gostty.Terminal
	stream *gostty.Stream
	state  *gostty.RenderState
	shell  *shell.Session

	// Draw consumes the snapshot and caches; it never reads native state.
	frame    frame
	renderer frontend.Renderer
	images   *graphics.Cache

	// Grid dimensions and position below the window's tab bar.
	cols, rows int
	offsetY    int
	relayout   bool // font changes require Layout even at the same window size

	// Per-tab interactions and optional features.
	sel     selection
	search  tabSearch
	panels  ui.Panels
	reports reportState
	title   windowTitle

	// Shared window resources.
	settings  *appearance.State
	clipboard *desktop.Clipboard

	// This update's host input and reusable terminal encoder buffers.
	input       frontend.Input
	out, report frameBuffer
}

// Bound scrollback per tab; native pruning happens at page boundaries.
const (
	scrollbackMaxLines = 10_000
	scrollbackMaxBytes = 16 << 20
)

// start acquires a tab's resources. Any failure releases what was created.
func (tab *terminal) start() (err error) {
	defer func() {
		if err != nil {
			tab.close()
		}
	}()
	if tab.vt, err = gostty.NewTerminalWithConfig(tab.terminalConfig()); err != nil {
		return err
	}
	// The stream must be closed before the terminal: its handler reaches
	// through the terminal for an allocator when it tears down.
	if tab.stream, err = tab.vt.NewStreamWithConfig(tab.streamConfig()); err != nil {
		return err
	}
	// OSC 72 registrations tell the host which dropped file types to offer.
	if err := tab.stream.OnDrag(tab.onDrag); err != nil {
		return err
	}
	if tab.state, err = gostty.NewRenderState(); err != nil {
		return err
	}
	if tab.images, err = graphics.NewCache(tab.vt); err != nil {
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
	if err != nil {
		return err
	}
	return nil
}

// readOutput feeds PTY output and returns terminal replies to the shell.
// The budget also lets background tabs and window input make progress.
func (tab *terminal) readOutput() (bool, error) {
	fed := false
	for remaining := 64; remaining > 0; remaining-- {
		select {
		case chunk, ok := <-tab.shell.Output:
			if !ok {
				return fed, io.EOF // the shell exited
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

// refreshSnapshot captures the terminal, then updates search, graphics, and overlays.
// Draw and the cat both consume this completed snapshot.
func (tab *terminal) refreshSnapshot() error {
	g := tab.grid()
	if err := tab.frame.read(tab.state, tab.vt, g, tab.currentTheme()); err != nil {
		return err
	}
	// Derived from the cells: none of these reads what another one writes, so
	// the order between them is free.
	if err := tab.tickSearch(); err != nil {
		return err
	}
	if err := tab.refreshMatches(); err != nil {
		return err
	}
	if err := tab.images.Refresh(); err != nil {
		return err
	}
	if err := tab.refreshLink(); err != nil {
		return err
	}
	if err := tab.refreshScrollbar(); err != nil {
		return err
	}
	tab.frame.tickBlink(time.Now(), g)
	return nil
}

// resize moves the emulated screen and the pty together. They have to agree:
// the program asks the pty how big it is and writes for the terminal.
func (tab *terminal) resize(cols, rows int) error {
	tab.cols, tab.rows = cols, rows
	g := tab.grid()
	// `ResizeCells` rather than `Resize`: a Kitty image sized in cells is
	// measured in pixels through the cell size, and the terminal stores the
	// pixel size of the whole grid, so it goes stale on every column change.
	if err := tab.vt.ResizeCells(uint16(cols), uint16(rows), uint32(g.cellW), uint32(g.cellH)); err != nil {
		return err
	}
	// The selection gesture measures the pointer in pixels, so it is told the
	// new geometry rather than left to work from a stale cell size.
	if err := tab.syncGestureGeometry(); err != nil {
		return err
	}
	// The pty carries the same size, which is where a program that has not
	// asked the terminal directly reads it from.
	return tab.shell.Pty.Resize(cols, rows)
}

func (tab *terminal) close() {
	tab.shell.Close()
	// Reverse construction order; the stream is a child of the terminal, and
	// closing the terminal first would be refused. The search is a child of a
	// screen, which is borrowed from the terminal, so it goes first of all.
	tab.closeSearch()
	if tab.sel.gesture != nil {
		_ = tab.sel.gesture.Close()
		tab.sel.gesture = nil
	}
	tab.renderer.Close()
	tab.images.Close()
	_ = tab.state.Close()
	_ = tab.stream.Close()
	_ = tab.vt.Close()
}

// terminalConfig sets dimensions, scrollback limits, and reset-persistent modes.
func (tab *terminal) terminalConfig() gostty.TerminalConfig {
	lines, bytes := uint(scrollbackMaxLines), uint(scrollbackMaxBytes)
	return gostty.TerminalConfig{
		Cols:               uint16(tab.cols),
		Rows:               uint16(tab.rows),
		ScrollbackMaxLines: &lines,
		ScrollbackMaxBytes: &bytes,
		ModeDefaults: []gostty.ModeDefault{
			// The renderer supports clusters. Keep mode 2027 enabled after reset.
			{Mode: gostty.ModeGraphemeCluster, Enabled: true},
		},
	}
}

// streamConfig configures parser limits, terminal identity, and OSC callbacks.
func (tab *terminal) streamConfig() gostty.StreamConfig {
	scheme := tab.colorScheme()
	return gostty.StreamConfig{
		// Log a bounded sample of unsupported sequences.
		UnknownMaxBytes: 256,
		Version:         &gostty.VersionReport{Name: reportName, Version: reportVersion},
		// Answer color-scheme queries; themeChanged also notifies subscribers.
		ColorScheme:    &scheme,
		ClipboardWrite: tab.writeClipboard,
		ClipboardRead:  tab.readClipboard,
	}
}

// onScreen borrows the currently active screen for one operation.
// Fetch it each time because programs can switch between primary and alternate screens.
func (tab *terminal) onScreen(f func(*gostty.Screen) error) error {
	screen, err := tab.vt.ActiveScreen()
	if err != nil {
		return err
	}
	return f(screen)
}

// fonts and emoji are the window's, shared with every other tab: what a tab
// draws with follows the settings panel wherever it was opened.
func (tab *terminal) fonts() *fonts.Set   { return tab.settings.Fonts() }
func (tab *terminal) emoji() *fonts.Emoji { return tab.settings.Emoji() }

// Tab switches reset interaction state and invalidate the visible grid.
func (tab *terminal) activate() {
	tab.reports.focusedFrames = 0
	tab.frame.redraw.MarkAll()
}

func (tab *terminal) deactivate() {
	tab.endGesture()
	tab.reports.mouseGrabbed = false
}
