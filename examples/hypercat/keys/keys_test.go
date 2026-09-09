package keys

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty/input"
)

func TestRepeating(t *testing.T) {
	// First press fires immediately.
	if !Repeating(1) {
		t.Errorf("Repeating(1) = false, want true")
	}

	// Between 2 and repeatDelayTicks, it must not repeat.
	for held := 2; held <= repeatDelayTicks; held++ {
		if Repeating(held) {
			t.Errorf("Repeating(%d) = true, want false during initial delay", held)
		}
	}

	// After delay, it fires every repeatIntervalTicks.
	if !Repeating(repeatDelayTicks + repeatIntervalTicks) {
		t.Errorf("Repeating(%d) = false, want true", repeatDelayTicks+repeatIntervalTicks)
	}
	if Repeating(repeatDelayTicks + 1) {
		t.Errorf("Repeating(%d) = true, want false", repeatDelayTicks+1)
	}
}

// The repeat rate is invented, so it is pinned: a key with no text of its own
// has to repeat at something like the rate the window system repeats a letter
// at, or the two halves of the keyboard disagree -- and a key that commits a
// line, held for a moment, fills the screen.
//
// The bounds are the range the desktop platforms' defaults fall in, at
// Ebitengine's 60Hz tick: roughly a third of a second before the first repeat,
// and roughly ten a second after it.
func TestRepeatRateMatchesThePlatform(t *testing.T) {
	const ticksPerSecond = 60

	delay := float64(repeatDelayTicks) / ticksPerSecond
	if delay < 0.25 || delay > 0.6 {
		t.Errorf("the first repeat comes after %.2fs, want between 0.25s and 0.6s", delay)
	}
	rate := float64(ticksPerSecond) / float64(repeatIntervalTicks)
	if rate < 8 || rate > 15 {
		t.Errorf("keys repeat %.0f times a second, want between 8 and 15", rate)
	}

	// The same thing said in the terms the caller sees: a key held for a
	// second fires about a dozen times, not several dozen.
	fired := 0
	for held := 1; held <= ticksPerSecond; held++ {
		if Repeating(held) {
			fired++
		}
	}
	if fired < 5 || fired > 12 {
		t.Errorf("a key held for a second fired %d times, want about eight", fired)
	}
}

func TestCurrentDoesNotPanic(t *testing.T) {
	// On macOS this reaches CoreGraphics through purego, so that it answers at
	// all is the claim; there is no keyboard to assert about under `go test`.
	t.Logf("Current: %+v", Current())
}

// Shift is deliberately not one of the modifiers that suppress text: the
// platform has already folded it into the rune it produced.
func TestAnyIgnoresShift(t *testing.T) {
	if (Mods{Shift: true}).Any() {
		t.Errorf("Mods{Shift: true}.Any() = true, want false")
	}
	for _, m := range []Mods{{Ctrl: true}, {Alt: true}, {Super: true}} {
		if !m.Any() {
			t.Errorf("%+v.Any() = false, want true", m)
		}
	}
}

// Ebitengine's key names and the binding's W3C codes are the same strings, so
// the mapping between them is a translation rather than a table. What it has
// to get right is that every key worth sending is named, and named correctly.
func TestTerminalKeyNamesEbitengineKeys(t *testing.T) {
	for _, test := range []struct {
		key  ebiten.Key
		want input.Key
	}{
		{ebiten.KeyA, input.KeyKeyA},
		{ebiten.KeyZ, input.KeyKeyZ},
		{ebiten.KeyDigit0, input.KeyDigit0},
		{ebiten.KeyArrowUp, input.KeyArrowUp},
		{ebiten.KeyEnter, input.KeyEnter},
		{ebiten.KeyBackspace, input.KeyBackspace},
		{ebiten.KeyEscape, input.KeyEscape},
		{ebiten.KeyF1, input.KeyF1},
		{ebiten.KeyBracketLeft, input.KeyBracketLeft},
		{ebiten.KeySpace, input.KeySpace},
		// Past what the hand-written tables covered, and free with the
		// translation: the high function keys, the numpad, and the modifiers
		// the Kitty protocol reports.
		{ebiten.KeyF13, input.KeyF13},
		{ebiten.KeyNumpad7, input.KeyNumpad7},
		{ebiten.KeyNumpadEnter, input.KeyNumpadEnter},
		{ebiten.KeyShiftLeft, input.KeyShiftLeft},
	} {
		got, named := terminalKey(test.key)
		if !named || got != test.want {
			t.Errorf("terminalKey(%v) = %v, %v; want %v", test.key, got, named, test.want)
		}
	}
}

// Which keys have to be described to the encoder is the binding's answer, not
// a list kept here: a printable key arrives as text, and everything else has
// to be named. The tab is the exception every platform makes.
func TestProducesTextFollowsPrintable(t *testing.T) {
	for _, key := range []input.Key{input.KeyKeyA, input.KeyDigit1, input.KeySpace, input.KeyNumpad0} {
		if !producesText(key) {
			t.Errorf("producesText(%v) = false, want true", key)
		}
	}
	for _, key := range []input.Key{input.KeyEnter, input.KeyArrowUp, input.KeyF5, input.KeyTab, input.KeyBackspace} {
		if producesText(key) {
			t.Errorf("producesText(%v) = true, want false", key)
		}
	}
}

// A press on the first tick, a repeat afterwards: the Kitty protocol tells the
// two apart and the legacy encoding does not care.
func TestActionIsPressThenRepeat(t *testing.T) {
	if got := action(1); got != input.KeyActionPress {
		t.Errorf("action(1) = %v, want a press", got)
	}
	if got := action(repeatDelayTicks + repeatIntervalTicks); got != input.KeyActionRepeat {
		t.Errorf("action(held) = %v, want a repeat", got)
	}
}
