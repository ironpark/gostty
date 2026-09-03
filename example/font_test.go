package main

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// The grid only works if the faces agree with the terminal about how many
// columns a character occupies: one for Latin, two for Hangul and CJK. A font
// without those glyphs, or with a wide glyph that is not exactly two cells,
// would drift across the row, so this checks coverage and advance together.
func TestFontsMatchTheGrid(t *testing.T) {
	fonts, err := loadFonts()
	if err != nil {
		t.Fatalf("loadFonts: %v", err)
	}
	if fonts.cellW <= 0 || fonts.cellH <= 0 {
		t.Fatalf("cell = %vx%v, want positive", fonts.cellW, fonts.cellH)
	}

	// A wide glyph has to fit inside its two columns without rattling around in
	// one: anything at or below a single cell means the face has no glyph and
	// is drawing a fallback box, and anything above two cells would spill into
	// the neighbouring cell.
	span := 2 * fonts.cellW
	for _, s := range []string{"안", "녕", "하", "세", "요", "日", "本", "あ"} {
		got := text.Advance(s, fonts.wide)
		if got > span || got <= fonts.cellW {
			t.Errorf("advance(%q) = %v, want within (%v, %v]", s, got, fonts.cellW, span)
		}
	}
	// The narrow advance is what the cell was rounded up from, so it is at most
	// one cell and never much less.
	const tolerance = 1.0
	for _, s := range []string{"a", "M", "1", "-"} {
		got := text.Advance(s, fonts.narrow)
		if got > fonts.cellW || got < fonts.cellW-tolerance {
			t.Errorf("advance(%q) = %v, want about %v (one cell)", s, got, fonts.cellW)
		}
	}
	// The centring offset keeps the glyph inside its span.
	if fonts.wideDX < 0 {
		t.Errorf("wideDX = %v, want a glyph no wider than its span", fonts.wideDX)
	}
}

// The bundled bitmap font is the fallback when no system font is found, so it
// has to satisfy the same rule on its own. It is one font rather than two, so
// its wide glyphs are exactly two cells.
func TestBitmapFallbackMatchesTheGrid(t *testing.T) {
	fonts := bitmapFonts()
	narrow := text.Advance("M", fonts.narrow)
	if got := text.Advance("안", fonts.wide); got != narrow*2 {
		t.Errorf("advance(\"안\") = %v, want %v (two cells)", got, narrow*2)
	}
	if fonts.cellW != narrow*bitmapScale {
		t.Errorf("cellW = %v, want %v", fonts.cellW, narrow*bitmapScale)
	}
}

// Both faces are drawn at the same em size unless the wide one had to shrink to
// fit. A wide face bigger than the narrow one is the bug this guards: matching
// the advance to two columns instead of the em inflates it by half again.
func TestWideFaceIsNotLarger(t *testing.T) {
	fonts, err := loadFonts()
	if err != nil {
		t.Fatalf("loadFonts: %v", err)
	}
	narrow, ok := fonts.narrow.(*text.GoTextFace)
	if !ok {
		t.Skip("no system font; the bitmap fallback is one face")
	}
	wide, ok := fonts.wide.(*text.GoTextFace)
	if !ok {
		t.Fatalf("wide face is %T, want a system face", fonts.wide)
	}
	if wide.Size > narrow.Size {
		t.Errorf("wide size %v > narrow size %v", wide.Size, narrow.Size)
	}
}
