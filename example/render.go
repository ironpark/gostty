package main

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/ironpark/gostty"
)

// Style flags, as packed by the Zig side. The underline style occupies bits
// 8..11 and holds a gostty.Underline; the selection flag sits above it.
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

const (
	underlineShift = 8
	underlineMask  = 0xf
	flagSelected   = 1 << 12
	// The flags that make decorate draw anything.
	flagsDecorated = flagStrikethrough | flagOverline | underlineMask<<underlineShift
)

// Cell widths, as reported by the render state.
const (
	widthNarrow = iota
	widthWide
	widthSpacerTail
	widthSpacerHead
)

// asciiGlyphs maps the common runes to the one-rune strings text.Draw wants,
// so a full screen of text does not allocate one string per cell per frame.
var asciiGlyphs = func() [128]string {
	var table [128]string
	for i := range table {
		table[i] = string(rune(i))
	}
	return table
}()

func glyphString(r rune) string {
	if r < rune(len(asciiGlyphs)) {
		return asciiGlyphs[r]
	}
	return string(r)
}

func (g *game) Draw(screen *ebiten.Image) {
	screen.Fill(g.bg)
	if g.bell > 0 {
		// A visual bell: the GUI answer to \a.
		g.bell--
		screen.Fill(color.RGBA{R: 0x55, G: 0x55, B: 0x55, A: 0xff})
	}
	g.drawBackgrounds(screen)
	g.drawGlyphs(screen)
	g.drawCursor(screen)
}

// drawBackgrounds fills the cells whose background is not the default, in runs.
// A row is usually one colour, so filling per cell would be a few thousand
// draws a frame to say what a few dozen say.
func (g *game) drawBackgrounds(screen *ebiten.Image) {
	for row := 0; row < g.rows; row++ {
		line := g.cells[row*g.cols : (row+1)*g.cols]
		for start := 0; start < len(line); {
			bg := g.cellBackground(line[start])
			end := start + 1
			for end < len(line) && g.cellBackground(line[end]) == bg {
				end++
			}
			if bg != g.bg {
				vector.DrawFilledRect(screen,
					float32(float64(start)*g.fonts.cellW), float32(float64(row)*g.fonts.cellH),
					float32(float64(end-start)*g.fonts.cellW), float32(g.fonts.cellH),
					bg, false)
			}
			start = end
		}
	}
}

func (g *game) cellBackground(cell gostty.RenderCell) color.RGBA {
	if cell.Flags&flagSelected != 0 {
		return rgb(cell.Fg)
	}
	return rgb(cell.Bg)
}

func (g *game) drawGlyphs(screen *ebiten.Image) {
	for i, cell := range g.cells {
		// The tail of a wide character is drawn by the head, and the head of a
		// soft-wrap is not drawn at all.
		if cell.Wide == widthSpacerTail || cell.Wide == widthSpacerHead {
			continue
		}
		if cell.Flags&flagInvisible != 0 || cell.Codepoint == 0 || cell.Codepoint == ' ' {
			continue
		}

		fg := rgb(cell.Fg)
		if cell.Flags&flagSelected != 0 {
			fg = rgb(cell.Bg)
		}
		if cell.Flags&flagFaint != 0 {
			fg = color.RGBA{R: fg.R / 2, G: fg.G / 2, B: fg.B / 2, A: 0xff}
		}

		x := float64(i%g.cols) * g.fonts.cellW
		y := float64(i/g.cols) * g.fonts.cellH
		g.glyph(screen, rune(cell.Codepoint), x, y, cell.Wide == widthWide, fg)

		if cell.Flags&flagsDecorated != 0 {
			width := g.fonts.cellW
			if cell.Wide == widthWide {
				width *= 2
			}
			g.decorate(screen, cell, x, y, width, fg)
		}
	}
}

// decorate draws the lines the font does not: underline, strikethrough and
// overline. Where they go is a font metric, so it comes from the font set.
func (g *game) decorate(screen *ebiten.Image, cell gostty.RenderCell, x, y, width float64, fg color.RGBA) {
	line := func(dy float64) {
		vector.DrawFilledRect(screen, float32(x), float32(y+dy),
			float32(width), float32(g.fonts.lineH), fg, false)
	}
	switch underline := gostty.Underline((cell.Flags >> underlineShift) & underlineMask); underline {
	case gostty.UnderlineNone:
	case gostty.UnderlineDouble:
		// The one style worth distinguishing from a single line at this size.
		line(g.fonts.underlineY)
		line(g.fonts.underline2Y)
	default:
		line(g.fonts.underlineY)
	}
	if cell.Flags&flagStrikethrough != 0 {
		line(g.fonts.strikeY)
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
	op := &g.drawOp
	op.GeoM.Reset()
	if s := g.fonts.scale; s != 1 {
		// The bitmap fallback is scaled by a whole number and drawn unfiltered
		// so its pixels stay square.
		op.GeoM.Scale(s, s)
		op.Filter = ebiten.FilterNearest
	}
	op.GeoM.Translate(x+dx, y+dy)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(fg)
	text.Draw(screen, glyphString(r), face, op)
}

// drawCursor paints the cursor from the state refresh() read. Nothing here
// touches the terminal: Draw cannot report an error, so it draws pixels only.
func (g *game) drawCursor(screen *ebiten.Image) {
	if !g.cursor.visible {
		return
	}
	x := float64(g.cursor.x) * g.fonts.cellW
	y := float64(g.cursor.y) * g.fonts.cellH
	thickness := float32(2 * g.fonts.lineH)

	switch g.cursor.style {
	case gostty.CursorStyleBar:
		vector.DrawFilledRect(screen, float32(x), float32(y), thickness, float32(g.fonts.cellH), g.fg, false)
	case gostty.CursorStyleUnderline:
		vector.DrawFilledRect(screen, float32(x), float32(y+g.fonts.cellH)-thickness,
			float32(g.fonts.cellW), thickness, g.fg, false)
	default: // block
		vector.DrawFilledRect(screen, float32(x), float32(y),
			float32(g.fonts.cellW), float32(g.fonts.cellH), g.fg, false)
		// Redraw the glyph in the background color so it stays legible.
		if i := int(g.cursor.y)*g.cols + int(g.cursor.x); i < len(g.cells) {
			cell := g.cells[i]
			if r := rune(cell.Codepoint); r > ' ' {
				g.glyph(screen, r, x, y, cell.Wide == widthWide, g.bg)
			}
		}
	}
}
