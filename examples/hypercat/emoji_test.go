package main

import (
	"testing"

	"github.com/ironpark/gostty"
)

// Only two-column cells are drawn as pictures. That is the same rule the
// terminal laid the line out with, and it is what keeps the text-presentation
// symbols -- which the emoji font also has glyphs for -- in the text face.
func TestOnlyWideCellsAreEmoji(t *testing.T) {
	wide := map[rune]bool{}
	for _, r := range []rune{'😀', '🎉', '✅', '⌚', '한'} {
		wide[r] = true
	}
	for _, r := range []rune{'→', '©', '☂', 'a', '#'} {
		wide[r] = false
	}
	for r, want := range wide {
		w, err := gostty.CodepointWidth(r)
		if err != nil {
			t.Fatalf("CodepointWidth(%q): %v", r, err)
		}
		if got := w == 2; got != want {
			t.Errorf("%q is %d columns wide; the picture path %s reach it",
				r, w, map[bool]string{true: "would", false: "would not"}[got])
		}
	}
}
