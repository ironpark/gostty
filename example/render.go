package main

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/ironpark/gostty"
)

// Style flags, as packed by the Zig side.
const (
	flagBold = 1 << iota
	flagItalic
	flagFaint
	flagBlink
	flagInverse
	flagInvisible
	flagStrikethrough
	flagOverline
)

// The underline style occupies bits 8..11; the selection flag sits above it.
const flagSelected = 1 << 12

// Cell widths, as reported by the render state.
const (
	widthNarrow = iota
	widthWide
	widthSpacerTail
	widthSpacerHead
)

func (g *game) Draw(screen *ebiten.Image) {
	screen.Fill(g.bg)
	if g.bell > 0 {
		// A visual bell: the GUI answer to \a.
		g.bell--
		screen.Fill(color.RGBA{R: 0x55, G: 0x55, B: 0x55, A: 0xff})
	}

	cols := g.cols
	for i, cell := range g.cells {
		// The tail of a wide character is drawn by the head, and the head of a
		// soft-wrap is not drawn at all.
		if cell.Wide == widthSpacerTail || cell.Wide == widthSpacerHead {
			continue
		}
		x := float64(i%cols) * g.fonts.cellW
		y := float64(i/cols) * g.fonts.cellH
		width := g.fonts.cellW
		if cell.Wide == widthWide {
			width *= 2
		}

		fg, bg := rgb(cell.Fg), rgb(cell.Bg)
		if cell.Flags&flagSelected != 0 {
			fg, bg = bg, fg
		}
		if bg != g.bg {
			vector.DrawFilledRect(screen, float32(x), float32(y), float32(width), float32(g.fonts.cellH), bg, false)
		}
		if cell.Flags&flagInvisible != 0 || cell.Codepoint == 0 || cell.Codepoint == ' ' {
			continue
		}
		if cell.Flags&flagFaint != 0 {
			fg = color.RGBA{R: fg.R / 2, G: fg.G / 2, B: fg.B / 2, A: 0xff}
		}
		g.glyph(screen, rune(cell.Codepoint), x, y, cell.Wide == widthWide, fg)

		g.decorate(screen, cell, x, y, width, fg)
	}

	g.drawCursor(screen)
}

// decorate draws the lines the font does not: underline, strikethrough and
// overline. The underline style lives in the high bits of the flags.
func (g *game) decorate(screen *ebiten.Image, cell gostty.RenderCell, x, y, width float64, fg color.RGBA) {
	line := func(dy float64) {
		vector.DrawFilledRect(screen, float32(x), float32(y+dy), float32(width), 1, fg, false)
	}
	if underline := (cell.Flags >> 8) & 0xf; underline != 0 {
		line(g.fonts.cellH - 2)
		// Double underline is the one style worth distinguishing at this size.
		if underline == 2 {
			line(g.fonts.cellH - 4)
		}
	}
	if cell.Flags&flagStrikethrough != 0 {
		line(g.fonts.cellH / 2)
	}
	if cell.Flags&flagOverline != 0 {
		line(0)
	}
}

// glyph draws one character at a cell's top-left corner, with the face chosen
// by the width the terminal gave the cell rather than by which font happens to
// have the glyph.
func (g *game) glyph(screen *ebiten.Image, r rune, x, y float64, wide bool, fg color.RGBA) {
	face, dx, dy := g.fonts.narrow, 0.0, 0.0
	if wide {
		face, dx, dy = g.fonts.wide, g.fonts.wideDX, g.fonts.wideDY
	}
	op := &text.DrawOptions{}
	if s := g.fonts.scale(); s != 1 {
		// The bitmap fallback is scaled by a whole number and drawn unfiltered
		// so its pixels stay square.
		op.GeoM.Scale(s, s)
		op.Filter = ebiten.FilterNearest
	}
	op.GeoM.Translate(x+dx, y+dy)
	op.ColorScale.ScaleWithColor(fg)
	text.Draw(screen, string(r), face, op)
}

func (g *game) drawCursor(screen *ebiten.Image) {
	visible, err := g.state.CursorVisible()
	if err != nil || !visible {
		return
	}
	cx, ok, err := g.state.CursorX()
	if err != nil || !ok {
		return // scrolled out of the viewport
	}
	cy, ok, err := g.state.CursorY()
	if err != nil || !ok {
		return
	}
	style, err := g.state.CursorStyle()
	if err != nil {
		return
	}

	fgv, err := g.state.Foreground()
	if err != nil {
		return
	}
	fg := rgb(fgv)
	x := float64(cx) * g.fonts.cellW
	y := float64(cy) * g.fonts.cellH

	switch style {
	case gostty.CursorStyleBar:
		vector.DrawFilledRect(screen, float32(x), float32(y), 2, float32(g.fonts.cellH), fg, false)
	case gostty.CursorStyleUnderline:
		vector.DrawFilledRect(screen, float32(x), float32(y+g.fonts.cellH-2), float32(g.fonts.cellW), 2, fg, false)
	default: // block
		vector.DrawFilledRect(screen, float32(x), float32(y), float32(g.fonts.cellW), float32(g.fonts.cellH), fg, false)
		// Redraw the glyph in the background color so it stays legible.
		if i := cy*uint16(g.cols) + cx; int(i) < len(g.cells) {
			if r := rune(g.cells[i].Codepoint); r > ' ' {
				g.glyph(screen, r, x, y, g.cells[i].Wide == widthWide, g.bg)
			}
		}
	}
}
