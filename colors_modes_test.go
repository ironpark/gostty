package gostty

import "testing"

// Colors come back packed as 0xRRGGBB, the same as RenderCell, and follow
// the OSC 10/11/12 changes a program makes.
func TestColors(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	if err := term.SetDefaultBackgroundColor(0x101010); err != nil {
		t.Fatal(err)
	}
	if bg, ok, err := term.BackgroundColor(); err != nil || !ok || bg != 0x101010 {
		t.Errorf("BackgroundColor() = %#x, %v, %v; want default 0x101010", bg, ok, err)
	}
	// OSC 11 overrides the default.
	feed(t, stream, "\x1b]11;rgb:ff/00/00\x1b\\")
	if bg, ok, err := term.BackgroundColor(); err != nil || !ok || bg != 0xff0000 {
		t.Errorf("BackgroundColor() after OSC 11 = %#x, %v, %v; want 0xff0000", bg, ok, err)
	}
	// OSC 111 resets to the default.
	feed(t, stream, "\x1b]111\x1b\\")
	if bg, _, _ := term.BackgroundColor(); bg != 0x101010 {
		t.Errorf("BackgroundColor() after OSC 111 = %#x; want 0x101010", bg)
	}

	// Palette entry 1 is red by default; OSC 4 changes it.
	before, err := term.PaletteColor(1)
	if err != nil {
		t.Fatal(err)
	}
	feed(t, stream, "\x1b]4;1;rgb:12/34/56\x1b\\")
	if c, _ := term.PaletteColor(1); c != 0x123456 || c == before {
		t.Errorf("PaletteColor(1) = %#x; want 0x123456", c)
	}
	palette := make([]uint32, 256)
	n, err := term.PaletteColors(palette)
	if err != nil || n != 256 || palette[1] != 0x123456 {
		t.Errorf("PaletteColors() = %d, %v, [1]=%#x", n, err, palette[1])
	}
	if n, _ := term.PaletteColors(palette[:16]); n != 16 {
		t.Errorf("PaletteColors(16) wrote %d", n)
	}
}

// Modes read the state the parser keeps, and can be set without a sequence.
func TestModes(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	if on, err := term.ModeEnabled(ModeCursorKeys); err != nil || on {
		t.Fatalf("ModeEnabled(cursor_keys) = %v, %v; want off", on, err)
	}
	feed(t, stream, "\x1b[?1h")
	if on, _ := term.ModeEnabled(ModeCursorKeys); !on {
		t.Error("DECCKM not reported after CSI ? 1 h")
	}
	if err := term.SetMode(ModeCursorKeys, false); err != nil {
		t.Fatal(err)
	}
	if on, _ := term.ModeEnabled(ModeCursorKeys); on {
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
	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := screen.Selection(); err != nil || ok {
		t.Fatalf("Selection() before selecting = ok %v, %v", ok, err)
	}
	if ok, err := screen.SetSelection(Selection{StartX: 1, StartY: 0, EndX: 2, EndY: 1}); err != nil || !ok {
		t.Fatalf("SetSelection = %v, %v", ok, err)
	}
	sel, ok, err := screen.Selection()
	if err != nil || !ok {
		t.Fatal(err)
	}
	if sel != (Selection{StartX: 1, StartY: 0, EndX: 2, EndY: 1}) {
		t.Errorf("Selection() = %+v", sel)
	}
	if text, ok, _ := screen.SelectionString(); !ok || text != "ello\nwor" {
		t.Errorf("SelectionString() = %q, %v", text, ok)
	}
	if top, err := screen.ViewportTop(); err != nil || top != 0 {
		t.Errorf("ViewportTop() = %d, %v", top, err)
	}
	if ok, _ := screen.SetSelection(Selection{StartX: 0, StartY: 99, EndX: 0, EndY: 99}); ok {
		t.Error("SetSelection outside the screen reported true")
	}
}

// Search matches come back as selections without moving the screen's own.
func TestSearchMatches(t *testing.T) {
	term, stream := newStreamPair(t, 10, 3)
	feed(t, stream, "ab ab\r\nab")
	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatal(err)
	}
	search, err := screen.NewSearch("ab")
	if err != nil {
		t.Fatal(err)
	}
	defer search.Close()
	if err := search.SearchAll(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := search.SelectedMatch(); err != nil || ok {
		t.Fatalf("SelectedMatch() before Select = ok %v, %v", ok, err)
	}
	count, err := search.MatchCount()
	if err != nil || count != 3 {
		t.Fatalf("MatchCount() = %d, %v; want 3", count, err)
	}
	matches := make([]Selection, count)
	n, err := search.Matches(matches)
	if err != nil || n != 3 {
		t.Fatalf("Matches() = %d, %v", n, err)
	}
	for _, m := range matches {
		if m.EndX != m.StartX+1 || m.StartY != m.EndY {
			t.Errorf("match %+v does not span two cells on one row", m)
		}
	}
	if _, err := search.Select(SearchDirectionNext); err != nil {
		t.Fatal(err)
	}
	if sel, ok, err := search.SelectedMatch(); err != nil || !ok || sel.EndX != sel.StartX+1 {
		t.Errorf("SelectedMatch() = %+v, %v, %v", sel, ok, err)
	}
}
