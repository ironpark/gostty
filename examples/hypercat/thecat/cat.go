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
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand/v2"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/colorm"
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

// State is what the cat is doing, which is also which sprite sheet it is drawn
// from.
type State int

const (
	Idle State = iota
	Walk
	Run
	Jump
	Apex
	Fall
	Spin
	Land
	Sleep
	Cheer
	Attack
	Wall
	Climb
	Duck
	DuckWalk
	Rest
	Winded
	Rise
	Bonk
)

func (s State) String() string {
	return [...]string{
		"idle", "walk", "run", "jumping", "at the top", "falling", "spinning",
		"landing", "asleep", "pleased", "batting at it", "at a wall",
		"climbing", "ducking", "crawling", "sitting down",
		"getting its breath back", "getting up", "bonked",
	}[s]
}

// frame is the picture at a point in an animation, which is not the same as the
// count of frames played: an animation marked `reverse` is the same sheet read
// back to front, so the counting, the looping and "has it finished" all stay as
// they are.
func (a anim) frame(at int) *ebiten.Image {
	i := min(at, len(a.frames)-1)
	if a.reverse {
		i = len(a.frames) - 1 - i
	}
	return a.frames[i]
}

// anim is one sheet, sliced.
type anim struct {
	frames []*ebiten.Image
	// Seconds per frame.
	hold float64
	// Whether it repeats, and where it repeats from: the sleep animation
	// settles down over its first two frames and only then breathes in a loop.
	loop     bool
	loopFrom int
	// Played back to front. Standing up is sitting down in reverse, and one
	// sheet drawn either way is better than two sheets to keep in step.
	reverse bool
}

var sheets = map[State]struct {
	file     string
	fps      float64
	loop     bool
	loopFrom int
	reverse  bool
}{
	Idle:     {"cat/sheets/Cat_idle_1.png", 6, true, 0, false},
	Walk:     {"cat/sheets/Cat_walk_1.png", 9, true, 0, false},
	Run:      {"cat/sheets/Cat_run_1.png", 14, true, 0, false},
	Jump:     {"cat/sheets/Cat_jump_1.png", 1, false, 0, false},
	Apex:     {"cat/sheets/Cat_jump_2.png", 1, false, 0, false},
	Fall:     {"cat/sheets/Cat_fall_1.png", 1, false, 0, false},
	Spin:     {"cat/sheets/Cat_spining_1.png", 18, true, 0, false},
	Land:     {"cat/sheets/Cat_landding_1.png", 22, false, 0, false},
	Sleep:    {"cat/sheets/Cat_asleep_1.png", 4, true, 2, false},
	Cheer:    {"cat/sheets/Cat_win_cheer_1.png", 8, true, 0, false},
	Attack:   {"cat/sheets/Cat_attack_1.png", 14, false, 0, false},
	Wall:     {"cat/sheets/Cat_against_wall.png", 1, false, 0, false},
	Climb:    {"cat/sheets/Cat_ladder_1.png", 6, true, 0, false},
	Duck:     {"cat/sheets/Cat_ducking_idle_1.png", 4, true, 0, false},
	DuckWalk: {"cat/sheets/Cat_ducking_move_1.png", 8, true, 0, false},
	// Sitting down is played through once and then held, breathing slowly:
	// the crouch is a movement, the recovery is a pose. Getting up again is the
	// crouch backwards.
	Rest:   {"cat/sheets/Cat_ducking_1.png", 10, false, 0, false},
	Winded: {"cat/sheets/Cat_ducking_idle_1.png", 2, true, 0, false},
	Rise:   {"cat/sheets/Cat_ducking_1.png", 12, false, 0, true},
	Bonk:   {"cat/sheets/Cat_hit_1.png", 14, false, 0, false},
}

// follows says what a play-once animation turns into when it runs out, for the
// pairs where one movement settles into a pose.
var follows = map[State]State{
	Rest: Winded,
	// Once it is on its feet the cat is just a cat again, and the next tick
	// puts it back to whatever it was doing.
	Rise: Idle,
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

// slice cuts one sheet into its frames.
func slice(name string) ([]*ebiten.Image, error) {
	data, err := assets.ReadFile(name)
	if err != nil {
		return nil, err
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	bounds := src.Bounds()
	if bounds.Dy() != frameSize || bounds.Dx()%frameSize != 0 {
		return nil, fmt.Errorf("%s: %dx%d is not a strip of %d-pixel frames",
			name, bounds.Dx(), bounds.Dy(), frameSize)
	}
	sheet := ebiten.NewImageFromImage(src)
	var frames []*ebiten.Image
	for x := bounds.Min.X; x < bounds.Max.X; x += frameSize {
		frames = append(frames, sheet.SubImage(
			image.Rect(x, bounds.Min.Y, x+frameSize, bounds.Max.Y),
		).(*ebiten.Image))
	}
	return frames, nil
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

// What each way of getting about costs, and what standing still pays back, per
// second of it. A full gauge is about six seconds of running, which is a couple
// of lengths of a terminal window.
const (
	runCost   = 1.0 / 6
	crawlCost = 1.0 / 12
	climbCost = 1.0 / 8
	walkCost  = 1.0 / 40
	// One jump, taken when the cat leaves the ground.
	jumpCost = 1.0 / 14

	restGain  = 1.0 / 4
	sleepGain = 1.0 / 2

	// Below this the cat is flagging and slows towards a walk.
	flagging = 0.45
	// How far the gauge has to come back before a spent cat gets up again.
	rested = 0.55
)

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

// spend moves the gauge by whatever the cat just spent a tick doing.
//
// Read off the state rather than tracked separately: the state is already the
// answer to "what is it doing", and a second copy of that would be a second
// thing to keep in step.
func (c *Cat) spend(dt float64) {
	if c.hyper {
		c.sp, c.spent = 1, false
		return
	}
	switch c.state {
	case Run:
		c.sp -= runCost * dt
	case Walk:
		c.sp -= walkCost * dt
	case DuckWalk:
		c.sp -= crawlCost * dt
	case Climb:
		c.sp -= climbCost * dt
	case Sleep:
		c.sp += sleepGain * dt
	case Jump, Apex, Fall, Spin:
		// The air is free; the take-off was paid for.
	default:
		c.sp += restGain * dt
	}
	c.sp = math.Min(1, math.Max(0, c.sp))

	if c.sp <= 0 {
		c.spent = true
	} else if c.spent && c.sp >= rested {
		c.spent = false
	}
}

// vigour is how much of the difference between a walk and a run the cat still
// has in it: all of it until the gauge is down to `flagging`, and none of it at
// the bottom. This is the "runs, then slows, then stops" of the whole thing.
func (c *Cat) vigour() float64 {
	if c.sp >= flagging {
		return 1
	}
	return c.sp / flagging
}

// Stamina is the gauge, for a caller that wants to show it.
func (c *Cat) Stamina() float64 { return c.sp }

// recovering reports whether the cat is sitting one out: crouching down,
// crouched and breathing, or getting back up.
func (c *Cat) recovering() bool {
	return c.state == Rest || c.state == Winded || c.state == Rise
}

// rest is a cat that has run itself out. It sits down, gets its breath back,
// and stands up again before going anywhere.
//
// Not `setState` every tick: each of the three runs into the next on its own,
// and saying "sit down" again every tick would restart the crouch for ever.
func (c *Cat) rest() {
	c.vx = 0
	switch {
	case c.spent:
		if c.state != Rest && c.state != Winded {
			c.setState(Rest)
		}
	case c.state != Rise:
		// Enough back in the gauge: on its feet before it goes anywhere.
		c.setState(Rise)
	}
}

func (c *Cat) setState(s State) {
	if c.state == s {
		return
	}
	c.state, c.elapsed, c.frame = s, 0, 0
}

// finished reports whether a play-once animation has run out.
func (c *Cat) finished() bool {
	a := c.anims[c.state]
	return !a.loop && c.frame >= len(a.frames)-1
}

func (c *Cat) animate() {
	a := c.anims[c.state]
	if len(a.frames) == 0 {
		return
	}
	c.frame = int(c.elapsed / a.hold)
	if c.frame < len(a.frames) {
		return
	}
	if !a.loop {
		c.frame = len(a.frames) - 1
		// A movement that settles into a pose does so here rather than at the
		// place that started it, which has moved on by now.
		if next, ok := follows[c.state]; ok {
			c.setState(next)
		}
		return
	}
	span := len(a.frames) - a.loopFrom
	c.frame = a.loopFrom + (c.frame-a.loopFrom)%span
}

// frame is the sprite to draw now, and where to put it.
func (c *Cat) currentFrame() (*ebiten.Image, ebiten.GeoM, bool) {
	a := c.anims[c.state]
	if len(a.frames) == 0 {
		return nil, ebiten.GeoM{}, false
	}
	var geom ebiten.GeoM
	geom.Scale(c.scale*c.facing, c.scale)
	if c.facing < 0 {
		// Mirroring moves the sprite a width to the left; put it back.
		geom.Translate(c.height, 0)
	}
	geom.Translate(c.x-c.height/2, c.y-c.height)
	return a.frame(c.frame), geom, true
}

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

// Draw paints the cat at its feet.
func (c *Cat) Draw(dst *ebiten.Image) {
	frame, geom, ok := c.currentFrame()
	if !ok {
		return
	}
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest, GeoM: geom}
	dst.DrawImage(frame, op)

	// Over the top of it: a poke should read as coming off the animal.
	c.drawSparks(dst)
}

// Outline draws a line around the cat's own shape, one sprite pixel thick.
//
// The shape, not the frame: the sprite is a square with a good deal of air in
// it, so a box around it would be a box around nothing much. This draws the
// silhouette eight times, a pixel out in each direction, and the animal is then
// drawn over the middle of it -- which leaves exactly the pixels that have
// nothing beside them showing.
//
// A silhouette is a flat colour that keeps the sprite's alpha. Ebitengine's
// images are alpha-premultiplied, so it cannot be had by zeroing the colour and
// adding a constant -- that would tint the transparent pixels too. The colour
// matrix's alpha-to-red, alpha-to-green and alpha-to-blue terms give the
// premultiplied answer directly.
func (c *Cat) Outline(dst *ebiten.Image, clr color.Color) {
	c.drawOutline(dst, clr, 1)
}

// Glow draws a wider, translucent silhouette around a hyper cat.
func (c *Cat) Glow(dst *ebiten.Image, clr color.Color) {
	c.drawOutline(dst, clr, 3)
	c.drawOutline(dst, clr, 2)
}

func (c *Cat) drawOutline(dst *ebiten.Image, clr color.Color, spread float64) {
	frame, geom, ok := c.currentFrame()
	if !ok {
		return
	}
	r, g, b, a := clr.RGBA()
	alpha := float64(a) / 0xffff

	var cm colorm.ColorM
	cm.Scale(0, 0, 0, alpha)
	cm.SetElement(0, 3, float64(r)/0xffff*alpha)
	cm.SetElement(1, 3, float64(g)/0xffff*alpha)
	cm.SetElement(2, 3, float64(b)/0xffff*alpha)

	// One sprite pixel, which is what the cat is drawn in.
	step := c.scale
	for _, at := range [8][2]float64{
		{-1, -1}, {0, -1}, {1, -1},
		{-1, 0}, {1, 0},
		{-1, 1}, {0, 1}, {1, 1},
	} {
		op := &colorm.DrawImageOptions{Filter: ebiten.FilterNearest, GeoM: geom}
		op.GeoM.Translate(at[0]*step*spread, at[1]*step*spread)
		colorm.DrawImage(dst, frame, cm, op)
	}
}

// Feet is where the cat is standing, for a caller that needs to know.
func (c *Cat) Feet() (x, y float64) { return c.x, c.y }

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
