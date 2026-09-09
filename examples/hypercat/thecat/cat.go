// Package thecat is a cat that walks around on whatever you give it to stand
// on.
//
// It knows about sprites, gravity and moods, and nothing at all about
// terminals: the world comes from a World the caller implements, so what the
// cat walks on is the caller's business. In this repository that is the text on
// the screen -- a line of output is a ledge, a paragraph is a wall to climb --
// but a platformer's tilemap would do just as well.
//
// The sprites are by Jump Button (@jumpbutton.bsky.social); see cat/Read_me.txt
// for their terms.
package thecat

import (
	"embed"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

//go:embed cat
var assets embed.FS

// Every sheet is a horizontal strip of frames this tall and this wide.
const frameSize = 32

// World is what the cat moves through, in the caller's pixels.
type World interface {
	// GroundBelow reports the top of the first surface at or below `from` in
	// the column at `x`, and whether there is one at all. A world with a floor
	// always has one.
	GroundBelow(x, from float64) (float64, bool)

	// Solid reports whether there is something at a point: a wall in the way, a
	// ceiling overhead. GroundBelow answers what can be stood on; this answers
	// what cannot be walked through.
	Solid(x, y float64) bool

	// Bounds is the area the cat stays inside.
	Bounds() (w, h float64)

	// Attention is somewhere the cat should go, and whether it cares. A mouse
	// pointer, usually. Without one the cat wanders and then sleeps.
	Attention() (x, y float64, ok bool)
}

// mode is how the cat is getting about, which decides what a tick does. The
// State is only what it looks like while doing it.
type mode int

const (
	onGround mode = iota
	inAir
	onWall
)

// Cat is one cat. Everything is in the caller's pixels; the sprite is scaled to
// whatever height it was asked for.
type Cat struct {
	// Where the cat's feet are: x is the middle of it, y is the sole.
	x, y   float64
	vx, vy float64

	height float64 // one sprite, scaled
	scale  float64
	facing float64 // +1 drawn as-is (rightwards), -1 mirrored
	mode   mode

	state State
	// Seconds spent in the current state, and which frame that works out to.
	elapsed float64
	frame   int

	// How long since anything interesting happened, which is what turns a
	// standing cat into a sleeping one.
	bored float64
	// Where it decided to wander to, when nothing else is going on.
	wanderX     float64
	wanderUntil float64
	// How far it fell, so a long drop can be a spin and a short one cannot.
	fellFrom float64
	// Seconds before it may jump again, so a ledge it cannot clear does not
	// turn the cat into a pogo stick.
	jumpCooldown float64
	// Seconds spent leaning on a wall before it gives up and turns round.
	wallFor float64
	// Seconds left ignoring whatever it was chasing, after a wall got in the
	// way of it. Without this a cat pressed against a wall it cannot climb
	// turns round, is told to go back, and leans on the same wall for ever.
	sulk float64
	// Set while there is something directly overhead.
	ducking bool

	// Stamina, 0 to 1. Running, climbing and jumping spend it; standing about
	// and sleeping put it back. It is what makes a chase across the screen end
	// in a cat that has to sit down, rather than one that runs at the same
	// speed for ever.
	sp float64
	// Sparks thrown off by a poke, oldest first.
	sparks []spark
	// Hyper mode keeps the cat at full stamina and leaves short-lived copies of
	// its recent frames behind it.
	hyper     bool
	trailTick int
	trail     []afterimage

	// Set once the gauge has run out, until it is back up to `rested`. The two
	// thresholds are different on purpose: recovering to a hair above zero and
	// setting off again would give a cat that stutters instead of resting.
	spent bool

	anims map[State]anim
}

// New loads the sprites and returns a cat `height` pixels tall.
//
// The sprite is pixel art, so it is scaled by a whole number and drawn
// unfiltered: half a pixel of smoothing turns a two-pixel eye into a smudge.
// The height asked for is therefore the nearest one that is a whole multiple.
func New(height float64) (*Cat, error) {
	c := &Cat{facing: 1, sp: 1, anims: make(map[State]anim, len(sheets))}
	// One sheet can serve two states -- sitting down and standing up are the
	// same pictures either way round -- so it is decoded and uploaded once.
	cut := map[string][]*ebiten.Image{}
	for state, sheet := range sheets {
		frames, ok := cut[sheet.file]
		if !ok {
			var err error
			if frames, err = slice(sheet.file); err != nil {
				return nil, err
			}
			cut[sheet.file] = frames
		}
		c.anims[state] = anim{
			frames:   frames,
			hold:     1 / sheet.fps,
			loop:     sheet.loop,
			loopFrom: sheet.loopFrom,
			reverse:  sheet.reverse,
		}
	}
	c.SetHeight(height)
	return c, nil
}

// SetHyper enables the tireless, faster cat. Turning it on also releases a cat
// that was already sitting down to recover.
func (c *Cat) SetHyper(on bool) {
	c.hyper = on
	if on {
		c.sp, c.spent = 1, false
		if c.recovering() {
			c.setState(Idle)
		}
	} else {
		c.trail = c.trail[:0]
	}
}

// Hyper reports whether Hyper Cat mode is active.
func (c *Cat) Hyper() bool { return c.hyper }

// SetHeight rescales the cat, keeping its feet where they are. The window's
// font can change size underneath it.
func (c *Cat) SetHeight(height float64) {
	c.scale = math.Max(1, math.Round(height/frameSize))
	c.height = c.scale * frameSize
}

// Place drops the cat somewhere, feet first.
func (c *Cat) Place(x, y float64) {
	c.x, c.y = x, y
	c.vx, c.vy = 0, 0
	c.mode = inAir
	// Where the fall started, so landing right here is not reported as a drop
	// from the top of the world.
	c.fellFrom = y
	c.setState(Fall)
}

func (c *Cat) State() State { return c.state }

// Update moves the cat on by one tick.
func (c *Cat) Update(w World, dt float64) {
	width, height := w.Bounds()
	if width <= 0 || height <= 0 {
		return
	}
	c.elapsed += dt
	c.updateTrail(dt)
	c.jumpCooldown = math.Max(0, c.jumpCooldown-dt)

	targetX, targetY, chasing := w.Attention()
	if c.sulk > 0 {
		c.sulk -= dt
		chasing = false
	}
	if chasing {
		c.bored = 0
	} else {
		c.bored += dt
		targetX, targetY = c.wander(width, dt)
	}

	switch {
	case c.mode == onWall:
		c.climb(w, targetY)
	case c.mode == inAir:
		c.fly(targetX, dt)
	case (c.spent || c.recovering()) && c.bored <= boredom:
		// Run out, or still getting over it. Falling asleep is allowed -- it is
		// the better way to get a gauge back -- but chasing anything is not.
		c.rest()
	default:
		c.walk(w, targetX, targetY, chasing, dt)
	}

	c.x += c.vx * dt
	c.y += c.vy * dt

	// The window's edges are walls. Running into one stops the cat rather than
	// letting it walk out of the world.
	half := c.height / 2
	if c.x < half {
		c.x, c.vx = half, 0
	}
	if c.x > width-half {
		c.x, c.vx = width-half, 0
	}

	c.settle(w, height)
	c.spend(dt)
	c.updateSparks(dt)
	c.animate()
	c.captureAfterimage()
}

// Feet is where the cat is standing, for a caller that needs to know.
func (c *Cat) Feet() (x, y float64) { return c.x, c.y }
