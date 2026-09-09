package main

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/ironpark/gostty"
)

// The scrollbar is `Screen.Scrollbar`: the terminal counts what is in the
// scrollback and where the viewport sits in it, in rows, and hands over the
// three numbers a thumb is drawn from. Counting them here would mean tracking
// every scroll, every resize and every line that aged out of the scrollback --
// which the terminal is already doing.

// How wide the bar is, in device-independent pixels, and how long it stays
// visible after the last scroll.
const (
	scrollbarWidth  = 4
	scrollbarFrames = 90
)

// scrollbarState is the last reading, and how long it stays on screen. It is
// shown while the viewport is away from the bottom and fades out shortly after
// it comes back, the way an overlay scrollbar does: a terminal sitting at its
// prompt should not have a bar down the side of it.
type scrollbarState struct {
	bar     gostty.Scrollbar
	visible int
}

// refreshScrollbar reads where the viewport sits in the scrollback.
func (tab *terminalTab) refreshScrollbar() error {
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
	switch {
	case bar.Total <= bar.Len:
		// Nothing to scroll through: no bar, however recently it moved.
		f.scrollbar.visible = 0
	case bar.Offset+bar.Len < bar.Total:
		// Away from the bottom, which is the whole reason to show it.
		f.scrollbar.visible = scrollbarFrames
	case bar.Offset != previous.Offset || bar.Total != previous.Total:
		f.scrollbar.visible = scrollbarFrames
	case f.scrollbar.visible > 0:
		f.scrollbar.visible--
	}
}

// drawScrollbar paints the thumb down the right edge. Like the link underline
// it goes over the finished grid rather than into it, because it follows the
// viewport rather than the cells.
func (tab *terminalTab) drawScrollbar(screen *ebiten.Image) {
	bar := tab.frame.scrollbar.bar
	if tab.frame.scrollbar.visible <= 0 || bar.Total <= bar.Len || bar.Total == 0 {
		return
	}
	g := tab.grid()
	width := scrollbarWidth * tab.settings.dsf
	height := g.height()
	// The thumb is the viewport's share of the whole, kept big enough to see
	// on a scrollback that dwarfs it.
	thumb := max(height*float64(bar.Len)/float64(bar.Total), 2*width)
	top := (height - thumb) * float64(bar.Offset) / float64(bar.Total-bar.Len)

	x := float32(g.width() - width)
	vector.FillRect(screen, x, 0, float32(width), float32(height), scrollbarTrack(tab.frame.colors.fg), false)
	vector.FillRect(screen, x, float32(top), float32(width), float32(thumb), scrollbarThumb(tab.frame.colors.fg), false)
}

// The track and the thumb are the foreground colour at two transparencies, so
// they read on any theme without one having to name a colour for them.
func scrollbarTrack(fg color.RGBA) color.RGBA {
	return color.RGBA{R: fg.R / 8, G: fg.G / 8, B: fg.B / 8, A: 0x30}
}

func scrollbarThumb(fg color.RGBA) color.RGBA {
	return color.RGBA{R: fg.R / 2, G: fg.G / 2, B: fg.B / 2, A: 0xb0}
}
