package main

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/shell"
)

// Bound scrollback per tab; native pruning happens at page boundaries.
const (
	scrollbackMaxLines = 10_000
	scrollbackMaxBytes = 16 << 20
)

// start acquires a tab's resources. Any failure releases what was created.
func (tab *terminalTab) start() (err error) {
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
	if tab.images, err = newImageCache(tab.vt); err != nil {
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
	tab.startCat()
	return nil
}

// readOutput feeds PTY output and returns terminal replies to the shell.
// The budget also lets background tabs and window input make progress.
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

// terminalConfig sets dimensions, scrollback limits, and reset-persistent modes.
func (tab *terminalTab) terminalConfig() gostty.TerminalConfig {
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
func (tab *terminalTab) streamConfig() gostty.StreamConfig {
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

func (tab *terminalTab) close() {
	tab.shell.Close()
	// Reverse construction order; the stream is a child of the terminal, and
	// closing the terminal first would be refused. The search is a child of a
	// screen, which is borrowed from the terminal, so it goes first of all.
	tab.closeSearch()
	if tab.sel.gesture != nil {
		_ = tab.sel.gesture.Close()
		tab.sel.gesture = nil
	}
	tab.layers.close()
	tab.images.close()
	_ = tab.state.Close()
	_ = tab.stream.Close()
	_ = tab.vt.Close()
}
