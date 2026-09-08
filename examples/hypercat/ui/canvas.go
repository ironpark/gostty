package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Canvas connects UI layout to the host's text renderer and cell metrics.
type Canvas struct {
	Screen                                      *ebiten.Image
	Width, Height, CellWidth, CellHeight, Scale float64
	Theme                                       Theme
	DrawText                                    func(*ebiten.Image, string, float64, float64, color.RGBA) float64
	RuneWidth                                   func(rune) int
}

func (c Canvas) Text(s string, x, y float64, fg color.RGBA) float64 {
	return c.DrawText(c.Screen, s, x, y, fg)
}

func (c Canvas) Panel(x, y, w, h float64) {
	vector.FillRect(c.Screen, float32(x), float32(y), float32(w), float32(h), c.Theme.Panel, false)
	vector.StrokeRect(c.Screen, float32(x), float32(y), float32(w), float32(h), 1, c.Theme.Border, false)
}

func (c Canvas) TextCells(s string) int {
	cells := 0
	for _, r := range s {
		cells += max(c.RuneWidth(r), 0)
	}
	return cells
}
