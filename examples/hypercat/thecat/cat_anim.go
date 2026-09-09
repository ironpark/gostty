// Sprite sheets and the animation state machine: which pictures a state is
// drawn from, how fast they play, and what a play-once animation settles into.
package thecat

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	"github.com/hajimehoshi/ebiten/v2"
)

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
