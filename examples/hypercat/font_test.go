package main

import (
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// The grid only works if the faces agree with the terminal about how many
// columns a character occupies: one for Latin, two for Hangul and CJK. A font
// without those glyphs, or with a wide glyph that is not exactly two cells,
// would drift across the row, so this checks coverage and advance together.
func TestFontsMatchTheGrid(t *testing.T) {
	fonts := loadFonts(startingFamily(t), defaultFontSize)
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
		got := text.Advance(s, fonts.narrow[0])
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
	fonts := bitmapFonts(defaultFontSize)
	narrow := text.Advance("M", fonts.narrow[0])
	if got := text.Advance("안", fonts.wide); got != narrow*2 {
		t.Errorf("advance(\"안\") = %v, want %v (two cells)", got, narrow*2)
	}
	if fonts.cellW != narrow*fonts.scale {
		t.Errorf("cellW = %v, want %v", fonts.cellW, narrow*fonts.scale)
	}
}

// A bitmap can only be enlarged by a whole number, so the fallback answers a
// request for a size with the nearest one it can draw -- which is what makes it
// work on a HiDPI display, where the size asked for is twice as many pixels.
func TestBitmapFallbackScalesWholeNumbers(t *testing.T) {
	for _, c := range []struct {
		size  float64
		scale float64
	}{
		{8, 1}, {12, 1}, {14, 1}, {24, 2}, {28, 2}, {40, 3},
	} {
		fonts := bitmapFonts(c.size)
		if fonts.scale != c.scale {
			t.Errorf("bitmapFonts(%v).scale = %v, want %v", c.size, fonts.scale, c.scale)
		}
		if fonts.scale != math.Trunc(fonts.scale) {
			t.Errorf("bitmapFonts(%v).scale = %v, want a whole number", c.size, fonts.scale)
		}
	}
}

// Both faces are drawn at the same em size unless the wide one had to shrink to
// fit. A wide face bigger than the narrow one is the bug this guards: matching
// the advance to two columns instead of the em inflates it by half again.
func TestWideFaceIsNotLarger(t *testing.T) {
	fonts := loadFonts(startingFamily(t), defaultFontSize)
	narrow, ok := fonts.narrow[0].(*text.GoTextFace)
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

// startingFamily is what the window would open with on this machine, or nil
// where there is no system font at all and the bitmap fallback takes over.
func startingFamily(t *testing.T) *fontFamily {
	t.Helper()
	family, _ := defaultFamily(discoverFonts())
	return family
}

// A family is offered in the settings panel only if it can draw regular text,
// and the faces it does have must be the ones it says: a bold slot filled with
// a regular face would draw bold and regular text identically.
func TestDiscoveredFamiliesAreUsable(t *testing.T) {
	families := discoverFonts()
	if len(families) == 0 {
		t.Skip("no system fonts on this machine")
	}
	seen := map[string]bool{}
	for _, family := range families {
		if family.sources[0] == nil {
			t.Errorf("%s has no regular face", family.name)
		}
		if seen[family.name] {
			t.Errorf("%s is listed twice", family.name)
		}
		seen[family.name] = true

		for i, src := range family.sources {
			if src == nil {
				continue
			}
			meta := src.Metadata()
			if bold := meta.Weight >= text.WeightSemibold; bold != (i&faceBold != 0) {
				t.Errorf("%s slot %d holds a face of weight %v", family.name, i, meta.Weight)
			}
			if italic := meta.Style == text.StyleItalic; italic != (i&faceItalic != 0) {
				t.Errorf("%s slot %d holds a face of style %v", family.name, i, meta.Style)
			}
		}
	}
}

// Bold and italic have to come out of the family the user chose, and where the
// family has no such face the renderer is told to double-strike instead of
// silently drawing regular text.
func TestFaceSelection(t *testing.T) {
	family := startingFamily(t)
	if family == nil {
		t.Skip("no system fonts on this machine")
	}
	fonts := loadFonts(family, defaultFontSize)
	for _, c := range []struct{ bold, italic bool }{
		{false, false}, {true, false}, {false, true}, {true, true},
	} {
		face, synthetic := fonts.face(c.bold, c.italic)
		if face == nil {
			t.Fatalf("bold=%v italic=%v has no face", c.bold, c.italic)
		}
		if family.has(c.bold, c.italic) && synthetic {
			t.Errorf("bold=%v italic=%v is double-struck though %s has the face",
				c.bold, c.italic, family.name)
		}
		if c.bold && !family.has(true, c.italic) && !synthetic {
			t.Errorf("bold=%v italic=%v is neither a bold face nor double-struck",
				c.bold, c.italic)
		}
		// Every face is the same size, so the grid stays square.
		if got, ok := face.(*text.GoTextFace); ok && got.Size != fonts.size {
			t.Errorf("bold=%v italic=%v size = %v, want %v", c.bold, c.italic, got.Size, fonts.size)
		}
	}
}

// The cell is what the grid is laid out on, so it has to come from the regular
// face and hold whole pixels at every size the settings panel offers.
func TestCellSizeAcrossSizes(t *testing.T) {
	family := startingFamily(t)
	if family == nil {
		t.Skip("no system fonts on this machine")
	}
	var lastW, lastH float64
	for size := float64(minFontSize); size <= maxFontSize; size++ {
		fonts := loadFonts(family, size)
		if fonts.cellW <= 0 || fonts.cellH <= 0 {
			t.Fatalf("size %v: cell = %vx%v", size, fonts.cellW, fonts.cellH)
		}
		if fonts.cellW != math.Trunc(fonts.cellW) || fonts.cellH != math.Trunc(fonts.cellH) {
			t.Errorf("size %v: cell %vx%v is not whole pixels", size, fonts.cellW, fonts.cellH)
		}
		if fonts.cellW < lastW || fonts.cellH < lastH {
			t.Errorf("size %v: cell %vx%v shrank from %vx%v", size, fonts.cellW, fonts.cellH, lastW, lastH)
		}
		lastW, lastH = fonts.cellW, fonts.cellH
	}
}

// Everything in the window is measured in device pixels, so that a HiDPI screen
// is drawn at its own resolution instead of at a quarter of it and stretched.
// The size in the settings panel stays device-independent, because that is what
// a user means by "14px"; the scale factor is applied when the faces are built.
func TestFontSizeIsScaledByTheDisplay(t *testing.T) {
	families := discoverFonts()
	if len(families) == 0 {
		t.Skip("no system fonts on this machine")
	}
	g := &game{dsf: 2}
	g.ui.families = families
	g.ui.size = defaultFontSize
	g.applyFont()

	if g.fonts.size != defaultFontSize*2 {
		t.Errorf("face size = %v, want %v (%v at 2x)", g.fonts.size, defaultFontSize*2, defaultFontSize)
	}
	if g.ui.size != defaultFontSize {
		t.Errorf("the settings size became %v; it should stay in the units the user chose", g.ui.size)
	}
	if !g.relayout {
		t.Error("the grid was not marked for relayout after the cell changed shape")
	}

	// The cell has to grow with it, since that is what the grid is laid out on.
	one := loadFonts(families[0], defaultFontSize)
	two := loadFonts(families[0], defaultFontSize*2)
	if two.cellW < 2*one.cellW-2 || two.cellH < 2*one.cellH-2 {
		t.Errorf("cell at 2x is %vx%v, want about twice %vx%v",
			two.cellW, two.cellH, one.cellW, one.cellH)
	}
}

// With no system font the same has to hold for the bundled bitmap, which can
// only grow in whole steps.
func TestBitmapFallbackFollowsTheDisplay(t *testing.T) {
	g := &game{dsf: 2}
	g.ui.size = defaultFontSize
	g.applyFont()
	if g.fonts.family != "bitmap" {
		t.Fatalf("family = %q, want the bitmap fallback with no families to choose from", g.fonts.family)
	}
	one := bitmapFonts(defaultFontSize)
	if g.fonts.scale <= one.scale {
		t.Errorf("scale at 2x is %v, want more than %v", g.fonts.scale, one.scale)
	}
}

// JetBrains Mono is what the window opens with when it is on the machine: a
// terminal font with all four faces beats whatever the system happens to ship.
// The Nerd Font builds come in three widths and only the "Mono" one keeps its
// icons inside a single cell, so it is preferred over the others.
func TestDefaultFamilyPrefersJetBrainsMono(t *testing.T) {
	for _, c := range []struct {
		name string
		have []string
		want string
	}{
		{"nothing else", []string{"Menlo", "Monaco"}, "Menlo"},
		{"the plain family", []string{"Menlo", "JetBrains Mono", "Monaco"}, "JetBrains Mono"},
		{"the nerd font builds", []string{"Menlo", "JetBrainsMono NF", "JetBrainsMono NFM"}, "JetBrainsMono NFM"},
		{"only the wide nerd font", []string{"Menlo", "JetBrainsMono NF"}, "JetBrainsMono NF"},
		{"a build not on the list", []string{"Menlo", "JetBrainsMono Something"}, "JetBrainsMono Something"},
		{"the plain family over a nerd font", []string{"JetBrainsMono NFM", "JetBrains Mono"}, "JetBrains Mono"},
	} {
		families := make([]*fontFamily, len(c.have))
		for i, name := range c.have {
			families[i] = &fontFamily{name: name}
		}
		got, at := defaultFamily(families)
		if got == nil {
			t.Errorf("%s: no family chosen", c.name)
			continue
		}
		if got.name != c.want {
			t.Errorf("%s: chose %q, want %q", c.name, got.name, c.want)
		}
		if families[at] != got {
			t.Errorf("%s: index %d is %q, not the family chosen", c.name, at, families[at].name)
		}
	}

	if _, at := defaultFamily(nil); at != 0 {
		t.Errorf("index = %d with no families at all, want 0", at)
	}
}

// Naming a font means it: the override is not second-guessed.
func TestDefaultFamilyRespectsTheOverride(t *testing.T) {
	t.Setenv("GOSTTY_FONT", "/somewhere/Courier.ttf")
	families := []*fontFamily{{name: "Courier"}, {name: "JetBrains Mono"}}
	got, at := defaultFamily(families)
	if got.name != "Courier" || at != 0 {
		t.Errorf("chose %q at %d, want the overridden font first", got.name, at)
	}
}

// The same family is written several ways depending on where it came from.
func TestFamilyNamesCompareLoosely(t *testing.T) {
	for _, c := range [][2]string{
		{"JetBrains Mono", "JetBrainsMono"},
		{"JetBrainsMono NFM", "jetbrainsmono nfm"},
		{"Menlo", "menlo"},
	} {
		if !sameFamily(c[0], c[1]) {
			t.Errorf("%q and %q are the same family", c[0], c[1])
		}
	}
	if sameFamily("JetBrains Mono", "JetBrainsMono NL") {
		t.Error("JetBrains Mono and JetBrainsMono NL are different families")
	}
}
