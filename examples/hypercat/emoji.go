package main

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// drawEmoji paints the picture for a cell, and reports whether there was one.
// The caller has already decided the cell is two columns wide.
//
// The cell's whole cluster is offered, since that is what the picture belongs
// to: a flag is two regional indicators and a family is three people and two
// joiners, and either one is one two-column cell.
func (tab *terminalTab) drawEmoji(screen *ebiten.Image, cluster string, x, y float64) bool {
	if tab.emoji() == nil {
		return false
	}
	img, ok := tab.emoji().Glyph(cluster, tab.fonts().CellHeight)
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
