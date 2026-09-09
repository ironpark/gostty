// Drawing the cat: the frame to show now, and the silhouettes drawn around it.
package thecat

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/colorm"
)

// frame is the sprite to draw now, and where to put it.
func (c *Cat) currentFrame() (*ebiten.Image, ebiten.GeoM, bool) {
	a := c.anims[c.state]
	if len(a.frames) == 0 {
		return nil, ebiten.GeoM{}, false
	}
	var geom ebiten.GeoM
	geom.Scale(c.scale*c.facing, c.scale)
	if c.facing < 0 {
		// Mirroring moves the sprite a width to the left; put it back.
		geom.Translate(c.height, 0)
	}
	geom.Translate(c.x-c.height/2, c.y-c.height)
	return a.frame(c.frame), geom, true
}

// Draw paints the cat at its feet.
func (c *Cat) Draw(dst *ebiten.Image) {
	frame, geom, ok := c.currentFrame()
	if !ok {
		return
	}
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest, GeoM: geom}
	dst.DrawImage(frame, op)

	// Over the top of it: a poke should read as coming off the animal.
	c.drawSparks(dst)
}

// Outline draws a line around the cat's own shape, one sprite pixel thick.
//
// The shape, not the frame: the sprite is a square with a good deal of air in
// it, so a box around it would be a box around nothing much. This draws the
// silhouette eight times, a pixel out in each direction, and the animal is then
// drawn over the middle of it -- which leaves exactly the pixels that have
// nothing beside them showing.
//
// A silhouette is a flat colour that keeps the sprite's alpha. Ebitengine's
// images are alpha-premultiplied, so it cannot be had by zeroing the colour and
// adding a constant -- that would tint the transparent pixels too. The colour
// matrix's alpha-to-red, alpha-to-green and alpha-to-blue terms give the
// premultiplied answer directly.
func (c *Cat) Outline(dst *ebiten.Image, clr color.Color) {
	c.drawOutline(dst, clr, 1)
}

// Glow draws a wider, translucent silhouette around a hyper cat.
func (c *Cat) Glow(dst *ebiten.Image, clr color.Color) {
	c.drawOutline(dst, clr, 3)
	c.drawOutline(dst, clr, 2)
}

func (c *Cat) drawOutline(dst *ebiten.Image, clr color.Color, spread float64) {
	frame, geom, ok := c.currentFrame()
	if !ok {
		return
	}
	r, g, b, a := clr.RGBA()
	alpha := float64(a) / 0xffff

	var cm colorm.ColorM
	cm.Scale(0, 0, 0, alpha)
	cm.SetElement(0, 3, float64(r)/0xffff*alpha)
	cm.SetElement(1, 3, float64(g)/0xffff*alpha)
	cm.SetElement(2, 3, float64(b)/0xffff*alpha)

	// One sprite pixel, which is what the cat is drawn in.
	step := c.scale
	for _, at := range [8][2]float64{
		{-1, -1}, {0, -1}, {1, -1},
		{-1, 0}, {1, 0},
		{-1, 1}, {0, 1}, {1, 1},
	} {
		op := &colorm.DrawImageOptions{Filter: ebiten.FilterNearest, GeoM: geom}
		op.GeoM.Translate(at[0]*step*spread, at[1]*step*spread)
		colorm.DrawImage(dst, frame, cm, op)
	}
}
