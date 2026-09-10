package frontend

import (
	"io/fs"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/input"
)

type Button struct{ Down, Pressed, Released bool }

// Input is the platform state for one update. Key events are read lazily:
// tab routing first decides focus duration and whether a panel owns typing.
// ReadKeys must be consumed during this Update; its event buffer is borrowed.
type Input struct {
	Mods                keys.Mods
	Focused             bool
	X, Y                int
	Left, Right, Middle Button
	Wheel               float64
	Dropped             fs.FS
	DeltaSeconds        float64
	PressedKeys         map[input.Key]bool
	Panel               ui.Input
	ReadKeys            func(focusedFrames int) []keys.Event
}

func (in Input) KeyPressed(key input.Key) bool { return in.PressedKeys[key] }

func (in Input) KeyEvents(focusedFrames int) []keys.Event {
	if in.ReadKeys == nil {
		return nil
	}
	return in.ReadKeys(focusedFrames)
}

type inputReader struct {
	keyboard keys.Reader
	chars    []rune
	pressed  map[input.Key]bool
	mods     keys.Mods
}

// These keys are host shortcuts and panel navigation. Terminal typing uses
// keys.Reader, which retains complete Kitty keyboard events and IME text.
var hostKeys = map[input.Key]ebiten.Key{
	input.KeyKeyT: ebiten.KeyT, input.KeyKeyW: ebiten.KeyW,
	input.KeyTab:    ebiten.KeyTab,
	input.KeyDigit1: ebiten.KeyDigit1, input.KeyDigit2: ebiten.KeyDigit2,
	input.KeyDigit3: ebiten.KeyDigit3, input.KeyDigit4: ebiten.KeyDigit4,
	input.KeyDigit5: ebiten.KeyDigit5, input.KeyDigit6: ebiten.KeyDigit6,
	input.KeyDigit7: ebiten.KeyDigit7, input.KeyDigit8: ebiten.KeyDigit8,
	input.KeyDigit9: ebiten.KeyDigit9,
}

func (r *inputReader) read() Input {
	r.mods = keys.Current()
	if r.pressed == nil {
		r.pressed = make(map[input.Key]bool)
	}
	clear(r.pressed)
	for key, native := range hostKeys {
		if inpututil.IsKeyJustPressed(native) {
			r.pressed[key] = true
		}
	}
	r.chars = ebiten.AppendInputChars(r.chars[:0])
	x, y := ebiten.CursorPosition()
	_, wheel := ebiten.Wheel()
	button := func(b ebiten.MouseButton) Button {
		return Button{ebiten.IsMouseButtonPressed(b), inpututil.IsMouseButtonJustPressed(b), inpututil.IsMouseButtonJustReleased(b)}
	}
	return Input{
		Mods: r.mods, Focused: ebiten.IsFocused(), X: x, Y: y,
		Left: button(ebiten.MouseButtonLeft), Right: button(ebiten.MouseButtonRight), Middle: button(ebiten.MouseButtonMiddle),
		Wheel: wheel, Dropped: ebiten.DroppedFiles(), DeltaSeconds: 1.0 / float64(ebiten.TPS()),
		PressedKeys: r.pressed, ReadKeys: r.readKeys,
		Panel: ui.Input{
			OpenSearch:   r.mods.Shortcut() && inpututil.IsKeyJustPressed(ebiten.KeyF),
			OpenSettings: r.mods.Shortcut() && inpututil.IsKeyJustPressed(ebiten.KeyComma),
			Close:        inpututil.IsKeyJustPressed(ebiten.KeyEscape),
			Enter:        inpututil.IsKeyJustPressed(ebiten.KeyEnter), Shift: r.mods.Shift,
			Chars:     r.chars,
			Backspace: keys.Repeating(inpututil.KeyPressDuration(ebiten.KeyBackspace)),
			Up:        inpututil.IsKeyJustPressed(ebiten.KeyArrowUp), Down: inpututil.IsKeyJustPressed(ebiten.KeyArrowDown),
			Left:  inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft) || inpututil.IsKeyJustPressed(ebiten.KeyMinus),
			Right: inpututil.IsKeyJustPressed(ebiten.KeyArrowRight) || inpututil.IsKeyJustPressed(ebiten.KeyEqual),
		},
	}
}

func (r *inputReader) readKeys(focusedFrames int) []keys.Event {
	return r.keyboard.Frame(r.mods, focusedFrames)
}
