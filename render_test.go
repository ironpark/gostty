package gostty

import "testing"

// A frame is one Update plus one Cells crossing. Colors come back resolved:
// the palette lookup and the default foreground/background fill happen in Zig,
// so Go never carries a palette.
func TestRenderCells(t *testing.T) {
	term, stream := newStreamPair(t, 10, 3)

	state, err := NewRenderState()
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	defer state.Close()

	feed(t, stream, "\x1b[31;1mhi\x1b[m\x1b[2;1mok")
	if err := state.Update(term); err != nil {
		t.Fatalf("Update: %v", err)
	}

	rows, err := state.Rows()
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	cols, err := state.Cols()
	if err != nil {
		t.Fatalf("Cols: %v", err)
	}
	if rows != 3 || cols != 10 {
		t.Fatalf("viewport = %dx%d, want 10x3", cols, rows)
	}

	n, err := state.CellCount()
	if err != nil {
		t.Fatalf("CellCount: %v", err)
	}
	if n != uint(rows)*uint(cols) {
		t.Fatalf("CellCount = %d, want %d", n, uint(rows)*uint(cols))
	}

	cells := make([]RenderCell, n)
	written, err := state.Cells(cells)
	if err != nil {
		t.Fatalf("Cells: %v", err)
	}
	if written != n {
		t.Fatalf("Cells wrote %d, want %d", written, n)
	}

	if cells[0].Codepoint != 'h' || cells[1].Codepoint != 'i' {
		t.Errorf("row 0 = %q%q, want \"hi\"", cells[0].Codepoint, cells[1].Codepoint)
	}
	// The flags word decodes into named fields, so nothing here has to know
	// which bit ghostty put bold in.
	if !CellFlagsFromBacking(cells[0].Flags).Bold {
		t.Errorf("flags = %+v, want bold set", CellFlagsFromBacking(cells[0].Flags))
	}
	// Palette index 1 is red; the render state resolves it for us.
	if cells[0].Fg == cells[0].Bg {
		t.Errorf("fg %#06x should differ from bg %#06x", cells[0].Fg, cells[0].Bg)
	}

	// A cell past the text keeps the terminal's defaults.
	fg, err := state.Foreground()
	if err != nil {
		t.Fatalf("Foreground: %v", err)
	}
	if cells[5].Codepoint != 0 || cells[5].Fg != fg {
		t.Errorf("blank cell = %+v, want empty with default fg %#06x", cells[5], fg)
	}
}

// A short buffer is refused rather than truncated.
func TestRenderCellsShortBuffer(t *testing.T) {
	term, _ := newStreamPair(t, 10, 3)
	state, err := NewRenderState()
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	defer state.Close()
	if err := state.Update(term); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := state.Cells(make([]RenderCell, 4)); err == nil {
		t.Fatal("Cells into a short buffer succeeded, want an error")
	}
}

// The cursor is reported in viewport coordinates, and is absent when scrolled
// out of view.
func TestRenderCursor(t *testing.T) {
	term, stream := newStreamPair(t, 10, 3)
	state, err := NewRenderState()
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	defer state.Close()

	feed(t, stream, "\x1b[2;4H")
	if err := state.Update(term); err != nil {
		t.Fatalf("Update: %v", err)
	}
	x, ok, err := state.CursorX()
	if err != nil || !ok || x != 3 {
		t.Errorf("CursorX() = %d, %v, %v; want 3, true, nil", x, ok, err)
	}
	y, ok, err := state.CursorY()
	if err != nil || !ok || y != 1 {
		t.Errorf("CursorY() = %d, %v, %v; want 1, true, nil", y, ok, err)
	}
	if visible, err := state.CursorVisible(); err != nil || !visible {
		t.Errorf("CursorVisible() = %v, %v; want true", visible, err)
	}
}

// A drag selection is set in viewport coordinates and shows up two ways: as the
// text through the screen, and as a per-cell flag through the render state.
func TestRenderSelection(t *testing.T) {
	term, stream := newStreamPair(t, 10, 3)
	feed(t, stream, "abcdef")

	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	ok, err := screen.SelectRange(1, 0, 3, 0, false)
	if err != nil || !ok {
		t.Fatalf("SelectRange = %v, %v; want true, nil", ok, err)
	}

	text, ok, err := screen.SelectionString()
	if err != nil || !ok || text != "bcd" {
		t.Errorf("SelectionString = %q, %v, %v; want \"bcd\"", text, ok, err)
	}

	state, err := NewRenderState()
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	defer state.Close()
	if err := state.Update(term); err != nil {
		t.Fatalf("Update: %v", err)
	}
	cells := make([]RenderCell, 30)
	if _, err := state.Cells(cells); err != nil {
		t.Fatalf("Cells: %v", err)
	}

	for x, want := range []bool{false, true, true, true, false, false} {
		if got := CellFlagsFromBacking(cells[x].Flags).Selected; got != want {
			t.Errorf("cell %d selected = %v, want %v", x, got, want)
		}
	}

	// Out of the viewport is a refusal, not a panic: that is what a drag
	// leaving the window looks like.
	if ok, err := screen.SelectRange(0, 0, 0, 99, false); err != nil || ok {
		t.Errorf("SelectRange past the viewport = %v, %v; want false, nil", ok, err)
	}
}

// A Hangul syllable occupies two columns: the cell carrying the codepoint is
// marked wide, and the one after it is a spacer the renderer must skip rather
// than draw.
func TestRenderWideCells(t *testing.T) {
	term, stream := newStreamPair(t, 10, 2)
	feed(t, stream, "안녕")

	state, err := NewRenderState()
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	defer state.Close()
	if err := state.Update(term); err != nil {
		t.Fatalf("Update: %v", err)
	}
	cells := make([]RenderCell, 20)
	if _, err := state.Cells(cells); err != nil {
		t.Fatalf("Cells: %v", err)
	}

	want := []struct {
		codepoint rune
		wide      CellWidth
	}{
		{'안', CellWidthWide},
		{0, CellWidthSpacerTail},
		{'녕', CellWidthWide},
		{0, CellWidthSpacerTail},
		{0, CellWidthNarrow},
	}
	for i, w := range want {
		if got := rune(cells[i].Codepoint); got != w.codepoint {
			t.Errorf("cell %d codepoint = %q, want %q", i, got, w.codepoint)
		}
		if got := CellFlagsFromBacking(cells[i].Flags).Wide; got != w.wide {
			t.Errorf("cell %d wide = %v, want %v", i, got, w.wide)
		}
	}

	// The width the grid uses is the width the terminal assigns.
	if w, err := CodepointWidth('안'); err != nil || w != 2 {
		t.Errorf("CodepointWidth('안') = %d, %v; want 2", w, err)
	}
}

// The word boundaries a double click uses are the embedder's choice, not
// ghostty's: its own UI reads them from configuration.
var wordBoundaries = []uint32{0, ' ', '\t', '\'', '"', '`', '|', ':', ';', ',', '(', ')', '[', ']', '{', '}', '<', '>', '$'}

// Word and line selection are what a double and triple click do. ghostty owns
// what a "word" and a "line" are -- soft wraps, whitespace trimming, semantic
// prompt boundaries -- and hands back the selection.
func TestSelectWordAndLine(t *testing.T) {
	term, stream := newStreamPair(t, 40, 3)
	feed(t, stream, "  hello world  ")

	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}

	// Column 4 is inside "hello".
	ok, err := screen.SelectWord(4, 0, wordBoundaries)
	if err != nil || !ok {
		t.Fatalf("SelectWord = %v, %v; want true, nil", ok, err)
	}
	if text, ok, err := screen.SelectionString(); err != nil || !ok || text != "hello" {
		t.Errorf("word = %q, %v, %v; want \"hello\"", text, ok, err)
	}

	// A line selection covers both words and trims the surrounding blanks.
	ok, err = screen.SelectLine(4, 0)
	if err != nil || !ok {
		t.Fatalf("SelectLine = %v, %v; want true, nil", ok, err)
	}
	if text, ok, err := screen.SelectionString(); err != nil || !ok || text != "hello world" {
		t.Errorf("line = %q, %v, %v; want \"hello world\"", text, ok, err)
	}

	// A boundary character is a run of its own rather than nothing: double
	// clicking the gap selects the gap, which is what every terminal does.
	if ok, err := screen.SelectWord(0, 0, wordBoundaries); err != nil || !ok {
		t.Fatalf("SelectWord on a blank = %v, %v; want true, nil", ok, err)
	}
	// The text of that run comes back empty: ghostty trims trailing whitespace
	// when it dumps a region, and a run of blanks is all trailing whitespace.
	if text, _, err := screen.SelectionString(); err != nil || len(text) != 0 {
		t.Errorf("blank run = %q, %v; want empty", text, err)
	}

	// Past the viewport is a refusal rather than a panic.
	if ok, err := screen.SelectLine(0, 99); err != nil || ok {
		t.Errorf("SelectLine past the viewport = %v, %v; want false, nil", ok, err)
	}
}

// Output selection needs the shell to mark its prompts with OSC 133. Without
// the marks there is no block to find; with them the command's output comes
// back on its own.
func TestSelectOutput(t *testing.T) {
	term, stream := newStreamPair(t, 40, 6)

	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	feed(t, stream, "plain text\r\n")
	if ok, err := screen.SelectOutput(0, 0); err != nil || ok {
		t.Errorf("SelectOutput without prompt marks = %v, %v; want false, nil", ok, err)
	}

	// A prompt, the command, then its output.
	feed(t, stream, "\x1b]133;A\x07$ \x1b]133;B\x07ls\x1b]133;C\x07\r\nout one\r\nout two\r\n\x1b]133;D\x07")
	ok, err := screen.SelectOutput(0, 2)
	if err != nil {
		t.Fatalf("SelectOutput: %v", err)
	}
	if !ok {
		t.Fatal("SelectOutput found no block on a marked output row")
	}
	text, _, err := screen.SelectionString()
	if err != nil {
		t.Fatalf("SelectionString: %v", err)
	}
	if got := text; got != "out one\nout two" {
		t.Errorf("output = %q, want %q", got, "out one\nout two")
	}
}

// Whether the viewport sits at the bottom is what decides if new output should
// scroll into view. ghostty takes the screen by value here, so it is reached
// through a wrapper.
func TestViewportIsBottom(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	if bottom, err := screen.ViewportIsBottom(); err != nil || !bottom {
		t.Errorf("a fresh screen reports %v, %v; want true", bottom, err)
	}

	// Push rows into the scrollback, then look at them.
	feed(t, stream, "one\r\ntwo\r\nthree\r\nfour\r\nfive\r\n")
	if err := term.ScrollViewport(ScrollViewportTop()); err != nil {
		t.Fatalf("ScrollViewport: %v", err)
	}
	if bottom, err := screen.ViewportIsBottom(); err != nil || bottom {
		t.Errorf("after scrolling up, reports %v, %v; want false", bottom, err)
	}
	if err := term.ScrollViewport(ScrollViewportBottom()); err != nil {
		t.Fatalf("ScrollViewport: %v", err)
	}
	if bottom, err := screen.ViewportIsBottom(); err != nil || !bottom {
		t.Errorf("after scrolling back, reports %v, %v; want true", bottom, err)
	}
}

// The scrollback is a viewport position, not a pixel offset: scrolling moves
// what the render state hands back, so a renderer that draws the cells it is
// given follows the scrollback without tracking anything itself.
func TestRenderStateFollowsViewport(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	for _, line := range []string{"one", "two", "three", "four", "five"} {
		feed(t, stream, line+"\r\n")
	}
	state, err := NewRenderState()
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	defer state.Close()

	topRow := func() string {
		t.Helper()
		if err := state.Update(term); err != nil {
			t.Fatalf("Update: %v", err)
		}
		n, err := state.CellCount()
		if err != nil {
			t.Fatalf("CellCount: %v", err)
		}
		cells := make([]RenderCell, n)
		if _, err := state.Cells(cells); err != nil {
			t.Fatalf("Cells: %v", err)
		}
		cols, err := state.Cols()
		if err != nil {
			t.Fatalf("Cols: %v", err)
		}
		var out []rune
		for _, cell := range cells[:cols] {
			if cell.Codepoint > ' ' {
				out = append(out, rune(cell.Codepoint))
			}
		}
		return string(out)
	}

	if got := topRow(); got != "four" {
		t.Errorf("top row at the bottom of the scrollback = %q, want %q", got, "four")
	}
	// A negative delta is up, which is the direction a wheel notch away from
	// the user means.
	if err := term.ScrollViewport(ScrollViewportDelta(-1)); err != nil {
		t.Fatalf("ScrollViewport: %v", err)
	}
	if got := topRow(); got != "three" {
		t.Errorf("top row after scrolling up one = %q, want %q", got, "three")
	}
	if err := term.ScrollViewport(ScrollViewportBottom()); err != nil {
		t.Fatalf("ScrollViewport: %v", err)
	}
	if got := topRow(); got != "four" {
		t.Errorf("top row after scrolling back to the bottom = %q, want %q", got, "four")
	}
}
