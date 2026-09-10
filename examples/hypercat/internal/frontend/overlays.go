package frontend

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const scrollbarWidth = 4

// drawLink underlines the hovered link. It is painted over the finished grid,
// not into it, because it follows the pointer rather than the terminal: a row
// the terminal did not change still has to lose its underline when the pointer
// leaves it.
func (tab *Renderer) drawLink(screen *ebiten.Image) {
	if !tab.frame.Link.valid() {
		return
	}
	g := tab.grid()
	thickness := 2 * tab.fonts().LineHeight
	y := g.y(tab.frame.Link.Row) + g.cellH - thickness
	vector.FillRect(screen,
		float32(g.x(tab.frame.Link.Start)), float32(y),
		float32(g.x(tab.frame.Link.End-tab.frame.Link.Start)), float32(thickness),
		linkColor(tab.frame.Colors.Fg), false)
}

// linkColor is the underline's colour: the foreground, brightened, so it reads
// as a link on either a light or a dark background without the theme having to
// name a colour for it.
func linkColor(fg color.RGBA) color.RGBA {
	mix := func(v uint8) uint8 { return uint8(int(v)/2 + 0x60) }
	return color.RGBA{R: mix(fg.R), G: mix(fg.G), B: mix(fg.B), A: 0xff}
}

// drawScrollbar paints the thumb down the right edge. Like the link underline
// it goes over the finished grid rather than into it, because it follows the
// viewport rather than the cells.
func (tab *Renderer) drawScrollbar(screen *ebiten.Image) {
	bar := tab.frame.Scrollbar.Bar
	if tab.frame.Scrollbar.Visible <= 0 || bar.Total <= bar.Len || bar.Total == 0 {
		return
	}
	g := tab.grid()
	width := scrollbarWidth * tab.frame.Scale
	height := g.height()
	// The thumb is the viewport's share of the whole, kept big enough to see
	// on a scrollback that dwarfs it.
	thumb := max(height*float64(bar.Len)/float64(bar.Total), 2*width)
	top := (height - thumb) * float64(bar.Offset) / float64(bar.Total-bar.Len)

	x := float32(g.width() - width)
	vector.FillRect(screen, x, 0, float32(width), float32(height), scrollbarTrack(tab.frame.Colors.Fg), false)
	vector.FillRect(screen, x, float32(top), float32(width), float32(thumb), scrollbarThumb(tab.frame.Colors.Fg), false)
}

// The track and the thumb are the foreground colour at two transparencies, so
// they read on any theme without one having to name a colour for them.
func scrollbarTrack(fg color.RGBA) color.RGBA {
	return color.RGBA{R: fg.R / 8, G: fg.G / 8, B: fg.B / 8, A: 0x30}
}

func scrollbarThumb(fg color.RGBA) color.RGBA {
	return color.RGBA{R: fg.R / 2, G: fg.G / 2, B: fg.B / 2, A: 0xb0}
}
