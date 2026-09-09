// Stamina. Running, climbing and jumping spend it; standing about and sleeping
// put it back, and a cat that runs out has to sit down.
package thecat

import "math"

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
