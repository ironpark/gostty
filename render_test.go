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
	const bold = 1 << 0
	if cells[0].Flags&bold == 0 {
		t.Errorf("flags = %#x, want bold set", cells[0].Flags)
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
	if err != nil || !ok || string(text) != "bcd" {
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

	const selected = 1 << 12
	for x, want := range []bool{false, true, true, true, false, false} {
		if got := cells[x].Flags&selected != 0; got != want {
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

	const (
		widthNarrow = 0
		widthWide   = 1
		widthSpacer = 2
	)
	want := []struct {
		codepoint rune
		wide      uint8
	}{
		{'안', widthWide},
		{0, widthSpacer},
		{'녕', widthWide},
		{0, widthSpacer},
		{0, widthNarrow},
	}
	for i, w := range want {
		if got := rune(cells[i].Codepoint); got != w.codepoint {
			t.Errorf("cell %d codepoint = %q, want %q", i, got, w.codepoint)
		}
		if cells[i].Wide != w.wide {
			t.Errorf("cell %d wide = %d, want %d", i, cells[i].Wide, w.wide)
		}
	}

	// The width the grid uses is the width the terminal assigns.
	if w, err := CodepointWidth('안'); err != nil || w != 2 {
		t.Errorf("CodepointWidth('안') = %d, %v; want 2", w, err)
	}
}
