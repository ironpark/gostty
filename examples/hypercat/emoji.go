package main

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// drawEmoji paints the picture for a cell, and reports whether there was one.
// The caller has already decided the cell is two columns wide.
func (tab *terminalTab) drawEmoji(screen *ebiten.Image, r rune, x, y float64) bool {
	if tab.emoji() == nil {
		return false
	}
	img, ok := tab.emoji().Glyph(r, tab.fonts().CellHeight)
	if !ok {
		return false
	}

	// Fit inside the two cells it was given, keeping it square: emoji are
	// drawn square and a stretched one is worse than a small one.
	box, line := 2*tab.fonts().CellWidth, tab.fonts().CellHeight
	w := float64(img.Bounds().Dx())
	h := float64(img.Bounds().Dy())
	scale := math.Min(box/w, line/h)

	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(x+(box-w*scale)/2, y+(line-h*scale)/2)
	screen.DrawImage(img, op)
	return true
}
