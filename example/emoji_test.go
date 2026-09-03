package main

import (
	"testing"

	"github.com/ironpark/gostty"
)

// The colour emoji font is read for its pictures. Ebitengine's text drawing
// rasterises outlines, and these glyphs are bitmaps, so without this an emoji
// is drawn as nothing at all.
func TestEmojiFontHasPictures(t *testing.T) {
	emoji := loadEmoji()
	if emoji == nil {
		t.Skip("no colour emoji font on this machine")
	}

	for _, r := range []rune{'😀', '🎉', '✅', '🌍', '🐈'} {
		img, ok := emoji.glyph(r, 20)
		if !ok {
			t.Errorf("no picture for %q", r)
			continue
		}
		if img.Bounds().Dx() <= 1 || img.Bounds().Dy() <= 1 {
			t.Errorf("picture for %q is %v, want something to look at", r, img.Bounds())
		}
	}

	// Letters and the wide scripts are the text faces' business; the emoji font
	// has nothing for them.
	for _, r := range []rune{'a', 'M', '한', '日'} {
		if _, ok := emoji.glyph(r, 20); ok {
			t.Errorf("the emoji font offered a picture for %q, which is text", r)
		}
	}

	// What cannot be drawn, and why: an emoji written as several codepoints --
	// a flag as two regional indicators, a family as people joined by zero
	// width joiners -- has its picture on the combination, not on any one of
	// them. A cell carries one codepoint, so the combination never arrives.
	if _, ok := emoji.glyph('🇰', 20); ok {
		t.Log("a lone regional indicator now has a picture of its own; flags may be drawable")
	}
}

// The pictures are per size, so a font size change has to throw them away: a
// bitmap for a 20px cell is not a bitmap for a 40px one.
func TestEmojiCacheFollowsTheSize(t *testing.T) {
	emoji := loadEmoji()
	if emoji == nil {
		t.Skip("no colour emoji font on this machine")
	}

	small, ok := emoji.glyph('😀', 20)
	if !ok {
		t.Fatal("no picture at 20px")
	}
	if len(emoji.cache) == 0 {
		t.Error("nothing was cached")
	}

	big, ok := emoji.glyph('😀', 64)
	if !ok {
		t.Fatal("no picture at 64px")
	}
	if big == small {
		t.Error("the same picture came back at a different size; the cache was not emptied")
	}
	if big.Bounds().Dx() <= small.Bounds().Dx() {
		t.Errorf("the 64px picture is %v and the 20px one %v; the bigger cell should get a bigger strike",
			big.Bounds(), small.Bounds())
	}

	// A rune with no picture is remembered as having none, rather than being
	// looked up again on every frame it is on screen.
	if _, ok := emoji.glyph('a', 64); ok {
		t.Fatal("the emoji font offered a picture for a letter")
	}
	if img, seen := emoji.cache['a']; !seen || img != nil {
		t.Error("a rune with no picture was not remembered as such")
	}
}

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
		w, err := gostty.CodepointWidth(uint32(r))
		if err != nil {
			t.Fatalf("CodepointWidth(%q): %v", r, err)
		}
		if got := w == 2; got != want {
			t.Errorf("%q is %d columns wide; the picture path %s reach it",
				r, w, map[bool]string{true: "would", false: "would not"}[got])
		}
	}
}
