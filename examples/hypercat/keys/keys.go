// Package keys reads a frame of keyboard state and says which terminal keys
// were pressed.
//
// It is the platform half of hypercat's input: Ebitengine's key set, the OS
// call that says whether a key is really down, and the repeat policy an
// emulator has to invent because Ebitengine reports how long a key has been
// held rather than repeats. What to do with a press -- encode it, write it to
// the pty -- is the program's business and stays there.
package keys

import (
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty/input"
)

// Key repeat, in ticks, since Ebitengine reports how long a key has been held
// rather than repeats. The default tick rate is 60Hz, so this is 0.4s then 30
// times a second.
const (
	repeatDelayTicks    = 24
	repeatIntervalTicks = 2
)

// Mods is the modifier state shared by the keyboard and the mouse.
type Mods struct{ Shift, Ctrl, Alt, Super bool }

// KeyMods is the same state in the form the encoders take.
func (m Mods) KeyMods() input.KeyMods {
	return input.KeyMods{Shift: m.Shift, Ctrl: m.Ctrl, Alt: m.Alt, Super: m.Super}
}

// Any reports whether any modifier that suppresses text is held. Shift is not
// one of them: it is already in the rune the platform produced.
func (m Mods) Any() bool { return m.Ctrl || m.Alt || m.Super }

// Shortcut reports whether the emulator's own bindings are being held, rather
// than something meant for the program: Ctrl+Shift, or Cmd where that is the
// convention. Everything hypercat keeps for itself is behind this.
func (m Mods) Shortcut() bool { return (m.Ctrl && m.Shift) || m.Super }

// Repeating reports whether a key held for this many ticks should fire now.
func Repeating(held int) bool {
	if held == 1 {
		return true
	}
	if held <= repeatDelayTicks {
		return false
	}
	return (held-repeatDelayTicks)%repeatIntervalTicks == 0
}

// An Event is one key press to describe to the encoder. Text is what the
// platform produced for it -- already through the layout, dead keys and the
// IME -- and is empty for a key that produces none. It stays valid until the
// next call to Frame.
type Event struct {
	Key  input.Key
	Text []byte

	off, length int
}

// A Reader turns each frame of keyboard state into events. Its scratch buffers
// are reused, so a frame of typing allocates nothing after the first.
type Reader struct {
	chars   []rune
	pressed []ebiten.Key
	text    []byte
	events  []Event
}

// Frame reports this frame's presses, in the order they should be sent.
//
// maxHeld drops keys that were already down when the window took focus: their
// press belongs to whatever had focus before, and Ebitengine goes on reporting
// them as held.
func (r *Reader) Frame(m Mods, maxHeld int) []Event {
	r.events, r.text = r.events[:0], r.text[:0]

	// Text first: a rune the platform produced already accounts for the
	// layout, dead keys and the IME. Only when a modifier makes it text-less
	// do we fall back to describing the physical key.
	if !m.Any() {
		r.chars = ebiten.AppendInputChars(r.chars[:0])
		for _, c := range r.chars {
			off := len(r.text)
			r.text = utf8.AppendRune(r.text, c)
			r.events = append(r.events, Event{
				Key:    input.KeyUnidentified,
				off:    off,
				length: len(r.text) - off,
			})
		}
	}

	r.pressed = inpututil.AppendPressedKeys(r.pressed[:0])
	for _, key := range r.pressed {
		if !PhysicallyPressed(key) {
			continue
		}
		held := inpututil.KeyPressDuration(key)
		if held > maxHeld {
			continue
		}
		if code, ok := nonTextKeys[key]; ok {
			if Repeating(held) {
				r.events = append(r.events, Event{Key: code})
			}
			continue
		}
		// Letters and digits produce text on their own, so they are only
		// described as keys when a modifier swallowed the text. They do not
		// repeat: Ctrl+C held down should interrupt once.
		if held == 1 && m.Any() {
			if code, ok := textKeys[key]; ok {
				r.events = append(r.events, Event{Key: code})
			}
		}
	}

	// The text buffer has stopped growing, so the offsets recorded above can
	// become the slices callers read. Doing it as the events were appended
	// would hand out slices an append could move.
	for i := range r.events {
		e := &r.events[i]
		e.Text = r.text[e.off : e.off+e.length]
	}
	return r.events
}

// Keys that do not produce text, and so must be described to the encoder.
var nonTextKeys = map[ebiten.Key]input.Key{
	ebiten.KeyArrowUp:    input.KeyArrowUp,
	ebiten.KeyArrowDown:  input.KeyArrowDown,
	ebiten.KeyArrowLeft:  input.KeyArrowLeft,
	ebiten.KeyArrowRight: input.KeyArrowRight,
	ebiten.KeyEnter:      input.KeyEnter,
	ebiten.KeyBackspace:  input.KeyBackspace,
	ebiten.KeyTab:        input.KeyTab,
	ebiten.KeyEscape:     input.KeyEscape,
	ebiten.KeyDelete:     input.KeyDelete,
	ebiten.KeyInsert:     input.KeyInsert,
	ebiten.KeyHome:       input.KeyHome,
	ebiten.KeyEnd:        input.KeyEnd,
	ebiten.KeyPageUp:     input.KeyPageUp,
	ebiten.KeyPageDown:   input.KeyPageDown,
	ebiten.KeyF1:         input.KeyF1,
	ebiten.KeyF2:         input.KeyF2,
	ebiten.KeyF3:         input.KeyF3,
	ebiten.KeyF4:         input.KeyF4,
	ebiten.KeyF5:         input.KeyF5,
	ebiten.KeyF6:         input.KeyF6,
	ebiten.KeyF7:         input.KeyF7,
	ebiten.KeyF8:         input.KeyF8,
	ebiten.KeyF9:         input.KeyF9,
	ebiten.KeyF10:        input.KeyF10,
	ebiten.KeyF11:        input.KeyF11,
	ebiten.KeyF12:        input.KeyF12,
}

// Keys that normally produce text, described only when a modifier is held.
var textKeys = map[ebiten.Key]input.Key{
	ebiten.KeyA: input.KeyKeyA, ebiten.KeyB: input.KeyKeyB, ebiten.KeyC: input.KeyKeyC,
	ebiten.KeyD: input.KeyKeyD, ebiten.KeyE: input.KeyKeyE, ebiten.KeyF: input.KeyKeyF,
	ebiten.KeyG: input.KeyKeyG, ebiten.KeyH: input.KeyKeyH, ebiten.KeyI: input.KeyKeyI,
	ebiten.KeyJ: input.KeyKeyJ, ebiten.KeyK: input.KeyKeyK, ebiten.KeyL: input.KeyKeyL,
	ebiten.KeyM: input.KeyKeyM, ebiten.KeyN: input.KeyKeyN, ebiten.KeyO: input.KeyKeyO,
	ebiten.KeyP: input.KeyKeyP, ebiten.KeyQ: input.KeyKeyQ, ebiten.KeyR: input.KeyKeyR,
	ebiten.KeyS: input.KeyKeyS, ebiten.KeyT: input.KeyKeyT, ebiten.KeyU: input.KeyKeyU,
	ebiten.KeyV: input.KeyKeyV, ebiten.KeyW: input.KeyKeyW, ebiten.KeyX: input.KeyKeyX,
	ebiten.KeyY: input.KeyKeyY, ebiten.KeyZ: input.KeyKeyZ,
	ebiten.KeyDigit0: input.KeyDigit0, ebiten.KeyDigit1: input.KeyDigit1,
	ebiten.KeyDigit2: input.KeyDigit2, ebiten.KeyDigit3: input.KeyDigit3,
	ebiten.KeyDigit4: input.KeyDigit4, ebiten.KeyDigit5: input.KeyDigit5,
	ebiten.KeyDigit6: input.KeyDigit6, ebiten.KeyDigit7: input.KeyDigit7,
	ebiten.KeyDigit8: input.KeyDigit8, ebiten.KeyDigit9: input.KeyDigit9,
	ebiten.KeySpace: input.KeySpace, ebiten.KeyMinus: input.KeyMinus,
	ebiten.KeyEqual: input.KeyEqual, ebiten.KeySlash: input.KeySlash,
	ebiten.KeyBackslash: input.KeyBackslash, ebiten.KeyComma: input.KeyComma,
	ebiten.KeyPeriod: input.KeyPeriod, ebiten.KeySemicolon: input.KeySemicolon,
	ebiten.KeyQuote: input.KeyQuote, ebiten.KeyBackquote: input.KeyBackquote,
	ebiten.KeyBracketLeft: input.KeyBracketLeft, ebiten.KeyBracketRight: input.KeyBracketRight,
}
