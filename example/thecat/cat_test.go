package thecat

import (
	"image/color"
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/colorm"
)

func TestSheetsLoad(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for state, sheet := range sheets {
		a := c.anims[state]
		if len(a.frames) == 0 {
			t.Errorf("%v (%s) has no frames", state, sheet.file)
		}
		if a.loopFrom >= len(a.frames) {
			t.Errorf("%v loops from frame %d of %d", state, a.loopFrom, len(a.frames))
		}
	}
}

// world is a floor plus a set of ledges, in the same shape the terminal gives
// the cat: a column has a surface at a y, or it does not.
type world struct {
	w, h    float64
	shelves []shelf
	target  *[2]float64
}

type shelf struct {
	x0, x1, y float64
	// How deep it is. Zero means one row's worth.
	depth float64
}

func (l shelf) thickness() float64 {
	if l.depth > 0 {
		return l.depth
	}
	return thick
}

func (w *world) GroundBelow(x, from float64) (float64, bool) {
	best := w.h
	for _, l := range w.shelves {
		if x >= l.x0 && x < l.x1 && l.y >= from && l.y < best {
			best = l.y
		}
	}
	return best, true
}

// A ledge is `thick` deep, the way a line of text is one row deep: solid from
// the side, with nothing above it.
const thick = 20.0

func (w *world) Solid(x, y float64) bool {
	for _, l := range w.shelves {
		if x >= l.x0 && x < l.x1 && y >= l.y && y < l.y+l.thickness() {
			return true
		}
	}
	return false
}

func (w *world) Bounds() (float64, float64) { return w.w, w.h }

func (w *world) Attention() (float64, float64, bool) {
	if w.target == nil {
		return 0, 0, false
	}
	return w.target[0], w.target[1], true
}

// run ticks the cat for a number of seconds at 60Hz.
func run(c *Cat, w World, seconds float64) {
	const dt = 1.0 / 60
	for range int(seconds / dt) {
		c.Update(w, dt)
	}
}

// A cat dropped in the air falls to the floor and stays on it.
func TestFallsToTheFloor(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{w: 800, h: 400}
	c.Place(400, 50)
	run(c, w, 3)
	if c.y != w.h {
		t.Errorf("feet at %v, want the floor at %v", c.y, w.h)
	}
	if c.mode != onGround {
		t.Error("the cat is on the floor but does not think it has landed")
	}
}

// It goes to what it is looking at, and stops when it gets there.
func TestWalksToTheTarget(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{w: 800, h: 400, target: &[2]float64{700, 400}}
	c.Place(100, 400)
	// It runs while the target is far, walks the last stretch, and may have to
	// stop for a breather on the way, so this is a good deal longer than the
	// distance alone suggests.
	run(c, w, 25)
	if math.Abs(c.x-700) > arrived*c.height {
		t.Errorf("stopped at %v, want about 700", c.x)
	}
	if c.mode != onGround {
		t.Error("it should have walked there, not fallen off the world")
	}
	// And it is facing the way it went.
	if c.facing < 0 {
		t.Errorf("facing = %v, want rightwards after walking right", c.facing)
	}
}

// A line of text one step up is stepped onto, without leaving the ground: this
// is what walking along the output looks like.
func TestStepsUpOntoALedge(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	step := 32 * stepUp * 0.8
	w := &world{
		w: 800, h: 400,
		shelves: []shelf{{x0: 400, x1: 800, y: 400 - step}},
		target:  &[2]float64{700, 400 - step},
	}
	c.Place(100, 400)
	run(c, w, 10)
	if c.y != 400-step {
		t.Errorf("feet at %v, want the ledge at %v", c.y, 400-step)
	}
}

// Walking off the end of a line drops the cat onto whatever is under it.
func TestFallsOffTheEnd(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	// A ledge high above the floor, and something to chase past the end of it.
	w := &world{
		w: 800, h: 400,
		shelves: []shelf{{x0: 0, x1: 300, y: 200}},
		target:  &[2]float64{700, 400},
	}
	c.Place(100, 200)
	run(c, w, 1)
	if c.y != 200 {
		t.Fatalf("feet at %v before the edge, want the ledge at 200", c.y)
	}
	run(c, w, 5)
	if c.y != 400 {
		t.Errorf("feet at %v, want the floor at 400 after walking off", c.y)
	}
}

// With nothing to chase, the cat wanders for a while and then sleeps.
func TestSleepsWhenBored(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{w: 800, h: 400}
	c.Place(400, 400)
	run(c, w, 2)
	if c.state == Sleep {
		t.Error("asleep after two seconds; it should still be wandering")
	}
	run(c, w, boredom+4)
	if c.state != Sleep {
		t.Errorf("state = %v after %v seconds of nothing, want asleep", c.state, boredom+6)
	}
}

// The cat stays in the window however hard it chases something outside it.
func TestStaysInBounds(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{w: 800, h: 400, target: &[2]float64{5000, 400}}
	c.Place(400, 400)
	run(c, w, 10)
	if c.x < c.height/2 || c.x > w.w-c.height/2 {
		t.Errorf("x = %v, want within [%v, %v]", c.x, c.height/2, w.w-c.height/2)
	}
	w.target = &[2]float64{-5000, 400}
	run(c, w, 10)
	if c.x < c.height/2 {
		t.Errorf("x = %v, want at least %v", c.x, c.height/2)
	}
}

// counts how many times the cat left the ground over a run.
func countJumps(c *Cat, w World, seconds float64) int {
	const dt = 1.0 / 60
	jumps, was := 0, c.mode
	for range int(seconds / dt) {
		c.Update(w, dt)
		if was == onGround && c.mode == inAir && c.vy < 0 {
			jumps++
		}
		was = c.mode
	}
	return jumps
}

// The pointer is nearly always somewhere up the screen, and on flat ground
// there is nothing to jump onto, so the cat should not leave the ground at all.
// Jumping at anything above it is what turned it into a pogo stick.
func TestDoesNotJumpAtNothing(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	// Flat floor, pointer high above it.
	w := &world{w: 800, h: 400, target: &[2]float64{400, 20}}
	c.Place(400, 400)
	run(c, w, 1)
	if n := countJumps(c, w, 8); n != 0 {
		t.Errorf("jumped %d times at a pointer it cannot reach; want none", n)
	}
}

// A jump carries exactly as far up as the ledge it is for, so stepping onto a
// line of text is a hop rather than a leap over the screen.
func TestJumpHeightMatchesTheLedge(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	// A ledge just past what can be walked up, and a target on top of it.
	rise := 32 * 0.9
	w := &world{
		w: 800, h: 400,
		shelves: []shelf{{x0: 300, x1: 800, y: 400 - rise}},
		target:  &[2]float64{600, 400 - rise},
	}
	c.Place(100, 400)

	const dt = 1.0 / 60
	peak := 400.0
	for range int(12 / dt) {
		c.Update(w, dt)
		peak = math.Min(peak, c.y)
	}
	if c.y != 400-rise {
		t.Errorf("feet at %v, want the ledge at %v", c.y, 400-rise)
	}
	// It cleared the ledge, but not by more than the clearance it aims for.
	over := (400 - rise) - peak
	if over < 0 {
		t.Errorf("peaked at %v, which is below the ledge at %v", peak, 400-rise)
	}
	if over > 2*clearance*c.height {
		t.Errorf("cleared the ledge by %v, want no more than %v", over, 2*clearance*c.height)
	}
}

// A wall taller than a jump, with something above it, is climbed.
func TestClimbsATallWall(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	// A block four cat-heights tall, like a paragraph of text, with the
	// pointer resting on top of it.
	top := 400.0 - 4*32
	w := &world{
		w: 800, h: 400,
		shelves: []shelf{{x0: 400, x1: 800, y: top, depth: 4 * 32}},
		target:  &[2]float64{600, top},
	}
	c.Place(100, 400)
	run(c, w, 20)
	if c.y > top+1 {
		t.Errorf("feet at %v, want the top of the wall at %v", c.y, top)
	}
	if c.x < 400 {
		t.Errorf("x = %v, want past the wall's edge at 400", c.x)
	}
}

// A wall it cannot climb and has no reason to climb is leaned on, and then the
// cat turns round rather than standing there for ever.
func TestTurnsAtAWallItCannotUse(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	top := 400.0 - 4*32
	// The pointer is on the near side of the wall and level with the cat, so
	// there is nothing up there worth climbing for.
	w := &world{
		w: 800, h: 400,
		shelves: []shelf{{x0: 400, x1: 800, y: top, depth: 4 * 32}},
		target:  &[2]float64{700, 400},
	}
	c.Place(100, 400)
	run(c, w, 6)
	if c.state != Wall && c.facing > 0 {
		t.Errorf("state = %v facing = %v; want it leaning on the wall or having turned back",
			c.state, c.facing)
	}
	run(c, w, 4)
	if c.y != 400 {
		t.Errorf("feet at %v, want still on the floor at 400", c.y)
	}
}

// Something directly overhead means crawling, not walking, and definitely not
// jumping into it.
func TestDucksUnderACeiling(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	// A ceiling too low to walk under, over the middle of the world, as thin
	// as a line of text.
	w := &world{
		w: 800, h: 400,
		shelves: []shelf{{x0: 200, x1: 600, y: 400 - 0.8*32, depth: 8}},
		target:  &[2]float64{500, 400},
	}
	c.Place(100, 400)
	run(c, w, 10)
	if c.state != DuckWalk && c.state != Duck {
		t.Errorf("state = %v under a ceiling, want it ducking", c.state)
	}
	if c.y != 400 {
		t.Errorf("feet at %v, want the floor at 400", c.y)
	}
}

// Running empties the gauge, and an empty gauge stops the cat: it sits down
// where it is until it has enough back to go on.
func TestRunningTiresItOut(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	// Something to chase that it can never reach, so it runs and runs.
	w := &world{w: 4000, h: 400, target: &[2]float64{3990, 400}}
	c.Place(20, 400)

	const dt = 1.0 / 60
	rested := false
	for range int(30 / dt) {
		c.Update(w, dt)
		if c.recovering() {
			rested = true
			break
		}
	}
	if !rested {
		t.Fatalf("still going after 30 seconds with %.2f in the gauge; it never tires", c.Stamina())
	}
	if c.Stamina() > 0.01 {
		t.Errorf("sat down with %.2f left; it should be spent", c.Stamina())
	}

	// And it gets up again once it has its breath back. Checked by watching
	// for it rather than by looking at the state after a fixed while: a cat
	// that has got up runs itself out again, so a snapshot taken at any one
	// moment could as easily catch the next sit-down.
	before := c.x
	const dt2 = 1.0 / 60
	got := map[State]bool{}
	for range int(12 / dt2) {
		c.Update(w, dt2)
		got[c.state] = true
	}
	if !got[Rise] {
		t.Error("it never stood up again")
	}
	if !got[Walk] && !got[Run] {
		t.Error("it never set off again")
	}
	if c.x <= before {
		t.Errorf("x = %v, want past %v: it made no progress", c.x, before)
	}
}

// It does not stutter: once it has sat down it stays down long enough to be
// worth it, rather than getting up the moment the gauge leaves zero.
func TestRestIsNotAStutter(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{w: 4000, h: 400, target: &[2]float64{3990, 400}}
	c.Place(20, 400)

	const dt = 1.0 / 60
	// Run it down.
	for range int(30 / dt) {
		if c.Update(w, dt); c.recovering() {
			break
		}
	}
	if !c.recovering() {
		t.Fatal("never sat down")
	}
	// It should be resting for a good second or two, not a frame.
	ticks := 0
	for range int(10 / dt) {
		c.Update(w, dt)
		if !c.recovering() {
			break
		}
		ticks++
	}
	if seconds := float64(ticks) * dt; seconds < 1 {
		t.Errorf("rested for %.2fs; that is a stutter, not a rest", seconds)
	}
	if c.Stamina() < rested-0.05 {
		t.Errorf("got up with %.2f in the gauge, want about %v", c.Stamina(), rested)
	}
}

// A tiring cat slows down before it stops, which is what the gauge is for:
// the same chase is slower at the end of it than at the start.
func TestItSlowsAsItTires(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{w: 4000, h: 400, target: &[2]float64{3990, 400}}
	c.Place(20, 400)

	const dt = 1.0 / 60
	speedAt := func(sp float64) float64 {
		for range int(60 / dt) {
			c.Update(w, dt)
			if c.Stamina() <= sp {
				break
			}
		}
		return math.Abs(c.vx)
	}
	fast := speedAt(0.9)
	slow := speedAt(0.15)
	if slow >= fast {
		t.Errorf("running at %v with a nearly empty gauge and %v with a full one; it should tire",
			slow, fast)
	}
	// But it is still moving, not stopped dead.
	if slow <= 0 {
		t.Errorf("speed = %v before the gauge ran out, want it still going", slow)
	}
}

// Sleeping is the better way to get a gauge back, so a cat left alone recovers
// faster than one standing about.
func TestSleepRecoversFaster(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing to chase, so it wanders, tires, and eventually sleeps.
	w := &world{w: 400, h: 400}
	c.Place(200, 400)
	c.sp = 0.2

	run(c, w, boredom+6)
	if c.state != Sleep {
		t.Fatalf("state = %v, want asleep with nothing to do", c.state)
	}
	if c.Stamina() < 0.9 {
		t.Errorf("gauge = %.2f after a nap, want it nearly full", c.Stamina())
	}
}

// Jumping costs something, so a cat that has just cleared a ledge has less in
// the gauge than one that walked the same way.
func TestJumpingCosts(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	rise := 32 * 0.9
	w := &world{
		w: 800, h: 400,
		shelves: []shelf{{x0: 300, x1: 800, y: 400 - rise}},
		target:  &[2]float64{600, 400 - rise},
	}
	c.Place(280, 400)
	before := c.Stamina()

	const dt = 1.0 / 60
	for range int(3 / dt) {
		c.Update(w, dt)
		if c.mode == inAir && c.vy < 0 {
			break
		}
	}
	if c.mode != inAir {
		t.Fatal("it never jumped")
	}
	if c.Stamina() >= before {
		t.Errorf("gauge = %.2f after a jump, want less than %.2f", c.Stamina(), before)
	}
}

// The pointer is over the cat when it is over the animal, not merely over the
// square the animal is drawn in: the sprite has a good deal of air around it,
// and a click a body's width away should not count as a poke.
func TestHitBoxIsTheAnimal(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	c.Place(100, 200)
	x, y, w, h := c.Box()
	if x != 100-c.height/2 || y != 200-c.height || w != c.height || h != c.height {
		t.Fatalf("box = (%v,%v) %vx%v, want the sprite around the feet at (100,200)", x, y, w, h)
	}

	for _, at := range []struct {
		name string
		x, y float64
		want bool
	}{
		{"the middle of it", 100, 200 - c.height/2, true},
		{"just above the feet", 100, 200 - 2, true},
		{"the far corner of the frame", x + 1, y + 1, false},
		{"a body to the left", 100 - c.height, 200 - c.height/2, false},
		{"below the feet", 100, 200 + 5, false},
		{"over its head", 100, y - 5, false},
	} {
		if got := c.Hit(at.x, at.y); got != at.want {
			t.Errorf("Hit(%v) at %s = %v, want %v", [2]float64{at.x, at.y}, at.name, got, at.want)
		}
	}
}

// A poke throws sparks, which rise, fall and go out on their own.
func TestPokeMakesSparksThatFade(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{w: 800, h: 400}
	c.Place(400, 400)
	run(c, w, 1)

	if c.Sparkling() {
		t.Fatal("sparking before anything poked it")
	}
	c.Poke()
	if !c.Sparkling() {
		t.Fatal("a poke made no sparks")
	}
	if n := len(c.sparks); n != sparkCount {
		t.Errorf("%d sparks, want %d", n, sparkCount)
	}
	// They start on the animal rather than at the corner of its frame.
	for _, s := range c.sparks {
		if math.Abs(s.x-c.x) > c.height/2 || math.Abs(s.y-(c.y-c.height/2)) > c.height/2 {
			t.Errorf("a spark started at (%v,%v), which is off the cat at (%v,%v)",
				s.x, s.y, c.x, c.y)
		}
	}

	// And they are gone a moment later, without being tidied up by hand.
	run(c, w, sparkLife+0.5)
	if c.Sparkling() {
		t.Errorf("%d sparks still going %v after the poke", len(c.sparks), sparkLife)
	}
}

func TestHyperCatNeverTiresAndLeavesAfterimages(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	target := [2]float64{700, 400}
	w := &world{w: 800, h: 400, target: &target}
	c.Place(100, 400)
	c.sp, c.spent = 0, true
	c.SetHyper(true)

	if !c.Hyper() || c.Stamina() != 1 || c.spent {
		t.Fatalf("hyper mode did not restore the cat: hyper=%v stamina=%v spent=%v", c.Hyper(), c.Stamina(), c.spent)
	}
	run(c, w, 0.5)
	if c.Stamina() != 1 || c.spent {
		t.Fatalf("hyper cat tired while running: stamina=%v spent=%v", c.Stamina(), c.spent)
	}
	if len(c.trail) == 0 {
		t.Fatal("moving hyper cat left no afterimages")
	}

	before := len(c.sparks)
	c.Poke()
	if made := len(c.sparks) - before; made != sparkCount*2 {
		t.Fatalf("hyper poke made %d sparks, want %d", made, sparkCount*2)
	}
	c.SetHyper(false)
	if c.Hyper() || len(c.trail) != 0 {
		t.Fatal("turning hyper mode off did not clear its trail")
	}
}

// Being poked is also being noticed: it wakes a sleeping cat and stops it
// sulking about a wall.
func TestPokeGetsItsAttention(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{w: 800, h: 400}
	c.Place(400, 400)
	run(c, w, boredom+3)
	if c.state != Sleep {
		t.Fatalf("state = %v, want it asleep with nothing to do", c.state)
	}

	c.Poke()
	if c.bored != 0 {
		t.Errorf("bored = %v after a poke, want it wide awake", c.bored)
	}
	run(c, w, 0.2)
	if c.state == Sleep {
		t.Error("still asleep after being prodded")
	}
}

// Sitting down is a movement that settles into a pose: the crouch plays once
// and then the cat breathes slowly, rather than crouching over and over.
func TestRestSettlesIntoAPose(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{w: 4000, h: 400, target: &[2]float64{3990, 400}}
	c.Place(20, 400)

	const dt = 1.0 / 60
	for range int(30 / dt) {
		if c.Update(w, dt); c.recovering() {
			break
		}
	}
	if c.state != Rest {
		t.Fatalf("state = %v, want the crouch first", c.state)
	}

	// The crouch runs to its last frame and then hands over.
	crouch := c.anims[Rest]
	if crouch.loop {
		t.Error("the crouch loops; it should play once")
	}
	for range int(2 / dt) {
		c.Update(w, dt)
		if c.state == Winded {
			break
		}
	}
	if c.state != Winded {
		t.Fatalf("state = %v after the crouch, want it settled into the pose", c.state)
	}
	if !c.anims[Winded].loop {
		t.Error("the pose does not loop; it should breathe")
	}
	// And it stays there, rather than crouching again.
	for range int(3 / dt) {
		c.Update(w, dt)
		if c.state == Rest {
			t.Fatal("it crouched a second time instead of holding the pose")
		}
		if !c.recovering() {
			break
		}
	}
}

// The pose is slower than the movement into it: a cat getting its breath back
// is not animated at walking pace.
func TestRestPoseIsSlow(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	if c.anims[Winded].hold <= c.anims[Rest].hold {
		t.Errorf("the pose holds each frame for %vs and the crouch for %vs; the pose should be slower",
			c.anims[Winded].hold, c.anims[Rest].hold)
	}
}

// The outline is the cat's own shape, so it is drawn from the sprite's alpha.
// Ebitengine's images are alpha-premultiplied, which is why the flat colour has
// to come out of the matrix's alpha terms: getting that wrong tints every
// transparent pixel of the frame and the outline becomes a filled box.
func TestOutlineColourKeepsTransparency(t *testing.T) {
	var cm colorm.ColorM
	cm.Scale(0, 0, 0, 1)
	cm.SetElement(0, 3, 1)
	cm.SetElement(1, 3, 0.5)
	cm.SetElement(2, 3, 0)

	// An opaque pixel of any colour comes out as the outline colour.
	r, g, b, a := cm.Apply(color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}).RGBA()
	if a != 0xffff {
		t.Errorf("alpha = %v, want it kept at %v", a, 0xffff)
	}
	if r != 0xffff || b != 0 {
		t.Errorf("colour = (%v,%v,%v), want the outline's red", r, g, b)
	}
	// A transparent one stays transparent, and colourless with it.
	r, g, b, a = cm.Apply(color.RGBA{}).RGBA()
	if r|g|b|a != 0 {
		t.Errorf("a transparent pixel came out as (%v,%v,%v,%v), want it invisible", r, g, b, a)
	}
}

// Getting up is sitting down backwards: the same sheet, read the other way, so
// there is one crouch to keep looking right rather than two.
func TestRiseIsTheCrouchReversed(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	down, up := c.anims[Rest], c.anims[Rise]
	if len(down.frames) != len(up.frames) || len(down.frames) == 0 {
		t.Fatalf("crouch has %d frames and standing up %d", len(down.frames), len(up.frames))
	}
	if !up.reverse {
		t.Fatal("standing up is not marked as reversed, so it would play as another crouch")
	}
	// Frame for frame, one is the other backwards.
	last := len(down.frames) - 1
	for i := range down.frames {
		if up.frame(i) != down.frame(last-i) {
			t.Errorf("frame %d of standing up is not frame %d of the crouch", i, last-i)
		}
	}
	// It plays once and hands back to an ordinary cat.
	if up.loop {
		t.Error("standing up loops; it should play once")
	}
	if follows[Rise] != Idle {
		t.Errorf("standing up runs into %v, want an ordinary idle", follows[Rise])
	}
}

// The whole sequence, in order: run out, sit down, breathe, stand up, go.
func TestRestSequence(t *testing.T) {
	c, err := New(32)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{w: 4000, h: 400, target: &[2]float64{3990, 400}}
	c.Place(20, 400)

	const dt = 1.0 / 60
	var order []State
	for range int(40 / dt) {
		c.Update(w, dt)
		if n := len(order); n == 0 || order[n-1] != c.state {
			order = append(order, c.state)
		}
	}

	// Find the sit-down and check what follows it.
	want := []State{Rest, Winded, Rise}
	for i := range order {
		if order[i] != Rest {
			continue
		}
		if i+len(want) > len(order) {
			break
		}
		if order[i+1] == want[1] && order[i+2] == want[2] {
			return // crouch, breathe, up: in that order and nothing between.
		}
		t.Fatalf("after sitting down it went %v, want %v", order[i:min(i+3, len(order))], want)
	}
	t.Fatalf("it never sat down; it went %v", order)
}
