package gostty

import (
	"errors"
	"testing"
)

func newGridRef(t *testing.T, term *Terminal, tag PointTag, x uint16, y uint32) *GridRef {
	t.Helper()
	ref, err := term.NewGridRef(tag, x, y)
	if err != nil {
		t.Fatalf("NewGridRef(%v, %d, %d): %v", tag, x, y, err)
	}
	t.Cleanup(func() { ref.Close() })
	return ref
}

// A reference made on the active area keeps naming its cell as output
// pushes the cell into the scrollback: the active position is gone, the
// screen position is unchanged, and the cell reads the same.
func TestGridRefFollowsCellIntoScrollback(t *testing.T) {
	term, stream := newStreamPair(t, 5, 2)
	feed(t, stream, "A")
	ref := newGridRef(t, term, PointTagActive, 0, 0)

	feed(t, stream, "\r\nB\r\nC")

	if ok, err := ref.HasValue(); err != nil || !ok {
		t.Fatalf("HasValue = %v, %v; want true", ok, err)
	}
	if pt, ok, err := ref.Point(PointTagScreen); err != nil || !ok || pt != (GridPoint{X: 0, Y: 0}) {
		t.Errorf("Point(screen) = %+v, %v, %v; want {0 0}", pt, ok, err)
	}
	if _, ok, err := ref.Point(PointTagActive); err != nil || ok {
		t.Errorf("Point(active) = ok %v, %v; want none, the row scrolled out", ok, err)
	}
	if pt, ok, err := ref.Point(PointTagHistory); err != nil || !ok || pt != (GridPoint{X: 0, Y: 0}) {
		t.Errorf("Point(history) = %+v, %v, %v; want {0 0}", pt, ok, err)
	}
	cell, ok, err := ref.Cell()
	if err != nil || !ok || cell.Codepoint != 'A' {
		t.Errorf("Cell = %+v, %v, %v; want codepoint 'A'", cell, ok, err)
	}
	var buf [4]rune
	if n, err := ref.Graphemes(buf[:]); err != nil || n != 1 || buf[0] != 'A' {
		t.Errorf("Graphemes = %d, %v, %v; want 1 'A'", n, err, buf[:n])
	}
}

// A reset discards every page, so the reference becomes empty, and Set
// points it somewhere new.
func TestGridRefEmptyAfterResetThenSet(t *testing.T) {
	term, stream := newStreamPair(t, 5, 2)
	feed(t, stream, "A")
	ref := newGridRef(t, term, PointTagActive, 0, 0)

	if err := term.FullReset(); err != nil {
		t.Fatal(err)
	}
	if ok, err := ref.HasValue(); err != nil || ok {
		t.Fatalf("HasValue after reset = %v, %v; want false", ok, err)
	}
	if _, ok, err := ref.Cell(); err != nil || ok {
		t.Errorf("Cell after reset = ok %v, %v; want none", ok, err)
	}
	if _, ok, err := ref.Point(PointTagScreen); err != nil || ok {
		t.Errorf("Point after reset = ok %v, %v; want none", ok, err)
	}
	if n, err := ref.Graphemes(make([]rune, 4)); err != nil || n != 0 {
		t.Errorf("Graphemes after reset = %d, %v; want 0", n, err)
	}

	feed(t, stream, "Z")
	if ok, err := ref.Set(PointTagActive, 0, 0); err != nil || !ok {
		t.Fatalf("Set = %v, %v; want true", ok, err)
	}
	if cell, ok, err := ref.Cell(); err != nil || !ok || cell.Codepoint != 'Z' {
		t.Errorf("Cell after Set = %+v, %v, %v; want 'Z'", cell, ok, err)
	}
	// A Set that misses leaves the reference where it was.
	if ok, err := ref.Set(PointTagActive, 50, 50); err != nil || ok {
		t.Errorf("Set off grid = %v, %v; want false", ok, err)
	}
	if ok, err := ref.HasValue(); err != nil || !ok {
		t.Errorf("HasValue after failed Set = %v, %v; want true", ok, err)
	}
}

// Pruned scrollback empties the reference too.
func TestGridRefEmptyAfterScrollbackPrune(t *testing.T) {
	term, stream := newStreamPair(t, 5, 2)
	if err := term.SetScrollbackMaxLines(1); err != nil {
		t.Fatal(err)
	}
	feed(t, stream, "A")
	ref := newGridRef(t, term, PointTagActive, 0, 0)
	// A limit is enforced a page at a time, so write well past one page of
	// rows. (A byte limit of zero is different: it turns scrollback off, and
	// rows are then erased in place rather than pruned.)
	for range 30000 {
		feed(t, stream, "\r\nx")
	}
	if ok, err := ref.HasValue(); err != nil || ok {
		t.Errorf("HasValue after prune = %v, %v; want false", ok, err)
	}
}

// A reference made on one screen stays with that screen.
func TestGridRefStaysWithItsScreen(t *testing.T) {
	term, stream := newStreamPair(t, 5, 2)
	feed(t, stream, "A")
	ref := newGridRef(t, term, PointTagActive, 0, 0)

	feed(t, stream, "\x1b[?1049hB")
	if cell, ok, err := ref.Cell(); err != nil || !ok || cell.Codepoint != 'A' {
		t.Errorf("Cell on alternate = %+v, %v, %v; want the primary screen's 'A'", cell, ok, err)
	}
	feed(t, stream, "\x1b[?1049l")
	if cell, ok, err := ref.Cell(); err != nil || !ok || cell.Codepoint != 'A' {
		t.Errorf("Cell back on primary = %+v, %v, %v; want 'A'", cell, ok, err)
	}
}

func TestGridRefOutOfBounds(t *testing.T) {
	term := newTerm(t, 5, 2)
	_, err := term.NewGridRef(PointTagActive, 10, 0)
	if !errors.Is(err, ErrOutOfBounds) {
		t.Errorf("NewGridRef off grid = %v; want ErrOutOfBounds", err)
	}
}

// The reference closes before the terminal; the other order is refused.
func TestGridRefCloseOrder(t *testing.T) {
	term, err := NewTerminal(5, 2)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := term.NewGridRef(PointTagActive, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := term.Close(); !errors.Is(err, ErrHandleInUse) {
		t.Errorf("Close terminal with a live GridRef = %v; want ErrHandleInUse", err)
	}
	if err := ref.Close(); err != nil {
		t.Errorf("Close ref: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Errorf("Close terminal: %v", err)
	}
	if _, err := ref.HasValue(); !errors.Is(err, ErrInvalidHandle) {
		t.Errorf("HasValue after Close = %v; want ErrInvalidHandle", err)
	}
}

// Styled cells come out with their colors resolved, and a grapheme cluster
// comes out whole.
func TestGridRefCellStyleAndGraphemes(t *testing.T) {
	term, stream := newStreamPair(t, 10, 2)
	feed(t, stream, "\x1b[1;38;2;255;136;0mé\x1b[m \x1b]8;;https://example.com\x1b\\L\x1b]8;;\x1b\\")

	ref := newGridRef(t, term, PointTagActive, 0, 0)
	cell, ok, err := ref.Cell()
	if err != nil || !ok {
		t.Fatalf("Cell = %v, %v", ok, err)
	}
	if cell.Codepoint != 'e' || cell.Fg != 0xFF8800 || !cell.Flags.Bold {
		t.Errorf("Cell = %+v; want 'e', fg 0xFF8800, bold", cell)
	}
	var buf [4]rune
	if n, err := ref.Graphemes(buf[:]); err != nil || n != 2 || buf[0] != 'e' || buf[1] != 0x301 {
		t.Errorf("Graphemes = %d, %v, %v; want [e U+0301]", n, err, buf[:n])
	}
	if _, err := ref.Graphemes(buf[:1]); !errors.Is(err, ErrNoSpaceLeft) {
		t.Errorf("Graphemes short buffer = %v; want ErrNoSpaceLeft", err)
	}
	if _, ok, err := ref.HyperlinkUri(); err != nil || ok {
		t.Errorf("HyperlinkUri on plain cell = ok %v, %v; want none", ok, err)
	}

	link := newGridRef(t, term, PointTagActive, 2, 0)
	if uri, ok, err := link.HyperlinkUri(); err != nil || !ok || uri != "https://example.com" {
		t.Errorf("HyperlinkUri = %q, %v, %v", uri, ok, err)
	}
}

// The untracked reads answer the same questions for a one-off position.
func TestTerminalCellAtAndHyperlinkAt(t *testing.T) {
	term, stream := newStreamPair(t, 10, 2)
	feed(t, stream, "\x1b]8;;https://example.com\x1b\\ab\x1b]8;;\x1b\\c")

	if cell, ok, err := term.CellAt(PointTagViewport, 2, 0); err != nil || !ok || cell.Codepoint != 'c' {
		t.Errorf("CellAt(2,0) = %+v, %v, %v; want 'c'", cell, ok, err)
	}
	if _, ok, err := term.CellAt(PointTagViewport, 50, 50); err != nil || ok {
		t.Errorf("CellAt off grid = ok %v, %v; want none", ok, err)
	}
	if uri, ok, err := term.HyperlinkAt(PointTagScreen, 1, 0); err != nil || !ok || uri != "https://example.com" {
		t.Errorf("HyperlinkAt(1,0) = %q, %v, %v", uri, ok, err)
	}
	if _, ok, err := term.HyperlinkAt(PointTagScreen, 2, 0); err != nil || ok {
		t.Errorf("HyperlinkAt(2,0) = ok %v, %v; want none", ok, err)
	}
	if _, ok, err := term.HyperlinkAt(PointTagScreen, 50, 50); err != nil || ok {
		t.Errorf("HyperlinkAt off grid = ok %v, %v; want none", ok, err)
	}
}

func TestPointTagText(t *testing.T) {
	for _, tag := range []PointTag{PointTagActive, PointTagViewport, PointTagScreen, PointTagHistory} {
		text, err := tag.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var back PointTag
		if err := back.UnmarshalText(text); err != nil || back != tag {
			t.Errorf("%s round trip = %v, %v", text, back, err)
		}
	}
}
