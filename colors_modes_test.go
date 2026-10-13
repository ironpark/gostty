package gostty

import "testing"

// Colors come back packed as 0xRRGGBB, the same as RenderCell, and follow
// the OSC 10/11/12 changes a program makes.
func TestColors(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	term.SetDefaultBackgroundColor(RGBFromUint32(0x101010))
	if bg, ok := term.BackgroundColor(); !ok || bg.Uint32() != 0x101010 {
		t.Errorf("BackgroundColor() = %#x, %v; want default 0x101010", bg, ok)
	}
	// OSC 11 overrides the default.
	feed(t, stream, "\x1b]11;rgb:ff/00/00\x1b\\")
	if bg, ok := term.BackgroundColor(); !ok || bg.Uint32() != 0xff0000 {
		t.Errorf("BackgroundColor() after OSC 11 = %#x, %v; want 0xff0000", bg, ok)
	}
	// OSC 111 resets to the default.
	feed(t, stream, "\x1b]111\x1b\\")
	if bg, _ := term.BackgroundColor(); bg.Uint32() != 0x101010 {
		t.Errorf("BackgroundColor() after OSC 111 = %#x; want 0x101010", bg)
	}

	// Palette entry 1 is red by default; OSC 4 changes it.
	palette := make([]RGB, 256)
	term.PaletteColors(palette)
	before := palette[1]
	feed(t, stream, "\x1b]4;1;rgb:12/34/56\x1b\\")
	n := term.PaletteColors(palette)
	if palette[1].Uint32() != 0x123456 || palette[1] == before {
		t.Errorf("palette[1] after OSC 4 = %#x; want 0x123456", palette[1])
	}
	if n != 256 || palette[1].Uint32() != 0x123456 {
		t.Errorf("PaletteColors() = %d, [1]=%#x", n, palette[1])
	}
	if n := term.PaletteColors(palette[:16]); n != 16 {
		t.Errorf("PaletteColors(16) wrote %d", n)
	}
}

// Modes read the state the parser keeps, and can be set without a sequence.
func TestModes(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	if on := term.ModeEnabled(ModeCursorKeys); on {
		t.Fatalf("ModeEnabled(cursor_keys) = %v; want off", on)
	}
	feed(t, stream, "\x1b[?1h")
	if on := term.ModeEnabled(ModeCursorKeys); !on {
		t.Error("DECCKM not reported after CSI ? 1 h")
	}
	term.SetMode(ModeCursorKeys, false)
	if on := term.ModeEnabled(ModeCursorKeys); on {
		t.Error("SetMode(false) did not clear DECCKM")
	}
	mode, err := ParseMode("bracketed_paste")
	if err != nil || mode != ModeBracketedPaste {
		t.Errorf("ParseMode = %v, %v", mode, err)
	}
}

// A selection is reported in screen coordinates and can be set back.
func TestSelectionValue(t *testing.T) {
	term, stream := newStreamPair(t, 10, 3)
	feed(t, stream, "hello\r\nworld")
	screen := term.ActiveScreen()
	if _, ok := screen.Selection(); ok {
		t.Fatalf("Selection() before selecting = ok %v", ok)
	}
	if ok, err := screen.SetSelection(Selection{StartX: 1, StartY: 0, EndX: 2, EndY: 1}); err != nil || !ok {
		t.Fatalf("SetSelection = %v, %v", ok, err)
	}
	sel, ok := screen.Selection()
	if !ok {
		t.Fatal("expected selection")
	}
	if sel != (Selection{StartX: 1, StartY: 0, EndX: 2, EndY: 1}) {
		t.Errorf("Selection() = %+v", sel)
	}
	if text, ok, _ := screen.SelectionString(); !ok || text != "ello\nwor" {
		t.Errorf("SelectionString() = %q, %v", text, ok)
	}
	if top := screen.ViewportTop(); top != 0 {
		t.Errorf("ViewportTop() = %d", top)
	}
	if ok, _ := screen.SetSelection(Selection{StartX: 0, StartY: 99, EndX: 0, EndY: 99}); ok {
		t.Error("SetSelection outside the screen reported true")
	}
}

// Search matches come back as selections without moving the screen's own.
func TestSearchMatches(t *testing.T) {
	term, stream := newStreamPair(t, 10, 3)
	feed(t, stream, "ab ab\r\nab")
	search, err := term.NewSearch("ab")
	if err != nil {
		t.Fatal(err)
	}
	defer search.Close()
	search.All()
	if _, ok := search.SelectedMatch(); ok {
		t.Fatalf("SelectedMatch() before Select = ok %v", ok)
	}
	count := search.MatchCount()
	if count != 3 {
		t.Fatalf("MatchCount() = %d; want 3", count)
	}
	matches := make([]Selection, count)
	n := search.Matches(matches)
	if n != 3 {
		t.Fatalf("Matches() = %d", n)
	}
	for _, m := range matches {
		if m.EndX != m.StartX+1 || m.StartY != m.EndY {
			t.Errorf("match %+v does not span two cells on one row", m)
		}
	}
	if _, err := search.Select(SearchDirectionNext, SearchScrollNone); err != nil {
		t.Fatal(err)
	}
	if sel, ok := search.SelectedMatch(); !ok || sel.EndX != sel.StartX+1 {
		t.Errorf("SelectedMatch() = %+v, %v", sel, ok)
	}
}

// A named color's default is what the terminal shows before a program or the
// embedder overrides it, so a palette editor can offer "reset to default".
func TestColorNameDefault(t *testing.T) {
	rgb, ok := ColorNameRed.Default()
	if !ok {
		t.Fatal("ColorNameRed.Default() reported no default")
	}
	if rgb.Uint32() != 0xCC6666 {
		t.Errorf("ColorNameRed.Default() = %#06x, want 0xcc6666", rgb)
	}
	if rgb, ok := ColorNameBrightWhite.Default(); !ok || rgb.Uint32() != 0xEAEAEA {
		t.Errorf("ColorNameBrightWhite.Default() = %#06x, %v; want 0xeaeaea, true", rgb, ok)
	}
	// The enum is open: every other palette index is a valid value, and those
	// take their color from the palette rather than from a default.
	if got, ok := ColorName(200).Default(); ok {
		t.Errorf("ColorName(200).Default() = %#06x, true; want false", got)
	}
}
