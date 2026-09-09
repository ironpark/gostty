package main

import (
	"image"
	"image/color"
	"time"

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

// gridCanvas is the pair of layers the grid is drawn into -- backgrounds and
// glyphs, kept apart so Kitty images can sit between them -- and the draw
// options the glyphs reuse. It is the only thing in the tab that owns GPU
// images of its own, so it is also the only thing that has to be closed.
type gridCanvas struct {
	bg, text *ebiten.Image
	op       text.DrawOptions
}

// fit sizes the layers to the grid and reports whether it had to build them,
// which is when everything that was on them has to be drawn again.
func (c *gridCanvas) fit(w, h int) bool {
	if c.bg != nil && c.bg.Bounds().Dx() == w && c.bg.Bounds().Dy() == h {
		return false
	}
	c.close()
	c.bg, c.text = ebiten.NewImage(w, h), ebiten.NewImage(w, h)
	return true
}

func (c *gridCanvas) clear() {
	c.bg.Clear()
	c.text.Clear()
}

// clearRect wipes one row's worth of both layers, so it can be drawn again
// without disturbing the rows that did not change.
func (c *gridCanvas) clearRect(r image.Rectangle) {
	c.bg.SubImage(r).(*ebiten.Image).Clear()
	c.text.SubImage(r).(*ebiten.Image).Clear()
}

func (c *gridCanvas) close() {
	if c.bg == nil {
		return
	}
	c.bg.Deallocate()
	c.text.Deallocate()
	c.bg, c.text = nil, nil
}

// Draw paints one frame of the tab's own content. The panels go on top of it,
// drawn by the window, which is what holds the settings they display.
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
	cellW, cellH := tab.fonts().CellWidth, tab.fonts().CellHeight
	tab.images.draw(screen, gostty.KittyLayerBelowBg, cellW, cellH)
	if tab.grid.bg != nil {
		screen.DrawImage(tab.grid.bg, nil)
	}
	tab.images.draw(screen, gostty.KittyLayerBelowText, cellW, cellH)
	if tab.grid.text != nil {
		screen.DrawImage(tab.grid.text, nil)
	}
	tab.images.draw(screen, gostty.KittyLayerAboveText, cellW, cellH)
	tab.drawLink(screen)
	tab.drawCursor(screen)
	tab.drawScrollbar(screen)
	tab.drawCat(screen)
}

// drawGrid brings the two grid layers up to date, redrawing only the rows
// marked dirty since the last frame. Cells with the default background stay
// transparent so the fill and the bell show through.
func (tab *terminalTab) drawGrid() {
	if tab.cols == 0 || tab.rows == 0 || len(tab.cells) < tab.rows*tab.cols {
		return
	}
	cellH := tab.fonts().CellHeight
	w := int(float64(tab.cols)*tab.fonts().CellWidth) + 1
	h := int(float64(tab.rows)*cellH) + 1
	if tab.grid.fit(w, h) {
		tab.redraw.markAll()
	}
	if tab.redraw.all {
		tab.grid.clear()
		for row := 0; row < tab.rows; row++ {
			tab.drawRow(row)
		}
		tab.redraw.clear()
		return
	}
	for row := range tab.rows {
		if !tab.redraw.take(row) {
			continue
		}
		tab.grid.clearRect(image.Rect(0, int(float64(row)*cellH), w, int(float64(row+1)*cellH)+1))
		tab.drawRow(row)
	}
}

func (tab *terminalTab) drawRow(row int) {
	tab.drawRowBackground(tab.grid.bg, row)
	tab.drawRowGlyphs(tab.grid.text, row)
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
				float32(float64(start)*tab.fonts().CellWidth), float32(float64(row)*tab.fonts().CellHeight),
				float32(float64(end-start)*tab.fonts().CellWidth), float32(tab.fonts().CellHeight),
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
	if i < len(tab.search.cells) && tab.search.cells[i] && !tab.cells[i].Flags.Selected {
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
		// SGR 5. The terminal only records that the cell blinks; when it is
		// dark is the renderer's to decide, and it is the same phase the
		// cursor uses so the two do not beat against each other.
		if flags.Blink && !tab.blink {
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
		x := float64(col) * tab.fonts().CellWidth
		y := float64(row) * tab.fonts().CellHeight
		// The cell's text, not its first codepoint: an accent, a skin tone or
		// the halves of a flag are all part of what this one cell says.
		tab.glyph(dst, tab.clusterAt(col, row, cell), x, y, wide, flags.Bold, flags.Italic, fg)

		if flags.Underline != gostty.UnderlineNone || flags.Strikethrough || flags.Overline {
			width := tab.fonts().CellWidth
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
			float32(width), float32(tab.fonts().LineHeight), fg, false)
	}
	switch flags.Underline {
	case gostty.UnderlineNone:
	case gostty.UnderlineDouble:
		// The one style worth distinguishing from a single line at this size.
		line(tab.fonts().UnderlineY)
		line(tab.fonts().Underline2Y)
	default:
		line(tab.fonts().UnderlineY)
	}
	if flags.Strikethrough {
		line(tab.fonts().StrikeY)
	}
	if flags.Overline {
		line(0)
	}
}

// glyph draws one cell's text at its top-left corner.
//
// A string rather than a rune, because a cell holds a grapheme cluster: the
// terminal lays out "e" plus a combining acute as one cell, and with grapheme
// clustering on so are a flag and a family. The text renderer shapes what it
// is given, so handing it the cluster is all that is needed for the marks to
// land on the letter.
//
// The face is chosen by the width the terminal gave the cell rather than by
// which font happens to have the glyph, and then by the cell's own bold and
// italic. A family that has no bold face is struck twice instead, a pixel
// apart: losing the distinction entirely is worse than a thickened glyph, and
// it is what terminals have always done.
func (tab *terminalTab) glyph(screen *ebiten.Image, str string, x, y float64, wide, bold, italic bool, fg color.RGBA) {
	// A two-column cell may be a picture rather than a character. Colour emoji
	// are stored in their font as bitmaps, which the text renderer cannot draw
	// at all, so they are fetched and drawn as images instead.
	if wide && tab.drawEmoji(screen, str, x, y) {
		return
	}

	face, synthetic := tab.fonts().Face(bold, italic)
	dx, dy := 0.0, 0.0
	if wide {
		// One wide face, at one weight: CJK text is drawn upright whatever the
		// cell says, rather than shown in a face from another family.
		face, synthetic, dx, dy = tab.fonts().Wide, false, tab.fonts().WideDX, tab.fonts().WideDY
	}

	op := &tab.grid.op
	op.GeoM.Reset()
	if s := tab.fonts().Scale; s != 1 {
		// The bitmap fallback is scaled by a whole number and drawn unfiltered
		// so its pixels stay square.
		op.GeoM.Scale(s, s)
		op.Filter = ebiten.FilterNearest
	}
	op.GeoM.Translate(x+dx, y+dy)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(fg)
	text.Draw(screen, str, face, op)
	if synthetic {
		op.GeoM.Translate(tab.fonts().LineHeight, 0)
		text.Draw(screen, str, face, op)
	}
}

// How long the cursor spends lit and how long dark, when the terminal says it
// blinks. Half a second each way is what the hardware terminals did and what
// every emulator has copied since.
const cursorBlinkPeriod = time.Second

// blinkLit is the lit half of the blink phase, from the clock rather than a
// frame counter so it does not speed up with the frame rate.
func blinkLit(now time.Time) bool {
	return now.UnixNano()%int64(cursorBlinkPeriod) < int64(cursorBlinkPeriod/2)
}

// cursorLit reports whether a blinking cursor is in its lit half.
func cursorLit(blinking bool, now time.Time) bool {
	return !blinking || blinkLit(now)
}

// drawCursor paints the cursor from the state refresh() read. Nothing here
// touches the terminal: Draw cannot report an error, so it draws pixels only.
func (tab *terminalTab) drawCursor(screen *ebiten.Image) {
	if !tab.cursor.visible || !cursorLit(tab.cursor.blinking, time.Now()) {
		return
	}
	// The colour the program asked for, and the foreground only as a fallback:
	// a program that sets OSC 12 means the cursor to be that colour.
	fill := tab.fg
	if tab.cursor.hasColor {
		fill = tab.themeColor(tab.cursor.color)
	}
	x := float64(tab.cursor.x) * tab.fonts().CellWidth
	y := float64(tab.cursor.y) * tab.fonts().CellHeight
	// A cursor on a wide character covers both of its cells, so the glyph
	// underneath is not left half-lit.
	width := tab.fonts().CellWidth
	if tab.cursor.wideTail {
		x -= width
		width *= 2
	} else if i := tab.cursorCellIndex(); i >= 0 && tab.cells[i].Flags.Wide == gostty.CellWidthWide {
		width *= 2
	}
	thickness := float32(2 * tab.fonts().LineHeight)

	switch tab.cursor.style {
	case gostty.CursorStyleBar:
		vector.FillRect(screen, float32(x), float32(y), thickness, float32(tab.fonts().CellHeight), fill, false)
	case gostty.CursorStyleUnderline:
		vector.FillRect(screen, float32(x), float32(y+tab.fonts().CellHeight)-thickness,
			float32(width), thickness, fill, false)
	default: // block
		vector.FillRect(screen, float32(x), float32(y),
			float32(width), float32(tab.fonts().CellHeight), fill, false)
		// Redraw the glyph in the background color so it stays legible --
		// except while the program is reading a password, where the block is
		// left solid rather than spelling out what was typed.
		if i := tab.cursorCellIndex(); i >= 0 && !tab.cursor.password {
			cell := tab.cells[i]
			if cell.Codepoint > ' ' {
				flags := cell.Flags
				wide := flags.Wide == gostty.CellWidthWide
				tab.glyph(screen, tab.clusterAt(int(tab.cursor.x), int(tab.cursor.y), cell),
					x, y, wide, flags.Bold, flags.Italic, tab.bg)
			}
		}
	}
}

// cursorCellIndex is the cell the cursor sits on, or -1 when it is off the
// grid the last refresh read. The tail of a wide character is drawn from its
// head, so that is the cell this reports.
func (tab *terminalTab) cursorCellIndex() int {
	x, y := int(tab.cursor.x), int(tab.cursor.y)
	if tab.cursor.wideTail {
		x--
	}
	i := y*tab.cols + x
	if x < 0 || x >= tab.cols || i < 0 || i >= len(tab.cells) {
		return -1
	}
	return i
}
