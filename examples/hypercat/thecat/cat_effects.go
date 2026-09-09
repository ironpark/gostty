// The cat's own effects: the trail a hyper cat leaves behind it, and the sparks
// a poke throws off.
package thecat

import (
	"image/color"
	"math"
	"math/rand/v2"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/colorm"
)

const (
	afterimageLife  = 0.32
	afterimageEvery = 2
	maxAfterimages  = 10
)

type afterimage struct {
	frame *ebiten.Image
	geom  ebiten.GeoM
	life  float64
}

func (c *Cat) updateTrail(dt float64) {
	alive := c.trail[:0]
	for _, trail := range c.trail {
		trail.life -= dt
		if trail.life > 0 {
			alive = append(alive, trail)
		}
	}
	c.trail = alive
}

func (c *Cat) captureAfterimage() {
	if !c.hyper || math.Abs(c.vx)+math.Abs(c.vy) < 0.05*c.height {
		return
	}
	c.trailTick++
	if c.trailTick%2 == 0 {
		c.emitHyperDust(2)
	}
	if c.trailTick%afterimageEvery != 0 {
		return
	}
	frame, geom, ok := c.currentFrame()
	if !ok {
		return
	}
	if len(c.trail) == maxAfterimages {
		copy(c.trail, c.trail[1:])
		c.trail = c.trail[:maxAfterimages-1]
	}
	c.trail = append(c.trail, afterimage{
		frame: frame, geom: geom, life: afterimageLife,
	})
}

// DrawTrail paints the recent motion snapshots behind a hyper cat.
func (c *Cat) DrawTrail(dst *ebiten.Image, phase float64) {
	for i, trail := range c.trail {
		clr := rainbowColor(phase - float64(len(c.trail)-i)*0.025)
		fade := 0.42 * trail.life / afterimageLife
		r, g, b, _ := clr.RGBA()
		var cm colorm.ColorM
		cm.Scale(0, 0, 0, fade)
		cm.SetElement(0, 3, float64(r)/0xffff*fade)
		cm.SetElement(1, 3, float64(g)/0xffff*fade)
		cm.SetElement(2, 3, float64(b)/0xffff*fade)
		op := &colorm.DrawImageOptions{Filter: ebiten.FilterNearest, GeoM: trail.geom}
		colorm.DrawImage(dst, trail.frame, cm, op)
	}
}

func (c *Cat) emitHyperDust(count int) {
	direction := math.Copysign(1, c.vx)
	for i := range count {
		phase := float64(c.trailTick)/240 + float64(i)/float64(count)*0.08
		c.sparks = append(c.sparks, spark{
			x:     c.x - direction*c.height*(0.15+0.25*rand.Float64()),
			y:     c.y - c.height*(0.15+0.65*rand.Float64()),
			vx:    -c.vx*(0.08+0.18*rand.Float64()) + (rand.Float64()-0.5)*c.height,
			vy:    -(0.15 + 0.5*rand.Float64()) * c.height,
			life:  0.25 + 0.3*rand.Float64(),
			size:  c.scale * float64(1+rand.IntN(2)),
			color: rainbowColor(phase),
		})
	}
}

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

// -- Being prodded ---------------------------------------------------------
//
// A cat on a screen invites a poke, so it answers one: a puff of sparks, and
// its attention. The sparks are the cat's own -- they come out of it and are
// drawn with it -- while what a click means is the caller's business.

// How many sparks a poke makes, and how long they last.
const (
	sparkCount = 36
	sparkLife  = 0.9
	// Sparks are thrown at up to this many cat-heights a second.
	sparkSpeed = 2.2
	// They are pulled down more gently than the cat is: this is a puff, not a
	// handful of gravel.
	sparkGravity = 3.0
)

type spark struct {
	x, y   float64
	vx, vy float64
	life   float64
	color  color.RGBA
	// Size in whole pixels, so the sparks look like the sprite they came from.
	size float64
}

// dot is the one pixel every spark is drawn from, made on first use.
var dot *ebiten.Image

// Box is where the cat is drawn: the top-left corner and the size.
func (c *Cat) Box() (x, y, w, h float64) {
	return c.x - c.height/2, c.y - c.height, c.height, c.height
}

// Hit reports whether a point is on the cat.
//
// The sprite is a square with a good deal of air around the animal, so this is
// the middle of it rather than the whole frame: a click a body's width away
// should not count as a poke.
func (c *Cat) Hit(x, y float64) bool {
	const inset = 0.2
	left, top, w, h := c.Box()
	return x >= left+inset*w && x <= left+(1-inset)*w &&
		y >= top+inset*h && y <= top+h
}

// Poke is the cat being prodded: it notices, and it makes a puff of sparks.
func (c *Cat) Poke() {
	c.bored = 0
	c.sulk = 0
	if c.mode == onGround && !c.spent {
		// Pleased to be noticed. Whatever it was doing resumes next tick.
		c.setState(Cheer)
	}
	count := sparkCount
	speedBoost := 1.0
	if c.hyper {
		count *= 2
		speedBoost = 1.35
	}
	for i := range count {
		// Aimed up and outwards from the middle of the animal, so the puff
		// blooms rather than dribbling down one side.
		angle := rand.Float64() * 2 * math.Pi
		speed := (0.35 + 0.65*rand.Float64()) * sparkSpeed * speedBoost * c.height
		clr := color.RGBA{R: 0xff, G: 0xe6, B: 0xa8, A: 0xff}
		if c.hyper {
			clr = rainbowColor(float64(i)/float64(count) + rand.Float64()*0.08)
		}
		c.sparks = append(c.sparks, spark{
			x:     c.x + (rand.Float64()-0.5)*c.height*0.35,
			y:     c.y - c.height*0.5 + (rand.Float64()-0.5)*c.height*0.35,
			vx:    math.Cos(angle) * speed,
			vy:    math.Sin(angle)*speed - 0.35*sparkSpeed*c.height,
			life:  sparkLife * (0.6 + 0.4*rand.Float64()),
			size:  c.scale * float64(1+rand.IntN(2)),
			color: clr,
		})
	}
}

// Sparkling reports whether there are still sparks in the air, which is the
// only reason to redraw a cat that is otherwise standing still.
func (c *Cat) Sparkling() bool { return len(c.sparks) > 0 }

func (c *Cat) updateSparks(dt float64) {
	alive := c.sparks[:0]
	for _, s := range c.sparks {
		s.life -= dt
		if s.life <= 0 {
			continue
		}
		s.vy += sparkGravity * c.height * dt
		s.x += s.vx * dt
		s.y += s.vy * dt
		alive = append(alive, s)
	}
	c.sparks = alive
}

func (c *Cat) drawSparks(dst *ebiten.Image) {
	if len(c.sparks) == 0 {
		return
	}
	if dot == nil {
		dot = ebiten.NewImage(1, 1)
		dot.Fill(color.White)
	}
	for _, s := range c.sparks {
		op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest}
		op.GeoM.Scale(s.size, s.size)
		op.GeoM.Translate(math.Round(s.x), math.Round(s.y))
		// Warm, and fading out over the tail of its life so the puff settles
		// rather than blinking off.
		fade := math.Min(1, s.life/(sparkLife*0.6))
		op.ColorScale.ScaleWithColor(s.color)
		op.ColorScale.ScaleAlpha(float32(fade))
		dst.DrawImage(dot, op)
	}
}
