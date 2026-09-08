package keys

import "testing"

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

// The two tables must not overlap: a key in both would be described twice,
// once as a repeating non-text key and once as a modified text key.
func TestKeyTablesAreDisjoint(t *testing.T) {
	for key := range nonTextKeys {
		if _, both := textKeys[key]; both {
			t.Errorf("key %v is in both nonTextKeys and textKeys", key)
		}
	}
}
