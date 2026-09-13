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
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// Everything here paints the frame that update read. Draw cannot report an
// error, so nothing here reads the terminal: it draws pixels only.

// draw paints one frame of the tab's content onto screen, with the cat on
// top. The panels and the tab bar are the window's.
func (tab *terminal) draw(screen *ebiten.Image, cat *thecat.Companion) {
	tab.drawGrid()
	screen.Fill(tab.frame.colors.bg)
	if tab.bell > 0 { // a visual bell: the GUI answer to \a
		tab.bell--
		screen.Fill(color.RGBA{R: 0x55, G: 0x55, B: 0x55, A: 0xff})
	}
	// A Kitty placement's layer says where in the stack it belongs: under the
	// cell backgrounds, between them and the text, or over the text.
	g := tab.grid()
	tab.images.draw(screen, gostty.KittyLayerBelowBg, g)
	if tab.layers.bg != nil {
		screen.DrawImage(tab.layers.bg, nil)
	}
	tab.images.draw(screen, gostty.KittyLayerBelowText, g)
	if tab.layers.text != nil {
		screen.DrawImage(tab.layers.text, nil)
	}
	tab.images.draw(screen, gostty.KittyLayerAboveText, g)
	tab.drawLink(screen)
	tab.drawCursor(screen)
	tab.drawScrollbar(screen)
	cat.Draw(screen, tab.currentTheme().Accent)
}

// themeColor maps a cell colour through the theme: the terminal's defaults
// become the theme's, explicit colours stay as they are.
func (tab *terminal) themeColor(c color.RGBA) color.RGBA {
	return tab.currentTheme().ResolveColor(c, tab.frame.colors.terminalBg, tab.frame.colors.terminalFg)
}

// drawGrid brings the two grid layers up to date, repainting only the rows
// marked since the last frame. Cells with the default background stay
// transparent so the fill and the bell show through.
func (tab *terminal) drawGrid() {
	g := tab.grid()
	if g.cols == 0 || g.rows == 0 || !g.holds(len(tab.frame.cells)) {
		return
	}
	// A pixel wider and taller than the cells, so a decoration on the last
	// row's baseline is not clipped.
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
		if tab.frame.redraw.take(row) {
			tab.layers.clearRect(image.Rect(0, int(g.y(row)), w, int(g.y(row+1))+1))
			tab.drawRow(row)
		}
	}
}

func (tab *terminal) drawRow(row int) {
	tab.drawRowBackground(tab.layers.bg, row)
	tab.drawRowGlyphs(tab.layers.text, row)
}

// matchHighlight tints every search match on screen; the current one is the
// screen's selection and keeps the selection colours.
var matchHighlight = color.RGBA{R: 0xb5, G: 0x89, B: 0x00, A: 0xff}

// cellBackground is a cell's background after selection and search: a
// selected cell swaps its colours.
func (tab *terminal) cellBackground(i int) color.RGBA {
	cell := tab.frame.cells[i]
	switch {
	case cell.Flags.Selected:
		return tab.themeColor(ui.FromColor(cell.Fg))
	case i < len(tab.search.cells) && tab.search.cells[i]:
		return matchHighlight
	}
	return tab.themeColor(ui.FromColor(cell.Bg))
}

// drawRowBackground fills the cells whose background is not the default, in
// runs: a row is usually one colour.
func (tab *terminal) drawRowBackground(dst *ebiten.Image, row int) {
	g := tab.grid()
	for start := 0; start < g.cols; {
		bg := tab.cellBackground(g.index(start, row))
		end := start + 1
		for end < g.cols && tab.cellBackground(g.index(end, row)) == bg {
			end++
		}
		if bg != tab.frame.colors.bg {
			vector.FillRect(dst, float32(g.x(start)), float32(g.y(row)), float32(g.x(end-start)), float32(g.cellH), bg, false)
		}
		start = end
	}
}

func (tab *terminal) drawRowGlyphs(dst *ebiten.Image, row int) {
	g := tab.grid()
	for col := range g.cols {
		cell := tab.frame.cells[g.index(col, row)]
		flags := cell.Flags
		// The tail of a wide character is drawn by its head, and the head of
		// a soft wrap is not drawn at all.
		if flags.Wide == gostty.CellWidthSpacerTail || flags.Wide == gostty.CellWidthSpacerHead {
			continue
		}
		if flags.Invisible || cell.Codepoint <= ' ' {
			continue
		}
		if flags.Blink && !tab.frame.blink { // SGR 5, in its dark half
			continue
		}
		fg := tab.themeColor(ui.FromColor(cell.Fg))
		if flags.Selected {
			fg = tab.themeColor(ui.FromColor(cell.Bg))
		}
		if flags.Faint {
			fg = color.RGBA{R: fg.R / 2, G: fg.G / 2, B: fg.B / 2, A: 0xff}
		}
		wide := flags.Wide == gostty.CellWidthWide
		x, y := g.x(col), g.y(row)
		// The cell's whole cluster, not its first codepoint: an accent, a
		// skin tone or the halves of a flag are part of what the cell says.
		tab.glyph(dst, tab.frame.clusterAt(g.index(col, row)), x, y, wide, flags.Bold, flags.Italic, fg)
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
// overline, at the font's own metrics.
func (tab *terminal) decorate(dst *ebiten.Image, flags gostty.CellFlags, x, y, width float64, fg color.RGBA) {
	f := tab.settings.fonts
	line := func(dy float64) {
		vector.FillRect(dst, float32(x), float32(y+dy), float32(width), float32(f.LineHeight), fg, false)
	}
	switch flags.Underline {
	case gostty.UnderlineNone:
	case gostty.UnderlineDouble:
		line(f.UnderlineY)
		line(f.Underline2Y)
	default:
		line(f.UnderlineY)
	}
	if flags.Strikethrough {
		line(f.StrikeY)
	}
	if flags.Overline {
		line(0)
	}
}

// glyph draws one cell's text at its top-left corner. A string rather than a
// rune, because a cell holds a grapheme cluster and the text renderer shapes
// what it is given. The face follows the width the terminal gave the cell,
// then the cell's bold and italic; a family with no bold face is struck twice
// a pixel apart, as terminals have always done.
func (tab *terminal) glyph(dst *ebiten.Image, str string, x, y float64, wide, bold, italic bool, fg color.RGBA) {
	// A two-column cell may be a picture: colour emoji are bitmaps the text
	// renderer cannot draw, so they are drawn as images.
	if wide && tab.drawEmoji(dst, str, x, y) {
		return
	}
	f := tab.settings.fonts
	face, synthetic := f.Face(bold, italic)
	dx, dy := 0.0, 0.0
	if wide { // one wide face at one weight: CJK is drawn upright whatever the cell says
		face, synthetic, dx, dy = f.Wide, false, f.WideDX, f.WideDY
	}
	op := &tab.layers.op
	op.GeoM.Reset()
	if f.Scale != 1 { // the bitmap fallback: whole-number scale, unfiltered
		op.GeoM.Scale(f.Scale, f.Scale)
		op.Filter = ebiten.FilterNearest
	}
	op.GeoM.Translate(x+dx, y+dy)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(fg)
	text.Draw(dst, str, face, op)
	if synthetic {
		op.GeoM.Translate(f.LineHeight, 0)
		text.Draw(dst, str, face, op)
	}
}

// drawEmoji paints the picture for a two-column cell, and reports whether
// there was one. The whole cluster is offered: a flag is two regional
// indicators, a family is three people and two joiners.
func (tab *terminal) drawEmoji(dst *ebiten.Image, cluster string, x, y float64) bool {
	emoji := tab.settings.emoji
	if emoji == nil {
		return false
	}
	g := tab.grid()
	img, ok := emoji.Glyph(cluster, g.cellH)
	if !ok {
		return false
	}
	// Fit inside the two cells, square: a stretched emoji is worse than a small one.
	box, line := 2*g.cellW, g.cellH
	w, h := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
	scale := math.Min(box/w, line/h)
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(x+(box-w*scale)/2, y+(line-h*scale)/2)
	dst.DrawImage(img, op)
	return true
}

// drawCursor paints the cursor in the colour the program asked for (OSC 12),
// or the foreground. On a wide character it covers both cells.
func (tab *terminal) drawCursor(screen *ebiten.Image) {
	c := tab.frame.cursor
	if !c.visible || !cursorLit(c.blinking, time.Now()) {
		return
	}
	fill := tab.frame.colors.fg
	if c.hasColor {
		fill = tab.themeColor(c.color)
	}
	g := tab.grid()
	x, y := g.x(int(c.x)), g.y(int(c.y))
	cursorCell := tab.frame.cursorCellIndex(g)
	width := g.cellW
	switch {
	case c.wideTail:
		x -= width
		width *= 2
	case cursorCell >= 0 && tab.frame.cells[cursorCell].Flags.Wide == gostty.CellWidthWide:
		width *= 2
	}
	thickness := float32(2 * tab.settings.fonts.LineHeight)
	switch c.style {
	case gostty.CursorStyleBar:
		vector.FillRect(screen, float32(x), float32(y), thickness, float32(g.cellH), fill, false)
	case gostty.CursorStyleUnderline:
		vector.FillRect(screen, float32(x), float32(y+g.cellH)-thickness, float32(width), thickness, fill, false)
	default: // block
		vector.FillRect(screen, float32(x), float32(y), float32(width), float32(g.cellH), fill, false)
		// Redraw the glyph in the background colour so it stays legible --
		// except while the program reads a password, where the block stays
		// solid rather than spelling out what was typed.
		if cursorCell >= 0 && !c.password {
			cell := tab.frame.cells[cursorCell]
			if cell.Codepoint > ' ' {
				wide := cell.Flags.Wide == gostty.CellWidthWide
				tab.glyph(screen, tab.frame.clusterAt(cursorCell), x, y, wide, cell.Flags.Bold, cell.Flags.Italic, tab.frame.colors.bg)
			}
		}
	}
}

// The link underline and the scrollbar are painted over the finished grid
// rather than into it, because they follow the pointer and the viewport
// rather than the cells: a row the terminal did not change still has to lose
// its underline when the pointer leaves.

func (tab *terminal) drawLink(screen *ebiten.Image) {
	link := tab.frame.link
	if !link.valid() {
		return
	}
	g := tab.grid()
	thickness := 2 * tab.settings.fonts.LineHeight
	y := g.y(link.row) + g.cellH - thickness
	// The foreground brightened, so it reads as a link on a light or a dark
	// background without the theme naming a colour for it.
	fg := tab.frame.colors.fg
	mix := func(v uint8) uint8 { return uint8(int(v)/2 + 0x60) }
	vector.FillRect(screen, float32(g.x(link.start)), float32(y), float32(g.x(link.end-link.start)), float32(thickness),
		color.RGBA{R: mix(fg.R), G: mix(fg.G), B: mix(fg.B), A: 0xff}, false)
}

const scrollbarWidth = 4

func (tab *terminal) drawScrollbar(screen *ebiten.Image) {
	bar := tab.frame.scrollbar.bar
	if tab.frame.scrollbar.visible <= 0 || bar.Total <= bar.Len || bar.Total == 0 {
		return
	}
	g := tab.grid()
	width := scrollbarWidth * tab.settings.dsf
	height := g.height()
	// The thumb is the viewport's share of the whole, kept big enough to see.
	thumb := max(height*float64(bar.Len)/float64(bar.Total), 2*width)
	top := (height - thumb) * float64(bar.Offset) / float64(bar.Total-bar.Len)
	// Track and thumb are the foreground at two transparencies.
	fg := tab.frame.colors.fg
	x := float32(g.width() - width)
	vector.FillRect(screen, x, 0, float32(width), float32(height), color.RGBA{R: fg.R / 8, G: fg.G / 8, B: fg.B / 8, A: 0x30}, false)
	vector.FillRect(screen, x, float32(top), float32(width), float32(thumb), color.RGBA{R: fg.R / 2, G: fg.G / 2, B: fg.B / 2, A: 0xb0}, false)
}

// canvas is what the ui package draws with: the tab's cell metrics and text
// renderer, so the panels line up with the terminal behind them.
func (tab *terminal) canvas(screen *ebiten.Image) ui.Canvas {
	g := tab.grid()
	return ui.Canvas{
		Screen: screen, CellWidth: g.cellW, CellHeight: g.cellH, Width: g.width(), Height: g.height(),
		Scale: tab.settings.dsf, Theme: tab.currentTheme(), DrawText: tab.drawText, RuneWidth: runeWidth,
	}
}

// drawText writes a line in the grid's cell width and returns where it ended.
func (tab *terminal) drawText(screen *ebiten.Image, s string, x, y float64, fg color.RGBA) float64 {
	cellW := tab.grid().cellW
	for _, r := range s {
		wide := runeWidth(r) == 2
		tab.glyph(screen, glyphString(r), x, y, wide, false, false, fg)
		x += cellW
		if wide {
			x += cellW
		}
	}
	return x
}

// runeWidth asks the binding how many columns a rune takes, the same answer
// the terminal used when it laid the grid out.
func runeWidth(r rune) int {
	w, err := gostty.CodepointWidth(r)
	if err != nil {
		return 1
	}
	return int(w)
}

// asciiGlyphs maps the common runes to the one-rune strings text.Draw wants,
// so a screen of text does not allocate one string per cell per frame.
var asciiGlyphs = func() (table [128]string) {
	for i := range table {
		table[i] = string(rune(i))
	}
	return table
}()

func glyphString(r rune) string {
	if r >= 0 && r < rune(len(asciiGlyphs)) {
		return asciiGlyphs[r]
	}
	return string(r)
}

// gridCanvas is the two GPU layers the grid is painted on, kept apart so
// Kitty images can sit between them, and the draw options glyphs reuse.
type gridCanvas struct {
	bg, text *ebiten.Image
	op       text.DrawOptions
}

// fit sizes the layers to the grid and reports whether it had to rebuild
// them, which is when everything on them has to be drawn again.
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

// clearRect wipes one row of both layers so it can be drawn again without
// disturbing the rows that did not change.
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
