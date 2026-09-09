package fonts

import "testing"

// The colour emoji font is read for its pictures. Ebitengine's text drawing
// rasterises outlines, and these glyphs are bitmaps, so without this an emoji
// is drawn as nothing at all.
func TestEmojiFontHasPictures(t *testing.T) {
	emoji := LoadEmoji()
	if emoji == nil {
		t.Skip("no colour emoji font on this machine")
	}

	for _, r := range []string{"😀", "🎉", "✅", "🌍", "🐈"} {
		img, ok := emoji.Glyph(r, 20)
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
	for _, r := range []string{"a", "M", "한", "日"} {
		if _, ok := emoji.Glyph(r, 20); ok {
			t.Errorf("the emoji font offered a picture for %q, which is text", r)
		}
	}

	// An emoji written as several codepoints -- a flag as two regional
	// indicators, a family as people joined by zero width joiners, a skin tone
	// as a modifier -- has its picture on the combination. The terminal hands
	// the whole cluster over for one cell, and the font is asked to shape it,
	// so these come out as one picture rather than as their parts.
	for _, cluster := range []string{"🇰🇷", "👨‍👩‍👧", "👍🏽"} {
		img, ok := emoji.Glyph(cluster, 20)
		if !ok {
			t.Errorf("no picture for the cluster %q", cluster)
			continue
		}
		if img.Bounds().Dx() <= 1 || img.Bounds().Dy() <= 1 {
			t.Errorf("picture for %q is %v, want something to look at", cluster, img.Bounds())
		}
	}

	// Half a flag is not a flag: a lone regional indicator has no picture, and
	// neither has a cluster of ordinary letters that happens to be long.
	for _, cluster := range []string{"ab", "e\u0301"} {
		if _, ok := emoji.Glyph(cluster, 20); ok {
			t.Errorf("the emoji font offered a picture for %q, which is text", cluster)
		}
	}
}

// The pictures are per size, so a font size change has to throw them away: a
// bitmap for a 20px cell is not a bitmap for a 40px one.
func TestEmojiCacheFollowsTheSize(t *testing.T) {
	emoji := LoadEmoji()
	if emoji == nil {
		t.Skip("no colour emoji font on this machine")
	}

	small, ok := emoji.Glyph("😀", 20)
	if !ok {
		t.Fatal("no picture at 20px")
	}
	if len(emoji.cache) == 0 {
		t.Error("nothing was cached")
	}

	big, ok := emoji.Glyph("😀", 64)
	if !ok {
		t.Fatal("no picture at 64px")
	}
	if big == small {
		t.Error("the same picture came back at a different size; the cache was not emptied")
	}
	// How much bigger, or whether at all, is the font's business: Apple Color
	// Emoji carries a strike per size and Noto's carries one 136x128 strike for
	// every ppem. What the cache owes is a re-render, not a larger one, so the
	// only size this can hold to is that the strike never shrank.
	if big.Bounds().Dx() < small.Bounds().Dx() {
		t.Errorf("the 64px picture is %v and the 20px one %v; the bigger cell got the smaller strike",
			big.Bounds(), small.Bounds())
	}

	// Text with no picture is remembered as having none, rather than being
	// looked up again on every frame it is on screen.
	if _, ok := emoji.Glyph("a", 64); ok {
		t.Fatal("the emoji font offered a picture for a letter")
	}
	if img, seen := emoji.cache["a"]; !seen || img != nil {
		t.Error("a rune with no picture was not remembered as such")
	}
}
