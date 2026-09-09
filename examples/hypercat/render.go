package main

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/ui"
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
	screen.Fill(tab.frame.colors.bg)
	if tab.bell > 0 {
		// A visual bell: the GUI answer to \a.
		tab.bell--
		screen.Fill(color.RGBA{R: 0x55, G: 0x55, B: 0x55, A: 0xff})
	}
	// A placement's layer says where in the stack it belongs, which is the
	// whole reason the protocol gives it a z: under the cell backgrounds,
	// between them and the text, or over the text.
	g := tab.grid()
	tab.images.draw(screen, gostty.KittyLayerBelowBg, g.cellW, g.cellH)
	if tab.layers.bg != nil {
		screen.DrawImage(tab.layers.bg, nil)
	}
	tab.images.draw(screen, gostty.KittyLayerBelowText, g.cellW, g.cellH)
	if tab.layers.text != nil {
		screen.DrawImage(tab.layers.text, nil)
	}
	tab.images.draw(screen, gostty.KittyLayerAboveText, g.cellW, g.cellH)
	tab.drawLink(screen)
	tab.drawCursor(screen)
	tab.drawScrollbar(screen)
	tab.drawCat(screen)
}

// drawGrid brings the two grid layers up to date, redrawing only the rows
// marked dirty since the last frame. Cells with the default background stay
// transparent so the fill and the bell show through.
func (tab *terminalTab) drawGrid() {
	g := tab.grid()
	if g.cols == 0 || g.rows == 0 || len(tab.frame.cells) < g.cols*g.rows {
		return
	}
	// A pixel wider and taller than the cells, so a decoration drawn on the
	// last row's baseline is not clipped away.
	w, h := int(g.width())+1, int(g.height())+1
	if tab.layers.fit(w, h) {
		tab.frame.redraw.markAll()
	}
	if tab.frame.redraw.all {
		tab.layers.clear()
		for row := range g.rows {
			tab.drawRow(row)
		}
		tab.frame.redraw.clear()
		return
	}
	for row := range g.rows {
		if !tab.frame.redraw.take(row) {
			continue
		}
		tab.layers.clearRect(image.Rect(0, int(g.y(row)), w, int(g.y(row+1))+1))
		tab.drawRow(row)
	}
}

func (tab *terminalTab) drawRow(row int) {
	tab.drawRowBackground(tab.layers.bg, row)
	tab.drawRowGlyphs(tab.layers.text, row)
}

// drawRowBackground fills the cells whose background is not the default, in
// runs. A row is usually one colour, so filling per cell would be a few
// thousand draws a frame to say what a few dozen say.
func (tab *terminalTab) drawRowBackground(dst *ebiten.Image, row int) {
	g := tab.grid()
	line := tab.frame.cells[g.index(0, row):g.index(0, row+1)]
	for start := 0; start < len(line); {
		bg := tab.cellBackgroundAt(g.index(start, row))
		end := start + 1
		for end < len(line) && tab.cellBackgroundAt(g.index(end, row)) == bg {
			end++
		}
		if bg != tab.frame.colors.bg {
			vector.FillRect(dst,
				float32(g.x(start)), float32(g.y(row)),
				float32(g.x(end-start)), float32(g.cellH),
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
		return tab.themeColor(ui.RGB(cell.Fg))
	}
	return tab.themeColor(ui.RGB(cell.Bg))
}

func (tab *terminalTab) cellBackgroundAt(i int) color.RGBA {
	if i < len(tab.search.cells) && tab.search.cells[i] && !tab.frame.cells[i].Flags.Selected {
		return matchHighlight
	}
	return tab.cellBackground(tab.frame.cells[i])
}

func (tab *terminalTab) drawRowGlyphs(dst *ebiten.Image, row int) {
	g := tab.grid()
	for col := range g.cols {
		cell := tab.frame.cells[g.index(col, row)]
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
		if flags.Blink && !tab.frame.blink {
			continue
		}

		fg := tab.themeColor(ui.RGB(cell.Fg))
		if flags.Selected {
			fg = tab.themeColor(ui.RGB(cell.Bg))
		}
		if flags.Faint {
			fg = color.RGBA{R: fg.R / 2, G: fg.G / 2, B: fg.B / 2, A: 0xff}
		}

		wide := flags.Wide == gostty.CellWidthWide
		x, y := g.x(col), g.y(row)
		// The cell's text, not its first codepoint: an accent, a skin tone or
		// the halves of a flag are all part of what this one cell says.
		tab.glyph(dst, tab.frame.clusterAt(g, col, row, cell), x, y, wide, flags.Bold, flags.Italic, fg)

		if flags.Underline != gostty.UnderlineNone || flags.Strikethrough || flags.Overline {
			width := g.cellW
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

	op := &tab.layers.op
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
	if !tab.frame.cursor.visible || !cursorLit(tab.frame.cursor.blinking, time.Now()) {
		return
	}
	// The colour the program asked for, and the foreground only as a fallback:
	// a program that sets OSC 12 means the cursor to be that colour.
	fill := tab.frame.colors.fg
	if tab.frame.cursor.hasColor {
		fill = tab.themeColor(tab.frame.cursor.color)
	}
	g := tab.grid()
	x, y := g.x(int(tab.frame.cursor.x)), g.y(int(tab.frame.cursor.y))
	cursorCell := tab.cursorCellIndex(g)
	// A cursor on a wide character covers both of its cells, whether it landed
	// on the head or on the tail, so the glyph is not left half-lit.
	width := g.cellW
	switch {
	case tab.frame.cursor.wideTail:
		x -= width
		width *= 2
	case cursorCell >= 0 && tab.frame.cells[cursorCell].Flags.Wide == gostty.CellWidthWide:
		width *= 2
	}
	thickness := float32(2 * tab.fonts().LineHeight)

	switch tab.frame.cursor.style {
	case gostty.CursorStyleBar:
		vector.FillRect(screen, float32(x), float32(y), thickness, float32(g.cellH), fill, false)
	case gostty.CursorStyleUnderline:
		vector.FillRect(screen, float32(x), float32(y+g.cellH)-thickness,
			float32(width), thickness, fill, false)
	default: // block
		vector.FillRect(screen, float32(x), float32(y),
			float32(width), float32(g.cellH), fill, false)
		// Redraw the glyph in the background color so it stays legible --
		// except while the program is reading a password, where the block is
		// left solid rather than spelling out what was typed.
		if cursorCell >= 0 && !tab.frame.cursor.password {
			cell := tab.frame.cells[cursorCell]
			if cell.Codepoint > ' ' {
				flags := cell.Flags
				wide := flags.Wide == gostty.CellWidthWide
				tab.glyph(screen, tab.frame.clusterAt(g, int(tab.frame.cursor.x), int(tab.frame.cursor.y), cell),
					x, y, wide, flags.Bold, flags.Italic, tab.frame.colors.bg)
			}
		}
	}
}

// cursorCellIndex is the cell the cursor sits on, or -1 when it is off the
// grid the last refresh read. The tail of a wide character is drawn from its
// head, so that is the cell this reports.
func (tab *terminalTab) cursorCellIndex(g grid) int {
	x, y := int(tab.frame.cursor.x), int(tab.frame.cursor.y)
	if tab.frame.cursor.wideTail {
		x--
	}
	i := g.index(x, y)
	if x < 0 || x >= g.cols || i < 0 || i >= len(tab.frame.cells) {
		return -1
	}
	return i
}

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
	g := tab.grid()
	img, ok := tab.emoji().Glyph(cluster, g.cellH)
	if !ok {
		return false
	}

	// Fit inside the two cells it was given, keeping it square: emoji are
	// drawn square and a stretched one is worse than a small one.
	box, line := 2*g.cellW, g.cellH
	w := float64(img.Bounds().Dx())
	h := float64(img.Bounds().Dy())
	scale := math.Min(box/w, line/h)

	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(x+(box-w*scale)/2, y+(line-h*scale)/2)
	screen.DrawImage(img, op)
	return true
}
