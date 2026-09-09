package thecat

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

const (
	companionRows  = 4
	attentionTicks = 90
)

// Companion adds pointer interaction and visual effects to a cat in a grid.
// The caller supplies input and decides what a consumed click should do.
type Companion struct {
	*Cat
	GridWorld
	enabled, hover     bool
	attention, frame   int
	pointerX, pointerY int
}

func NewCompanion(grid GridWorld) (*Companion, error) {
	height := companionRows * grid.CellHeight
	cat, err := New(height)
	if err != nil {
		return nil, err
	}
	width, floor := grid.Bounds()
	cat.Place(width/2, floor/2+height/2)
	return &Companion{Cat: cat, GridWorld: grid, enabled: true}, nil
}

// Mode is the companion's setting: on, tireless, or hidden. The zero value is
// the default, so a companion built without one still gets a cat.
type Mode int

const (
	ModeOn Mode = iota
	ModeHyper
	ModeOff
	modeCount
)

// Step wraps around the modes in either direction.
func (m Mode) Step(delta int) Mode {
	n := int(modeCount)
	return Mode(((int(m)+delta)%n + n) % n)
}

func (m Mode) String() string {
	switch m {
	case ModeHyper:
		return "hyper"
	case ModeOff:
		return "off"
	default:
		return "on"
	}
}

func (c *Companion) Enabled() bool { return c != nil && c.enabled }

// Mode derives the setting from the two switches it drives.
func (c *Companion) Mode() Mode {
	switch {
	case !c.Enabled():
		return ModeOff
	case c.Hyper():
		return ModeHyper
	default:
		return ModeOn
	}
}

// SetMode is nil-safe like the rest of the companion's API: a window whose
// sprites would not load still has a cat setting, it just has no cat.
func (c *Companion) SetMode(m Mode) {
	if c == nil {
		return
	}
	c.SetEnabled(m != ModeOff)
	c.SetHyper(m == ModeHyper)
}

func (c *Companion) SetEnabled(on bool) {
	c.enabled = on
	if on {
		c.attention = attentionTicks
	} else {
		c.hover = false
	}
}

// ClearHover removes the highlight when the containing view loses focus.
func (c *Companion) ClearHover() {
	if c != nil {
		c.hover = false
	}
}

// Update uses the latest grid and pointer position. dt is in seconds.
func (c *Companion) Update(grid GridWorld, x, y int, dt float64) {
	if !c.Enabled() {
		c.ClearHover()
		return
	}
	c.GridWorld = grid
	if x != c.pointerX || y != c.pointerY {
		c.pointerX, c.pointerY = x, y
		c.attention = attentionTicks
	} else if c.attention > 0 {
		c.attention--
	}
	c.TargetX, c.TargetY = float64(x), float64(y)
	c.TargetActive = c.attention > 0
	c.Cat.SetHeight(companionRows * grid.CellHeight)
	c.hover = c.Cat.Hit(float64(x), float64(y))
	c.frame++
	c.Cat.Update(c, dt)
}

// PokeAt reports whether the cat consumed a click at the given position.
func (c *Companion) PokeAt(x, y int) bool {
	if !c.Enabled() || !c.Cat.Hit(float64(x), float64(y)) {
		return false
	}
	c.Cat.Poke()
	return true
}

// Draw paints the cat, hyper effects, and the pointer highlight.
func (c *Companion) Draw(screen *ebiten.Image, accent color.RGBA) {
	if !c.Enabled() {
		return
	}
	if c.Hyper() {
		phase := float64(c.frame) / 240
		c.Cat.DrawTrail(screen, phase)
		c.Cat.Glow(screen, scaleAlpha(rainbowColor(phase+0.06), 0.18))
		c.Cat.Outline(screen, scaleAlpha(rainbowColor(phase), 0.62))
	}
	if c.hover {
		pulse := 0.55 + 0.45*math.Sin(float64(c.frame)/12)
		c.Cat.Outline(screen, scaleAlpha(accent, pulse))
	}
	c.Cat.Draw(screen)
}

func scaleAlpha(c color.RGBA, by float64) color.RGBA {
	c.A = uint8(float64(c.A) * by)
	return c
}
