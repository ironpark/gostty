package input_test

import (
	"testing"

	"github.com/ironpark/gostty/input"
)

// KeyMods is the packed struct a caller builds by hand for every key event, so
// it is the one most often printed while working out why a binding did not
// fire. Named members beat six bools and a padding word.
func TestKeyModsString(t *testing.T) {
	for _, c := range []struct {
		mods input.KeyMods
		want string
	}{
		{input.KeyMods{}, "none"},
		{input.KeyMods{Ctrl: true}, "Ctrl"},
		{input.KeyMods{Shift: true, Alt: true}, "Shift|Alt"},
		{input.KeyMods{Shift: true, Ctrl: true, Alt: true, Super: true}, "Shift|Ctrl|Alt|Super"},
	} {
		if got := c.mods.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}

	// The padding is not a modifier, whatever the backing integer carries.
	if got := (input.KeyMods{Padding: 3}).String(); got != "none" {
		t.Errorf("padding leaked into String(): %q", got)
	}
}
