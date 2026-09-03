package main

import (
	"image/color"
	"log"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/example/thecat"
)

// A cat that walks around on the text.
//
// The `thecat` package has the sprites, the gravity and the moods; what it does
// not have is any idea what the ground is. That comes from here, and here the
// ground is the output: a cell with a glyph in it is something to stand on, so
// the cat walks along the top of a line of text, steps up onto the next one,
// and falls off the end of it into whatever is below.
//
// Nothing is tracked to make that work. The render state is already a flat
// array of the cells on screen, refreshed every frame for the renderer, so the
// cat reads the same array the glyphs are drawn from -- which means it follows
// the shell's output, and the scrollback, without being told either exists.

// How tall the cat is, in rows of text.
const catRows = 4

// The cat only chases the pointer while it is being moved. A pointer left
// sitting somewhere is furniture, not a target.
const catAttentionTicks = 90

func (g *game) startCat() {
	cat, err := thecat.New(catRows * g.fonts.cellH)
	if err != nil {
		// A cat that will not load is not a reason to lose the terminal.
		log.Printf("no cat: %v", err)
		return
	}
	g.cat = cat
	g.catOn = true
	// Place the sprite itself in the middle of the viewport. Place takes the
	// position of the cat's feet, so move it down by half its height.
	height := catRows * g.fonts.cellH
	g.cat.Place(
		float64(g.cols)*g.fonts.cellW/2,
		float64(g.rows)*g.fonts.cellH/2+height/2,
	)
}

func (g *game) updateCat() {
	if !g.catOn || g.cat == nil {
		g.catHover = false
		return
	}
	// The pointer counts as a target for a moment after it moves.
	x, y := ebiten.CursorPosition()
	if x != g.catPointerX || y != g.catPointerY {
		g.catPointerX, g.catPointerY = x, y
		g.catAttention = catAttentionTicks
	} else if g.catAttention > 0 {
		g.catAttention--
	}
	g.cat.SetHeight(catRows * g.fonts.cellH)
	g.catHover = g.cat.Hit(float64(x), float64(y))
	g.frame++
	g.cat.Update(g, 1.0/float64(ebiten.TPS()))
}

// pokeCat answers a click on the cat, and reports whether it took it.
//
// Taking it matters: a click that lands on the cat must not also start a
// selection or be reported to the program, or prodding the cat would highlight
// half a line of output behind it.
func (g *game) pokeCat() bool {
	if !g.catOn || g.cat == nil || !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return false
	}
	// Asked again here rather than reusing the hover from the last frame: the
	// pointer may have arrived between then and the click.
	x, y := ebiten.CursorPosition()
	if !g.cat.Hit(float64(x), float64(y)) {
		return false
	}
	g.cat.Poke()
	// A poke is also how the settings are found, since the cat is the most
	// clickable thing on the screen and the panel is where it is turned off.
	_ = g.openSettings()
	return true
}

func (g *game) drawCat(screen *ebiten.Image) {
	if !g.catOn || g.cat == nil {
		return
	}
	if g.cat.Hyper() {
		// One trip around the color wheel every four seconds at 60 FPS.
		phase := float64(g.frame) / 240
		g.cat.DrawTrail(screen, phase)
		g.cat.Glow(screen, scaleAlpha(rainbowColor(phase+0.06), 0.18))
		g.cat.Outline(screen, scaleAlpha(rainbowColor(phase), 0.62))
	}
	if g.catHover {
		g.drawCatHover(screen)
	}
	g.cat.Draw(screen)
}

// rainbowColor walks once around a saturated RGB color wheel as phase moves
// from 0 to 1.
func rainbowColor(phase float64) color.RGBA {
	h := math.Mod(phase, 1) * 6
	if h < 0 {
		h += 6
	}
	x := uint8((1 - math.Abs(math.Mod(h, 2)-1)) * 255)
	switch int(h) {
	case 0:
		return color.RGBA{R: 0xff, G: x, A: 0xff}
	case 1:
		return color.RGBA{R: x, G: 0xff, A: 0xff}
	case 2:
		return color.RGBA{G: 0xff, B: x, A: 0xff}
	case 3:
		return color.RGBA{G: x, B: 0xff, A: 0xff}
	case 4:
		return color.RGBA{R: x, B: 0xff, A: 0xff}
	default:
		return color.RGBA{R: 0xff, B: x, A: 0xff}
	}
}

// The pointer is on the cat: draw a line around the animal itself, so what is
// highlighted is the cat rather than the text behind it.
func (g *game) drawCatHover(screen *ebiten.Image) {
	// A slow pulse, so the outline reads as a highlight rather than as part of
	// the terminal's own output.
	pulse := 0.55 + 0.45*math.Sin(float64(g.frame)/12)
	g.cat.Outline(screen, scaleAlpha(g.currentTheme().accent, pulse))
}

func scaleAlpha(c color.RGBA, by float64) color.RGBA {
	c.A = uint8(float64(c.A) * by)
	return c
}

// -- The world the cat walks in --------------------------------------------

// GroundBelow finds the first line of text at or below a point, and reports the
// top of it -- which is where a cat standing on that line puts its feet.
//
// The bottom of the window is the floor, so there is always an answer.
func (g *game) GroundBelow(x, from float64) (float64, bool) {
	floor := float64(g.rows) * g.fonts.cellH
	// Floor, not a cast: a cast truncates towards zero, which would put every
	// point in the first column-width to the left of the window into column 0.
	col := int(math.Floor(x / g.fonts.cellW))
	if col < 0 || col >= g.cols || len(g.cells) < g.rows*g.cols {
		return floor, true
	}
	// The first row boundary at or below the point.
	row := max(int(math.Ceil(from/g.fonts.cellH)), 0)
	for ; row < g.rows; row++ {
		if hasInk(g.cells[row*g.cols+col]) {
			return float64(row) * g.fonts.cellH, true
		}
	}
	return floor, true
}

// Solid reports what the cat cannot walk through, as opposed to what it can
// stand on.
//
// Only stacked text counts: a cell is solid when the cell above it has a glyph
// too. A single line of output is therefore a ledge and nothing more -- the cat
// steps onto it from above and walks past it from the side -- while a paragraph
// is a wall to climb and a ceiling to duck under.
//
// The alternative, every glyph a wall, was tried and is unusable: a cat several
// text rows tall fits nowhere on a screen with output on it, so it spends its
// life pressed against the letter it happens to be next to. What counts as a
// wall is a fact about text rather than about cats, which is why it is decided
// here and not in the cat.
func (g *game) Solid(x, y float64) bool {
	col := int(math.Floor(x / g.fonts.cellW))
	row := int(math.Floor(y / g.fonts.cellH))
	if col < 0 || col >= g.cols || row <= 0 || row >= g.rows {
		return false
	}
	if len(g.cells) < g.rows*g.cols {
		return false
	}
	return hasInk(g.cells[row*g.cols+col]) && hasInk(g.cells[(row-1)*g.cols+col])
}

func (g *game) Bounds() (w, h float64) {
	return float64(g.cols) * g.fonts.cellW, float64(g.rows) * g.fonts.cellH
}

func (g *game) Attention() (x, y float64, ok bool) {
	if g.catAttention <= 0 {
		return 0, 0, false
	}
	return float64(g.catPointerX), float64(g.catPointerY), true
}

// hasInk reports whether a cell is something to stand on. A space is not, and
// neither is the tail of a wide character -- that belongs to the cell before
// it, which is already ground.
func hasInk(cell gostty.RenderCell) bool {
	if cell.Codepoint <= ' ' {
		return false
	}
	flags := gostty.CellFlagsFromBacking(cell.Flags)
	return !flags.Invisible
}
