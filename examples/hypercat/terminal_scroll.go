package main

import (
	"bufio"
	"io"
	"log"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/internal/desktop"
)

// viewportTop is the scrollback row the top of the screen is showing, which is
// what turns a position in the scrollback into a row on the grid.
func (tab *terminal) viewportTop() (uint32, error) {
	var top uint32
	err := tab.onScreen(func(screen *gostty.Screen) error {
		var err error
		top, err = screen.ViewportTop()
		return err
	})
	return top, err
}

// revealRow scrolls only when the requested scrollback row is outside the viewport.
func (tab *terminal) revealRow(row uint32) error {
	top, err := tab.viewportTop()
	if err != nil {
		return err
	}
	if row >= top && row < top+uint32(tab.rows) {
		return nil
	}
	return tab.vt.ScrollViewport(gostty.ScrollViewportRow(uint(row)))
}

// The scrollbar is `Screen.Scrollbar`: the terminal counts what is in the
// scrollback and where the viewport sits in it, in rows, and hands over the
// three numbers a thumb is drawn from. Counting them here would mean tracking
// every scroll, every resize and every line that aged out of the scrollback --
// which the terminal is already doing.

// How long the bar stays visible after the last scroll.
const scrollbarFrames = 90

// scrollbarState is the last reading, and how long it stays on screen. It is
// shown while the viewport is away from the bottom and fades out shortly after
// it comes back, the way an overlay scrollbar does: a terminal sitting at its
// prompt should not have a bar down the side of it.
type scrollbarState struct {
	bar     gostty.Scrollbar
	visible int
}

// refreshScrollbar reads where the viewport sits in the scrollback.
func (tab *terminal) refreshScrollbar() error {
	return tab.onScreen(func(screen *gostty.Screen) error {
		bar, err := screen.Scrollbar()
		if err != nil {
			return err
		}
		tab.frame.updateScrollbar(bar)
		return nil
	})
}

// updateScrollbar takes a reading and decides how long the bar stays up.
func (f *frame) updateScrollbar(bar gostty.Scrollbar) {
	previous := f.scrollbar.bar
	f.scrollbar.bar = bar
	// Away from the bottom, or moved since the last frame: either is a reason
	// to be showing it.
	awayFromBottom := bar.Offset+bar.Len < bar.Total
	moved := bar.Offset != previous.Offset || bar.Total != previous.Total
	switch {
	case bar.Total <= bar.Len:
		// Nothing to scroll through: no bar, however recently it moved.
		f.scrollbar.visible = 0
	case awayFromBottom || moved:
		f.scrollbar.visible = scrollbarFrames
	case f.scrollbar.visible > 0:
		f.scrollbar.visible--
	}
}

// exportScrollback saves selected text, or the whole scrollback, as HTML. The
// naming convention lives here, with the format the writer produces.
func (tab *terminal) exportScrollback() {
	name, err := desktop.SaveTemp("", "hypercat-*.html", func(out io.Writer) error {
		// The native formatter writes one chunk at a time, so the whole
		// scrollback would otherwise be a syscall per chunk.
		buffered := bufio.NewWriter(out)
		if err := tab.formatScrollback(buffered); err != nil {
			return err
		}
		return buffered.Flush()
	})
	if err != nil {
		// An export failure is reported without ending the terminal session.
		log.Printf("save scrollback: %v", err)
		return
	}
	log.Printf("scrollback saved to %s", name)
}

// formatScrollback keeps native formatting policy here: unwrap soft wraps,
// resolve the palette, and export the selection when one exists.
func (tab *terminal) formatScrollback(out io.Writer) error {
	options := gostty.FormatOptions{
		Format:         gostty.FormatterFormatHtml,
		Unwrap:         true,
		ResolvePalette: true,
	}
	return tab.onScreen(func(screen *gostty.Screen) error {
		sel, ok, err := screen.Selection()
		if err != nil {
			return err
		}
		if !ok {
			return screen.Format(options, out)
		}
		_, err = screen.FormatSelection(options, sel, out)
		return err
	})
}
