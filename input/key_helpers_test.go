package input_test

import (
	"runtime"
	"testing"

	"github.com/ironpark/gostty/input"
)

// The key helpers answer what a key is, for callers mapping platform key
// codes onto the enum or deciding whether a press can carry text.
func TestKeyHelpers(t *testing.T) {
	key, ok := input.KeyFromAscii('a')
	if !ok || key != input.KeyKeyA {
		t.Errorf("KeyFromAscii('a') = %v, %v; want %v", key, ok, input.KeyKeyA)
	}
	if _, ok := input.KeyFromAscii(0x01); ok {
		t.Error("KeyFromAscii(0x01) reported a key")
	}
	cp, ok := input.KeyCodepoint(input.KeyKeyA)
	if !ok || cp != 'a' {
		t.Errorf("KeyCodepoint(KeyA) = %q, %v; want 'a'", cp, ok)
	}
	if _, ok := input.KeyCodepoint(input.KeyShiftLeft); ok {
		t.Error("KeyCodepoint(KeyShiftLeft) reported a codepoint")
	}
	checks := []struct {
		name string
		fn   func(input.Key) bool
		yes  input.Key
		no   input.Key
	}{
		{"KeyPrintable", input.KeyPrintable, input.KeyKeyA, input.KeyShiftLeft},
		{"KeyModifier", input.KeyModifier, input.KeyShiftLeft, input.KeyKeyA},
		{"KeyKeypad", input.KeyKeypad, input.KeyNumpad1, input.KeyDigit1},
		{"KeyLeftOrRightShift", input.KeyLeftOrRightShift, input.KeyShiftRight, input.KeyControlLeft},
		{"KeyLeftOrRightAlt", input.KeyLeftOrRightAlt, input.KeyAltLeft, input.KeyShiftLeft},
		// False for the writing system keys, whose meaning a layout decides.
		{"KeyShouldBeRemappable", input.KeyShouldBeRemappable, input.KeyF1, input.KeyKeyA},
	}
	for _, c := range checks {
		if !c.fn(c.yes) {
			t.Errorf("%s(%v) = false, want true", c.name, c.yes)
		}
		if c.fn(c.no) {
			t.Errorf("%s(%v) = true, want false", c.name, c.no)
		}
	}
}

// The W3C names are what a browser or Electron embedder receives as
// `KeyboardEvent.code`, so the pair has to round-trip.
func TestKeyW3C(t *testing.T) {
	for _, key := range []input.Key{input.KeyKeyA, input.KeyArrowUp, input.KeyEnter, input.KeyF1} {
		name := input.KeyW3C(key)
		if name == "" {
			t.Errorf("KeyW3C(%v) = %q, want a name", key, name)
			continue
		}
		got, ok := input.KeyFromW3C(name)
		if !ok || got != key {
			t.Errorf("KeyFromW3C(%q) = %v, %v; want %v, true", name, got, ok, key)
		}
	}
	if got, ok := input.KeyFromW3C("NotAKeyAtAll"); ok {
		t.Errorf("KeyFromW3C(unknown) = %v, true; want false", got)
	}
}

// Which key is the primary modifier is decided when the native library for
// this platform is built, so it follows the platform Go is running on.
func TestKeyCtrlOrSuper(t *testing.T) {
	primary, other := input.KeyControlLeft, input.KeyMetaLeft
	if runtime.GOOS == "darwin" {
		primary, other = input.KeyMetaLeft, input.KeyControlLeft
	}
	if !input.KeyCtrlOrSuper(primary) {
		t.Errorf("KeyCtrlOrSuper(%v) on %s = false, want true", primary, runtime.GOOS)
	}
	if input.KeyCtrlOrSuper(other) {
		t.Errorf("KeyCtrlOrSuper(%v) on %s = true, want false", other, runtime.GOOS)
	}
}
