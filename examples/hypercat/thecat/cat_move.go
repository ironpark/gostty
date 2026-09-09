// Getting about: walking, jumping, climbing, falling, and the reading of the
// world that decides between them.
package thecat

import (
	"math"
	"math/rand/v2"
)

// Speeds and distances, in cat-heights per second. Tying them to the sprite
// rather than to pixels keeps the cat moving the same way when the font size
// changes under it.
const (
	walkSpeed  = 1.1
	runSpeed   = 3.2
	climbSpeed = 1.4
	hyperSpeed = 1.55
	gravity    = 26.0

	// How close counts as arrived, so the cat does not jitter on the spot.
	arrived = 0.35
	// Beyond this it runs rather than walks.
	hurry = 2.5

	// A step it can walk up or down without leaving the ground.
	stepUp   = 0.35
	stepDown = 0.35
	// The tallest thing a jump can get onto. Anything higher is a wall.
	jumpReach = 1.5
	// Cleared by this much, so the cat lands on a ledge rather than in it.
	clearance = 0.3
	// Seconds between jumps.
	jumpEvery = 0.45

	// How far ahead the cat looks for ledges and walls.
	probe = 0.45
	// A wall has to reach this far up before it is worth climbing rather than
	// leaning on.
	climbable = 1.2
	// Seconds spent leaning on a wall before turning round.
	leanFor = 1.2

	// A drop this long is a fall worth spinning through.
	longFall = 2.0
	// Seconds of nothing before the cat sits down and sleeps.
	boredom = 6.0
)

// walk is what the cat does with its feet on something.
func (c *Cat) walk(w World, targetX, targetY float64, chasing bool, dt float64) {
	// Something directly overhead means crawling rather than walking, and no
	// jumping at all -- there is nowhere to jump to.
	c.ducking = c.overhead(w, c.x)

	dx := targetX - c.x
	if math.Abs(dx) < arrived*c.height {
		c.arrive(targetY, chasing)
		return
	}

	c.facing = math.Copysign(1, dx)
	ahead := c.x + c.facing*probe*c.height

	// What is in the way, and what could be got onto.
	switch obstacle := c.look(w, ahead); obstacle.kind {
	case blocked:
		c.lean(w, targetY, dt)
		return
	case ledge:
		if c.jumpCooldown == 0 && !c.ducking && c.sp > jumpCost && c.worthJumping(targetY, obstacle.y) {
			// Exactly enough to clear it, rather than a fixed leap: a line of
			// text is a hop and a paragraph is a bound.
			c.jumpTo(obstacle.y)
			return
		}
	}
	c.wallFor = 0

	speed, moving := walkSpeed, Walk
	switch {
	case c.ducking:
		speed, moving = walkSpeed*0.7, DuckWalk
	case math.Abs(dx) > hurry*c.height:
		// A run winds down towards a walk as the gauge empties, so the cat
		// visibly tires before it has to stop.
		speed, moving = walkSpeed+(runSpeed-walkSpeed)*c.vigour(), Run
	}
	if c.hyper {
		speed *= hyperSpeed
	}
	c.vx = c.facing * speed * c.height
	if c.state != Land && c.state != Bonk || c.finished() {
		c.setState(moving)
	}
}

// arrive is what the cat does once it is where it wanted to be.
func (c *Cat) arrive(targetY float64, chasing bool) {
	c.vx = 0
	c.wallFor = 0
	switch {
	case c.bored > boredom:
		c.setState(Sleep)
	case c.ducking:
		// Under something: there is no standing up to celebrate.
		c.setState(Duck)
	case chasing && targetY > c.y-c.height:
		// Caught the pointer: make a show of it, then bat at it for as long as
		// it stays put.
		switch {
		case c.state == Cheer && c.elapsed > 1.2:
			c.setState(Attack)
		case c.state == Attack && c.finished():
			c.setState(Cheer)
		case c.state != Attack && c.state != Cheer:
			c.setState(Cheer)
		}
	case c.state != Land && c.state != Bonk && c.state != Attack || c.finished():
		c.setState(Idle)
	}
}

// overhead reports whether anything is in the space the cat's head needs.
//
// Several points rather than one, from the shoulders to just over the ears: a
// line of text is a quarter of a cat thick, so a single probe slips between the
// lines and the cat walks through them with its head up.
func (c *Cat) overhead(w World, x float64) bool {
	for _, at := range [...]float64{1.05, 0.9, 0.75, 0.6} {
		if w.Solid(x, c.y-at*c.height) {
			return true
		}
	}
	return false
}

// obstacleKind is what the cat found in front of it.
type obstacleKind int

const (
	clear obstacleKind = iota
	// A surface it could get onto with a jump.
	ledge
	// Too tall to jump onto: a wall to climb or lean on.
	blocked
)

type obstacle struct {
	kind obstacleKind
	// The top of the ledge, when there is one.
	y float64
}

// look reports what is in the way just ahead of the cat.
//
// Two questions, because a world made of text answers them differently. What
// could be stood on up there -- a line of output has nothing above it, so its
// top is a ledge. And whether the way is blocked at chest height and still
// blocked above the reach of a jump -- a paragraph is, and that is a wall.
func (c *Cat) look(w World, ahead float64) obstacle {
	chest := w.Solid(ahead, c.y-0.4*c.height)
	if chest && w.Solid(ahead, c.y-(jumpReach+0.2)*c.height) {
		return obstacle{kind: blocked}
	}

	top, ok := w.GroundBelow(ahead, c.y-jumpReach*c.height)
	if !ok || top >= c.y-stepUp*c.height {
		// Level with the cat, or a step it can walk straight up.
		return obstacle{kind: clear}
	}
	// No headroom test: what a cat's head passes through is the caller's
	// business, and a world of text is drawn behind the cat anyway. Requiring a
	// body's clearance above every ledge made a wall of each line of output.
	return obstacle{kind: ledge, y: top}
}

// worthJumping reports whether getting onto a ledge takes the cat towards what
// it is after.
//
// Jumping at anything above it is what turned the cat into a pogo stick: the
// pointer is usually somewhere up the screen, and every line of text on the way
// is a ledge. So the target has to be above the cat by more than a step, and
// the ledge has to be part of the way there rather than past it.
func (c *Cat) worthJumping(targetY, ledgeY float64) bool {
	return targetY < c.y-stepUp*c.height && ledgeY >= targetY-c.height
}

// jumpTo leaves the ground with exactly the speed needed to arrive on `top`.
//
// Worked out rather than fixed, which is the difference between hopping onto
// the next line of text and launching over the whole screen.
func (c *Cat) jumpTo(top float64) {
	rise := (c.y - top) + clearance*c.height
	c.vy = -math.Sqrt(2 * gravity * c.height * rise)
	c.mode = inAir
	c.fellFrom = c.y
	c.jumpCooldown = jumpEvery
	c.sp = math.Max(0, c.sp-jumpCost)
	c.setState(Jump)
}

// lean is what happens at a wall: climb it if it is worth climbing, otherwise
// put a paw on it, think about it, and turn round.
func (c *Cat) lean(w World, targetY, dt float64) {
	c.vx = 0
	// A wall that carries on above the cat's head is a ladder, so long as there
	// is a reason to be up there.
	tall := w.Solid(c.x+c.facing*probe*c.height, c.y-climbable*c.height)
	if tall && targetY < c.y-c.height && !c.spent {
		c.mode = onWall
		c.wallFor = 0
		c.setState(Climb)
		return
	}

	c.setState(Wall)
	c.wallFor += dt
	if c.wallFor > leanFor {
		// Give up: turn round, and stop being told to come back for a while.
		c.wallFor = 0
		c.sulk = 4
		c.facing = -c.facing
		c.wanderX = c.x + c.facing*hurry*c.height
		c.wanderUntil = 3
	}
}

// climb goes up the face of a wall until it runs out, and steps onto the top.
func (c *Cat) climb(w World, targetY float64) {
	c.vx = 0
	c.vy = -climbSpeed * c.height
	if c.hyper {
		c.vy *= hyperSpeed
	}

	// Probed at the feet, not at the chest: the climb is over when the soles
	// clear the top edge, and stopping a chest-height earlier leaves the cat
	// stepping forward into the wall and dropping down the far side of it.
	face := c.x + c.facing*probe*c.height
	switch {
	case !w.Solid(face, c.y-0.1*c.height):
		// Over the top. Step onto it and let settle put the feet down.
		c.x += c.facing * probe * c.height
		c.vy = 0
		c.mode = inAir
		c.fellFrom = c.y
		c.setState(Jump)
	case c.y-c.height < 0 || c.y < targetY || c.spent:
		// Above what it was climbing for, out of the world, or out of puff:
		// let go.
		c.mode = inAir
		c.vy = 0
		c.fellFrom = c.y
		c.setState(Fall)
	default:
		c.setState(Climb)
	}
}

// fly is a cat in the air: gravity, and the little bit of steering every
// platformer lets you have.
func (c *Cat) fly(targetX, dt float64) {
	speed := runSpeed
	if c.hyper {
		speed *= hyperSpeed
	}
	want := math.Copysign(speed*c.height, targetX-c.x)
	if math.Abs(targetX-c.x) < arrived*c.height {
		want = 0
	}
	c.vx += (want - c.vx) * math.Min(1, 4*dt)
	c.vy += gravity * c.height * dt
	if c.vx != 0 {
		c.facing = math.Copysign(1, c.vx)
	}
}

// settle works out what the cat is standing on, if anything.
//
// One question does most of the job: what is the first surface at or below a
// point a step above the feet. Above the feet, because a surface it finds there
// is a step up onto the next line of text; below them, because one there is a
// step down, or a drop.
func (c *Cat) settle(w World, height float64) {
	if c.mode == onWall {
		return
	}

	ground, ok := w.GroundBelow(c.x, c.y-stepUp*c.height)
	if !ok {
		ground = height
	}

	if c.mode == inAir {
		if c.vy < 0 {
			// Rising: nothing to land on, but there is something to hit.
			if c.y-c.height < 0 || c.overhead(w, c.x) {
				c.vy = 0
				c.setState(Bonk)
			} else if c.vy > -0.8*c.height {
				c.setState(Apex)
			}
			return
		}
		if c.y >= ground {
			c.land(ground)
		} else if c.y-c.fellFrom > longFall*c.height {
			c.setState(Spin)
		} else if c.state != Spin {
			c.setState(Fall)
		}
		return
	}

	switch {
	case ground < c.y-stepUp*c.height:
		// Unreachable in one step; should not happen, since that is where the
		// question started, but do not fall through a floor over it.
		c.y = ground
	case ground <= c.y+stepDown*c.height:
		// A step, up or down: keep walking, no air time.
		c.y = ground
	default:
		// Walked off the end of the line.
		c.mode = inAir
		c.fellFrom = c.y
		c.setState(Fall)
	}
}

func (c *Cat) land(ground float64) {
	drop := c.y - c.fellFrom
	c.y, c.vy = ground, 0
	c.mode = onGround
	if drop > 0.5*c.height {
		c.setState(Land)
	}
}

// wander picks somewhere to go when nothing is asking for the cat's attention,
// and stops picking once it has fallen asleep.
//
// Always level with the cat: wandering gives it nowhere to be that is worth a
// jump, so a cat with nothing to chase keeps its feet on the text.
func (c *Cat) wander(width, dt float64) (x, y float64) {
	if c.bored > boredom {
		return c.x, c.y
	}
	c.wanderUntil -= dt
	if c.wanderUntil <= 0 {
		// Somewhere else on the screen, and a while to think about it.
		c.wanderX = rand.Float64() * width
		c.wanderUntil = 2 + 4*rand.Float64()
	}
	return c.wanderX, c.y
}
