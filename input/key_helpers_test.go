package input_test

import (
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
