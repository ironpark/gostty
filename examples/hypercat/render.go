package main

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/ironpark/gostty"
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

func (tab *terminalTab) Draw(screen *ebiten.Image) {
	tab.drawGrid()
	screen.Fill(tab.bg)
	if tab.bell > 0 {
		// A visual bell: the GUI answer to \a.
		tab.bell--
		screen.Fill(color.RGBA{R: 0x55, G: 0x55, B: 0x55, A: 0xff})
	}
	// A placement's layer says where in the stack it belongs, which is the
	// whole reason the protocol gives it a z: under the cell backgrounds,
	// between them and the text, or over the text.
	tab.drawImages(screen, gostty.KittyLayerBelowBg)
	if tab.bgLayer != nil {
		screen.DrawImage(tab.bgLayer, nil)
	}
	tab.drawImages(screen, gostty.KittyLayerBelowText)
	if tab.textLayer != nil {
		screen.DrawImage(tab.textLayer, nil)
	}
	tab.drawImages(screen, gostty.KittyLayerAboveText)
	tab.drawCursor(screen)
	tab.drawCat(screen)
	tab.drawUI(screen)
}

// drawGrid brings the two grid layers up to date, redrawing only the rows
// marked dirty since the last frame. Cells with the default background stay
// transparent so the fill and the bell show through.
func (tab *terminalTab) drawGrid() {
	if tab.cols == 0 || tab.rows == 0 || len(tab.cells) < tab.rows*tab.cols {
		return
	}
	w := int(float64(tab.cols)*tab.fonts.CellWidth) + 1
	h := int(float64(tab.rows)*tab.fonts.CellHeight) + 1
	if tab.bgLayer == nil || tab.bgLayer.Bounds().Dx() != w || tab.bgLayer.Bounds().Dy() != h {
		if tab.bgLayer != nil {
			tab.bgLayer.Deallocate()
			tab.textLayer.Deallocate()
		}
		tab.bgLayer = ebiten.NewImage(w, h)
		tab.textLayer = ebiten.NewImage(w, h)
		tab.redrawAll = true
	}
	if tab.redrawAll {
		tab.bgLayer.Clear()
		tab.textLayer.Clear()
		for row := 0; row < tab.rows; row++ {
			tab.drawRow(row)
		}
		tab.redrawAll = false
		clear(tab.rowDirty)
		return
	}
	for row, dirty := range tab.rowDirty {
		if !dirty || row >= tab.rows {
			continue
		}
		rect := image.Rect(0, int(float64(row)*tab.fonts.CellHeight), w, int(float64(row+1)*tab.fonts.CellHeight)+1)
		tab.bgLayer.SubImage(rect).(*ebiten.Image).Clear()
		tab.textLayer.SubImage(rect).(*ebiten.Image).Clear()
		tab.drawRow(row)
		tab.rowDirty[row] = false
	}
}

func (tab *terminalTab) drawRow(row int) {
	tab.drawRowBackground(tab.bgLayer, row)
	tab.drawRowGlyphs(tab.textLayer, row)
}

// drawRowBackground fills the cells whose background is not the default, in
// runs. A row is usually one colour, so filling per cell would be a few
// thousand draws a frame to say what a few dozen say.
func (tab *terminalTab) drawRowBackground(dst *ebiten.Image, row int) {
	line := tab.cells[row*tab.cols : (row+1)*tab.cols]
	for start := 0; start < len(line); {
		bg := tab.cellBackgroundAt(row*tab.cols + start)
		end := start + 1
		for end < len(line) && tab.cellBackgroundAt(row*tab.cols+end) == bg {
			end++
		}
		if bg != tab.bg {
			vector.FillRect(dst,
				float32(float64(start)*tab.fonts.CellWidth), float32(float64(row)*tab.fonts.CellHeight),
				float32(float64(end-start)*tab.fonts.CellWidth), float32(tab.fonts.CellHeight),
				bg, false)
		}
		start = end
	}
}

// matchHighlight tints every search match that is on screen; the current one
// is the screen's selection, so it keeps the selection colours.
var matchHighlight = color.RGBA{R: 0xb5, G: 0x89, B: 0x00, A: 0xff}

func (tab *terminalTab) cellBackground(cell gostty.RenderCell) color.RGBA {
	if cell.Flags.Selected {
		return tab.themeColor(rgb(cell.Fg))
	}
	return tab.themeColor(rgb(cell.Bg))
}

func (tab *terminalTab) cellBackgroundAt(i int) color.RGBA {
	if i < len(tab.matchCells) && tab.matchCells[i] && !tab.cells[i].Flags.Selected {
		return matchHighlight
	}
	return tab.cellBackground(tab.cells[i])
}

func (tab *terminalTab) drawRowGlyphs(dst *ebiten.Image, row int) {
	for col := 0; col < tab.cols; col++ {
		cell := tab.cells[row*tab.cols+col]
		flags := cell.Flags
		// The tail of a wide character is drawn by the head, and the head of a
		// soft-wrap is not drawn at all.
		if flags.Wide == gostty.CellWidthSpacerTail || flags.Wide == gostty.CellWidthSpacerHead {
			continue
		}
		if flags.Invisible || cell.Codepoint == 0 || cell.Codepoint == ' ' {
			continue
		}

		fg := tab.themeColor(rgb(cell.Fg))
		if flags.Selected {
			fg = tab.themeColor(rgb(cell.Bg))
		}
		if flags.Faint {
			fg = color.RGBA{R: fg.R / 2, G: fg.G / 2, B: fg.B / 2, A: 0xff}
		}

		wide := flags.Wide == gostty.CellWidthWide
		x := float64(col) * tab.fonts.CellWidth
		y := float64(row) * tab.fonts.CellHeight
		tab.glyph(dst, cell.Codepoint, x, y, wide, flags.Bold, flags.Italic, fg)

		if flags.Underline != gostty.UnderlineNone || flags.Strikethrough || flags.Overline {
			width := tab.fonts.CellWidth
			if wide {
				width *= 2
			}
			tab.decorate(dst, flags, x, y, width, fg)
		}
	}
}

// decorate draws the lines the font does not: underline, strikethrough and
// overline. Where they go is a font metric, so it comes from the font set.
func (tab *terminalTab) decorate(screen *ebiten.Image, flags gostty.CellFlags, x, y, width float64, fg color.RGBA) {
	line := func(dy float64) {
		vector.FillRect(screen, float32(x), float32(y+dy),
			float32(width), float32(tab.fonts.LineHeight), fg, false)
	}
	switch flags.Underline {
	case gostty.UnderlineNone:
	case gostty.UnderlineDouble:
		// The one style worth distinguishing from a single line at this size.
		line(tab.fonts.UnderlineY)
		line(tab.fonts.Underline2Y)
	default:
		line(tab.fonts.UnderlineY)
	}
	if flags.Strikethrough {
		line(tab.fonts.StrikeY)
	}
	if flags.Overline {
		line(0)
	}
}

// glyph draws one character at a cell's top-left corner.
//
// The face is chosen by the width the terminal gave the cell rather than by
// which font happens to have the glyph, and then by the cell's own bold and
// italic. A family that has no bold face is struck twice instead, a pixel
// apart: losing the distinction entirely is worse than a thickened glyph, and
// it is what terminals have always done.
func (tab *terminalTab) glyph(screen *ebiten.Image, r rune, x, y float64, wide, bold, italic bool, fg color.RGBA) {
	// A two-column cell may be a picture rather than a character. Colour emoji
	// are stored in their font as bitmaps, which the text renderer cannot draw
	// at all, so they are fetched and drawn as images instead.
	if wide && tab.drawEmoji(screen, r, x, y) {
		return
	}

	face, synthetic := tab.fonts.Face(bold, italic)
	dx, dy := 0.0, 0.0
	if wide {
		// One wide face, at one weight: CJK text is drawn upright whatever the
		// cell says, rather than shown in a face from another family.
		face, synthetic, dx, dy = tab.fonts.Wide, false, tab.fonts.WideDX, tab.fonts.WideDY
	}

	op := &tab.drawOp
	op.GeoM.Reset()
	if s := tab.fonts.Scale; s != 1 {
		// The bitmap fallback is scaled by a whole number and drawn unfiltered
		// so its pixels stay square.
		op.GeoM.Scale(s, s)
		op.Filter = ebiten.FilterNearest
	}
	op.GeoM.Translate(x+dx, y+dy)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(fg)
	str := glyphString(r)
	text.Draw(screen, str, face, op)
	if synthetic {
		op.GeoM.Translate(tab.fonts.LineHeight, 0)
		text.Draw(screen, str, face, op)
	}
}

// drawCursor paints the cursor from the state refresh() read. Nothing here
// touches the terminal: Draw cannot report an error, so it draws pixels only.
func (tab *terminalTab) drawCursor(screen *ebiten.Image) {
	if !tab.cursor.visible {
		return
	}
	x := float64(tab.cursor.x) * tab.fonts.CellWidth
	y := float64(tab.cursor.y) * tab.fonts.CellHeight
	thickness := float32(2 * tab.fonts.LineHeight)

	switch tab.cursor.style {
	case gostty.CursorStyleBar:
		vector.FillRect(screen, float32(x), float32(y), thickness, float32(tab.fonts.CellHeight), tab.fg, false)
	case gostty.CursorStyleUnderline:
		vector.FillRect(screen, float32(x), float32(y+tab.fonts.CellHeight)-thickness,
			float32(tab.fonts.CellWidth), thickness, tab.fg, false)
	default: // block
		vector.FillRect(screen, float32(x), float32(y),
			float32(tab.fonts.CellWidth), float32(tab.fonts.CellHeight), tab.fg, false)
		// Redraw the glyph in the background color so it stays legible.
		if i := int(tab.cursor.y)*tab.cols + int(tab.cursor.x); i < len(tab.cells) {
			cell := tab.cells[i]
			if r := cell.Codepoint; r > ' ' {
				flags := cell.Flags
				wide := flags.Wide == gostty.CellWidthWide
				tab.glyph(screen, r, x, y, wide, flags.Bold, flags.Italic, tab.bg)
			}
		}
	}
}
