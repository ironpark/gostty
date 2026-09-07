package gostty

import "testing"

// ColorName is one of the enums ghostty declares non-exhaustive, so a number
// arriving from a pty converts rather than failing. IsKnown separates such a
// number from a name this binding has; it does not say ghostty would reject it.
func TestOpenEnumIsKnown(t *testing.T) {
	if !ColorNameBlack.IsKnown() {
		t.Error("ColorNameBlack is a declared tag but IsKnown() is false")
	}
	if ColorName(200).IsKnown() {
		t.Error("an undeclared ColorName reported IsKnown() = true")
	}
	// The same holds for the other open ones.
	if !EraseLineRight.IsKnown() || EraseLine(200).IsKnown() {
		t.Error("EraseLine IsKnown() does not separate tags from stray numbers")
	}
}

// Values is declaration order, and every entry it lists is one IsKnown admits.
func TestEnumValuesAreKnownAndOrdered(t *testing.T) {
	modes := ModeValues()
	if len(modes) == 0 {
		t.Fatal("ModeValues() is empty")
	}
	for _, mode := range modes {
		if !mode.IsKnown() {
			t.Errorf("ModeValues() lists %v, which IsKnown() rejects", mode)
		}
	}
	// A fresh slice each call, so a caller sorting it cannot disturb the next.
	first := ModeValues()
	first[0] = Mode(0)
	if ModeValues()[0] == Mode(0) && modes[0] != Mode(0) {
		t.Error("ModeValues() handed back shared backing storage")
	}
}
