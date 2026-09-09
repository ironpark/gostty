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
// rather than repeats. The default tick rate is 60Hz, so this is 0.4s and then
// ten times a second.
//
// Invented, and so worth matching to what the platform would have done. Only
// the keys that produce no text need it: a held letter arrives as repeated
// characters from the window system, at whatever rate the user configured, and
// a key with no character has to be repeated here or not at all. The two
// halves of the keyboard repeating at visibly different rates is what a rate
// picked without reference to the platform looks like -- and on a key that
// commits a line, a rate three times too fast is a screen of prompts from a
// keypress that felt momentary.
//
// macOS defaults to 375ms and about eleven a second (InitialKeyRepeat 25,
// KeyRepeat 6, both in 15ms units), and the common Linux and Windows defaults
// land near enough to the same place.
const (
	repeatDelayTicks    = 24
	repeatIntervalTicks = 6
)

// Mods is the modifier state shared by the keyboard and the mouse.
type Mods struct{ Shift, Ctrl, Alt, Super bool }

// KeyMods is the same state in the form the encoders take.
func (m Mods) KeyMods() input.KeyMods {
	return input.KeyMods{Shift: m.Shift, Ctrl: m.Ctrl, Alt: m.Alt, Super: m.Super}
}

// Consumed is the modifiers the platform used up producing this event's text,
// which the encoder needs to tell "shift made a capital A" from "shift is a
// modifier the program should be told about".
//
// Text is the evidence: a rune arrived, so whatever it took to make it was
// spent on it. Ctrl and Super never make text on any platform this runs on,
// and Alt only where the layout says so.
func (m Mods) Consumed(hasText bool) input.KeyMods {
	if !hasText {
		return input.KeyMods{}
	}
	return input.KeyMods{Shift: m.Shift, Alt: m.Alt && altProducesText}
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

// An Event is one key event to describe to the encoder. Text is what the
// platform produced for it -- already through the layout, dead keys and the
// IME -- and is empty for a key that produces none. It stays valid until the
// next call to Frame.
//
// The action matters to the Kitty keyboard protocol, which reports repeats and
// releases as well as presses. It costs nothing under the legacy encoding,
// where a release encodes to no bytes at all and a repeat to the same bytes as
// the press: the encoder reads the terminal's modes and decides.
type Event struct {
	Key    input.Key
	Action input.KeyAction
	Text   []byte

	off, length int
}

// A Reader turns each frame of keyboard state into events. Its scratch buffers
// are reused, so a frame of typing allocates nothing after the first.
type Reader struct {
	chars    []rune
	pressed  []ebiten.Key
	released []ebiten.Key
	text     []byte
	events   []Event
}

// Frame reports this frame's presses, in the order they should be sent.
//
// maxHeld drops keys that were already down when the window took focus: their
// press belongs to whatever had focus before, and Ebitengine goes on reporting
// them as held.
func (r *Reader) Frame(m Mods, maxHeld int) []Event {
	r.events, r.text = r.events[:0], r.text[:0]
	r.pressed = inpututil.AppendPressedKeys(r.pressed[:0])

	// Text first: a rune the platform produced already accounts for the
	// layout, dead keys and the IME. Only when a modifier makes it text-less
	// do we fall back to describing the physical key.
	if !m.Any() {
		r.chars = ebiten.AppendInputChars(r.chars[:0])
		// One rune and one printable key going down together is ordinary
		// typing, and the two halves of one event: Ebitengine reports them
		// separately, so they are put back together here. The key matters
		// under the Kitty keyboard protocol, which describes the key that was
		// pressed rather than the text it made; without it the encoder has
		// nothing to name and quietly falls back to the legacy bytes.
		//
		// Anything else -- an IME committing a phrase, a dead key resolving a
		// frame later -- is text with no one key behind it, and says so.
		paired := input.KeyUnidentified
		if len(r.chars) == 1 {
			if key, ok := r.pairedKey(maxHeld); ok {
				paired = key
			}
		}
		for _, c := range r.chars {
			off := len(r.text)
			r.text = utf8.AppendRune(r.text, c)
			r.events = append(r.events, Event{
				Key:    paired,
				Action: input.KeyActionPress,
				off:    off,
				length: len(r.text) - off,
			})
			paired = input.KeyUnidentified
		}
	}

	for _, key := range r.pressed {
		code, named := terminalKey(key)
		if !named || !PhysicallyPressed(key) {
			continue
		}
		held := inpututil.KeyPressDuration(key)
		if held > maxHeld {
			continue
		}
		if producesText(code) {
			// Letters and digits produce text on their own, so they are only
			// described as keys when a modifier swallowed the text. They do not
			// repeat: Ctrl+C held down should interrupt once.
			if held == 1 && m.Any() {
				r.events = append(r.events, Event{Key: code, Action: input.KeyActionPress})
			}
			continue
		}
		if code.Modifier() {
			// A held modifier is one press and, later, one release. Repeating
			// it would be a program under the Kitty protocol being told the
			// shift key went down thirty times a second.
			if held == 1 {
				r.events = append(r.events, Event{Key: code, Action: input.KeyActionPress})
			}
			continue
		}
		if Repeating(held) {
			r.events = append(r.events, Event{Key: code, Action: action(held)})
		}
	}

	// Releases, which the legacy encoding drops and the Kitty protocol asks
	// for. They are reported for every key this program can name, whether or
	// not its press was described as text: what the program does with them is
	// the encoder's business, not this one's.
	r.released = inpututil.AppendJustReleasedKeys(r.released[:0])
	for _, key := range r.released {
		if code, named := terminalKey(key); named {
			r.events = append(r.events, Event{Key: code, Action: input.KeyActionRelease})
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

// action is a press on the first tick and a repeat afterwards.
func action(held int) input.KeyAction {
	if held == 1 {
		return input.KeyActionPress
	}
	return input.KeyActionRepeat
}

// pairedKey is the printable key that went down this frame, when exactly one
// did. Two at once in the same frame cannot be told apart from the one rune
// that arrived, so neither is claimed.
func (r *Reader) pairedKey(maxHeld int) (input.Key, bool) {
	found, ok := input.KeyUnidentified, false
	for _, key := range r.pressed {
		if held := inpututil.KeyPressDuration(key); held != 1 || held > maxHeld {
			continue
		}
		code, named := terminalKey(key)
		if !named || !producesText(code) || !PhysicallyPressed(key) {
			continue
		}
		if ok {
			return input.KeyUnidentified, false
		}
		found, ok = code, true
	}
	return found, ok
}

// producesText reports whether the platform will report typing this key as a
// character, which is what decides whether it has to be described as a key.
//
// `Printable` is the binding's answer to "does this key stand for a
// character", and it is the right question for everything but the tab: it
// carries a codepoint, and no platform reports pressing it as text.
func producesText(key input.Key) bool {
	return key.Printable() && key != input.KeyTab
}

// terminalKey names an Ebitengine key in the terminal's key set.
//
// Both sides speak W3C key codes -- Ebitengine's `String` is the code with the
// letters' "Key" prefix dropped, and the binding parses the code itself -- so
// the mapping is a translation rather than a table to keep in step. What comes
// out of it is every key the binding knows and Ebitengine can report,
// including the function keys past F12 and the whole numpad, rather than the
// few dozen worth writing down by hand.
func terminalKey(key ebiten.Key) (input.Key, bool) {
	if int(key) < len(keyTable) {
		named := keyTable[key]
		return named, named != input.KeyUnidentified
	}
	return input.KeyUnidentified, false
}

var keyTable = func() []input.Key {
	table := make([]input.Key, ebiten.KeyMax+1)
	for key := ebiten.Key(0); key <= ebiten.KeyMax; key++ {
		name := key.String()
		// "A" is Ebitengine's spelling of the W3C code "KeyA"; every other
		// name the two use is the same string.
		if len(name) == 1 && name[0] >= 'A' && name[0] <= 'Z' {
			name = "Key" + name
		}
		if named, ok := input.KeyFromW3C(name); ok {
			table[key] = named
		}
	}
	return table
}()
