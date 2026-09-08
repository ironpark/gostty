package fonts

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
	fonts := Load(startingFamily(t), DefaultSize)
	if fonts.CellWidth <= 0 || fonts.CellHeight <= 0 {
		t.Fatalf("cell = %vx%v, want positive", fonts.CellWidth, fonts.CellHeight)
	}

	// A wide glyph has to fit inside its two columns without rattling around in
	// one: anything at or below a single cell means the face has no glyph and
	// is drawing a fallback box, and anything above two cells would spill into
	// the neighbouring cell.
	span := 2 * fonts.CellWidth
	for _, s := range []string{"안", "녕", "하", "세", "요", "日", "本", "あ"} {
		got := text.Advance(s, fonts.Wide)
		if got > span || got <= fonts.CellWidth {
			t.Errorf("advance(%q) = %v, want within (%v, %v]", s, got, fonts.CellWidth, span)
		}
	}
	// The narrow advance is what the cell was rounded up from, so it is at most
	// one cell and never much less.
	const tolerance = 1.0
	for _, s := range []string{"a", "M", "1", "-"} {
		got := text.Advance(s, fonts.narrow[0])
		if got > fonts.CellWidth || got < fonts.CellWidth-tolerance {
			t.Errorf("advance(%q) = %v, want about %v (one cell)", s, got, fonts.CellWidth)
		}
	}
	// The centring offset keeps the glyph inside its span.
	if fonts.WideDX < 0 {
		t.Errorf("WideDX = %v, want a glyph no wider than its span", fonts.WideDX)
	}
}

// The bundled bitmap font is the fallback when no system font is found, so it
// has to satisfy the same rule on its own. It is one font rather than two, so
// its wide glyphs are exactly two cells.
func TestBitmapFallbackMatchesTheGrid(t *testing.T) {
	fonts := bitmapFonts(DefaultSize)
	narrow := text.Advance("M", fonts.narrow[0])
	if got := text.Advance("안", fonts.Wide); got != narrow*2 {
		t.Errorf("advance(\"안\") = %v, want %v (two cells)", got, narrow*2)
	}
	if fonts.CellWidth != narrow*fonts.Scale {
		t.Errorf("CellWidth = %v, want %v", fonts.CellWidth, narrow*fonts.Scale)
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
		if fonts.Scale != c.scale {
			t.Errorf("bitmapFonts(%v).scale = %v, want %v", c.size, fonts.Scale, c.scale)
		}
		if fonts.Scale != math.Trunc(fonts.Scale) {
			t.Errorf("bitmapFonts(%v).scale = %v, want a whole number", c.size, fonts.Scale)
		}
	}
}

// Both faces are drawn at the same em size unless the wide one had to shrink to
// fit. A wide face bigger than the narrow one is the bug this guards: matching
// the advance to two columns instead of the em inflates it by half again.
func TestWideFaceIsNotLarger(t *testing.T) {
	fonts := Load(startingFamily(t), DefaultSize)
	narrow, ok := fonts.narrow[0].(*text.GoTextFace)
	if !ok {
		t.Skip("no system font; the bitmap fallback is one face")
	}
	wide, ok := fonts.Wide.(*text.GoTextFace)
	if !ok {
		t.Fatalf("wide face is %T, want a system face", fonts.Wide)
	}
	if wide.Size > narrow.Size {
		t.Errorf("wide size %v > narrow size %v", wide.Size, narrow.Size)
	}
}

// startingFamily is what the window would open with on this machine, or nil
// where there is no system font at all and the bitmap fallback takes over.
func startingFamily(t *testing.T) *Family {
	t.Helper()
	family, _ := DefaultFamily(Discover())
	return family
}

// A family is offered in the settings panel only if it can draw regular text,
// and the faces it does have must be the ones it says: a bold slot filled with
// a regular face would draw bold and regular text identically.
func TestDiscoveredFamiliesAreUsable(t *testing.T) {
	families := Discover()
	if len(families) == 0 {
		t.Skip("no system fonts on this machine")
	}
	seen := map[string]bool{}
	for _, family := range families {
		if family.sources[0] == nil {
			t.Errorf("%s has no regular face", family.Name)
		}
		if seen[family.Name] {
			t.Errorf("%s is listed twice", family.Name)
		}
		seen[family.Name] = true

		for i, src := range family.sources {
			if src == nil {
				continue
			}
			meta := src.Metadata()
			if bold := meta.Weight >= text.WeightSemibold; bold != (i&faceBold != 0) {
				t.Errorf("%s slot %d holds a face of weight %v", family.Name, i, meta.Weight)
			}
			if italic := meta.Style == text.StyleItalic; italic != (i&faceItalic != 0) {
				t.Errorf("%s slot %d holds a face of style %v", family.Name, i, meta.Style)
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
	fonts := Load(family, DefaultSize)
	for _, c := range []struct{ bold, italic bool }{
		{false, false}, {true, false}, {false, true}, {true, true},
	} {
		face, synthetic := fonts.Face(c.bold, c.italic)
		if face == nil {
			t.Fatalf("bold=%v italic=%v has no face", c.bold, c.italic)
		}
		if family.Has(c.bold, c.italic) && synthetic {
			t.Errorf("bold=%v italic=%v is double-struck though %s has the face",
				c.bold, c.italic, family.Name)
		}
		if c.bold && !family.Has(true, c.italic) && !synthetic {
			t.Errorf("bold=%v italic=%v is neither a bold face nor double-struck",
				c.bold, c.italic)
		}
		// Every face is the same size, so the grid stays square.
		if got, ok := face.(*text.GoTextFace); ok && got.Size != fonts.Size {
			t.Errorf("bold=%v italic=%v size = %v, want %v", c.bold, c.italic, got.Size, fonts.Size)
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
	for size := float64(MinSize); size <= MaxSize; size++ {
		fonts := Load(family, size)
		if fonts.CellWidth <= 0 || fonts.CellHeight <= 0 {
			t.Fatalf("size %v: cell = %vx%v", size, fonts.CellWidth, fonts.CellHeight)
		}
		if fonts.CellWidth != math.Trunc(fonts.CellWidth) || fonts.CellHeight != math.Trunc(fonts.CellHeight) {
			t.Errorf("size %v: cell %vx%v is not whole pixels", size, fonts.CellWidth, fonts.CellHeight)
		}
		if fonts.CellWidth < lastW || fonts.CellHeight < lastH {
			t.Errorf("size %v: cell %vx%v shrank from %vx%v", size, fonts.CellWidth, fonts.CellHeight, lastW, lastH)
		}
		lastW, lastH = fonts.CellWidth, fonts.CellHeight
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
		families := make([]*Family, len(c.have))
		for i, name := range c.have {
			families[i] = &Family{Name: name}
		}
		got, at := DefaultFamily(families)
		if got == nil {
			t.Errorf("%s: no family chosen", c.name)
			continue
		}
		if got.Name != c.want {
			t.Errorf("%s: chose %q, want %q", c.name, got.Name, c.want)
		}
		if families[at] != got {
			t.Errorf("%s: index %d is %q, not the family chosen", c.name, at, families[at].Name)
		}
	}

	if _, at := DefaultFamily(nil); at != 0 {
		t.Errorf("index = %d with no families at all, want 0", at)
	}
}

// Naming a font means it: the override is not second-guessed.
func TestDefaultFamilyRespectsTheOverride(t *testing.T) {
	t.Setenv("GOSTTY_FONT", "/somewhere/Courier.ttf")
	families := []*Family{{Name: "Courier"}, {Name: "JetBrains Mono"}}
	got, at := DefaultFamily(families)
	if got.Name != "Courier" || at != 0 {
		t.Errorf("chose %q at %d, want the overridden font first", got.Name, at)
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
