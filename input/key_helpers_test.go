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
	key, ok = input.KeyFromW3C("KeyA")
	if !ok || key != input.KeyKeyA {
		t.Errorf("KeyFromW3C(\"KeyA\") = %v, %v; want %v", key, ok, input.KeyKeyA)
	}
	if got := input.KeyW3C(input.KeyKeyA); got != "KeyA" {
		t.Errorf("KeyW3C(KeyA) = %q, want %q", got, "KeyA")
	}
	cp, ok := input.KeyCodepoint(input.KeyKeyA)
	if !ok || cp != 'a' {
		t.Errorf("KeyCodepoint(KeyA) = %q, %v; want 'a'", cp, ok)
	}
	if _, ok := input.KeyCodepoint(input.KeyShiftLeft); ok {
		t.Error("KeyCodepoint(KeyShiftLeft) reported a codepoint")
	}
	// The command modifier is super on macOS and control elsewhere.
	commandKey := input.KeyControlLeft
	if runtime.GOOS == "darwin" {
		commandKey = input.KeyMetaLeft
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
		{"KeyCtrlOrSuper", input.KeyCtrlOrSuper, commandKey, input.KeyShiftLeft},
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
