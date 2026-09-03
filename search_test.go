package gostty

import (
	"errors"
	"strings"
	"testing"
)

func newSearchOn(t *testing.T, term *Terminal, needle string) (*Screen, *Search) {
	t.Helper()
	sc, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	s, err := sc.NewSearch(needle)
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.SearchAll(); err != nil {
		t.Fatalf("SearchAll: %v", err)
	}
	return sc, s
}

func writeLines(t *testing.T, term *Terminal, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if err := term.PrintString(line); err != nil {
			t.Fatalf("PrintString: %v", err)
		}
		if err := term.Index(); err != nil {
			t.Fatalf("Index: %v", err)
		}
		if err := term.CarriageReturn(); err != nil {
			t.Fatalf("CarriageReturn: %v", err)
		}
	}
}

func TestSearchMatchCount(t *testing.T) {
	term := newTerm(t, 20, 6)
	writeLines(t, term, "alpha", "beta", "alpha again", "gamma")

	_, s := newSearchOn(t, term, "alpha")
	n, err := s.MatchCount()
	if err != nil {
		t.Fatalf("MatchCount: %v", err)
	}
	if n != 2 {
		t.Errorf("MatchCount() = %d, want 2", n)
	}
}

func TestSearchNoMatches(t *testing.T) {
	term := newTerm(t, 20, 4)
	writeLines(t, term, "alpha", "beta")

	_, s := newSearchOn(t, term, "nothing here")
	if n, err := s.MatchCount(); err != nil || n != 0 {
		t.Errorf("MatchCount() = %d, %v; want 0, nil", n, err)
	}
	if ok, err := s.Select(SearchDirectionNext); err != nil || ok {
		t.Errorf("Select() with no matches = %v, %v; want false, nil", ok, err)
	}
}

// Selecting a match puts it in the screen's selection, which is how the text
// comes back out.
func TestSearchSelect(t *testing.T) {
	term := newTerm(t, 20, 6)
	writeLines(t, term, "one needle", "two", "three needle")

	sc, s := newSearchOn(t, term, "needle")
	if n, err := s.MatchCount(); err != nil || n != 2 {
		t.Fatalf("MatchCount() = %d, %v; want 2, nil", n, err)
	}

	ok, err := s.Select(SearchDirectionNext)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !ok {
		t.Fatal("Select() = false with matches present")
	}
	if has, err := sc.HasSelection(); err != nil || !has {
		t.Fatalf("HasSelection() after Select = %v, %v; want true, nil", has, err)
	}
	text, ok, err := sc.SelectionString()
	if err != nil || !ok {
		t.Fatalf("SelectionString() = ok %v, err %v; want true, nil", ok, err)
	}
	if got, want := strings.TrimSpace(text), "needle"; got != want {
		t.Errorf("selected text = %q, want %q", got, want)
	}
}

// A search is a child of its screen, and the screen is borrowed from the
// terminal, so the reservation has to land on the terminal that actually owns
// the memory: closing it is refused while a search is open, and closing it
// after the search closes has to work.
func TestSearchKeepsTerminalOpen(t *testing.T) {
	term, err := NewTerminal(20, 4)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	if err := term.PrintString("needle"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	sc, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	s, err := sc.NewSearch("needle")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}

	if err := term.Close(); !errors.Is(err, ErrHandleInUse) {
		t.Fatalf("Close with an open search = %v, want ErrHandleInUse", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("search Close: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("terminal Close after the search closed: %v", err)
	}
}

// Closing a search without ever attempting to close the terminal first must
// leave the terminal's child count at zero, not below it.
func TestSearchChildCountThroughBorrowedScreen(t *testing.T) {
	term, err := NewTerminal(20, 4)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	sc, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	s, err := sc.NewSearch("x")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("search Close: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("terminal Close: %v", err)
	}
}

// Two searches on the same screen each hold the terminal open.
func TestMultipleSearches(t *testing.T) {
	term, err := NewTerminal(20, 4)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	sc, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	first, err := sc.NewSearch("a")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	second, err := sc.NewSearch("b")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := term.Close(); !errors.Is(err, ErrHandleInUse) {
		t.Fatalf("Close with one search still open = %v, want ErrHandleInUse", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("terminal Close after both searches closed: %v", err)
	}
}

// A match in the scrollback is no use to a UI that cannot see it, so selecting
// one brings the viewport to it.
func TestSearchScrollsToMatch(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "needle here\r\n")
	for range 20 {
		feed(t, stream, "filler\r\n")
	}
	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	if bottom, err := screen.ViewportIsBottom(); err != nil {
		t.Fatalf("ViewportIsBottom: %v", err)
	} else if !bottom {
		t.Fatal("the viewport did not start at the bottom")
	}

	search, err := screen.NewSearch("needle")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	defer search.Close()
	if err := search.SearchAll(); err != nil {
		t.Fatalf("SearchAll: %v", err)
	}
	if n, err := search.MatchCount(); err != nil {
		t.Fatalf("MatchCount: %v", err)
	} else if n != 1 {
		t.Fatalf("matches = %d, want 1", n)
	}

	if ok, err := search.Select(SearchDirectionNext); err != nil {
		t.Fatalf("Select: %v", err)
	} else if !ok {
		t.Fatal("Select found nothing")
	}

	// The match scrolled off long ago, so the viewport had to move to it.
	if bottom, err := screen.ViewportIsBottom(); err != nil {
		t.Fatalf("ViewportIsBottom: %v", err)
	} else if bottom {
		t.Error("the viewport is still at the bottom, so the match is off screen")
	}
	// And it is the screen's selection, so it draws as one.
	text, ok, err := screen.SelectionString()
	if err != nil {
		t.Fatalf("SelectionString: %v", err)
	}
	if !ok || text != "needle" {
		t.Errorf("selection = %q (ok=%v), want %q", text, ok, "needle")
	}
}

// Which way the two directions go, because the names do not say and a search UI
// has to bind them to keys: `next` starts at the match nearest the prompt and
// walks backwards in time, which is the direction a search through what already
// scrolled past goes. Both wrap.
func TestSearchDirectionOrder(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	// Three matches, each far enough apart to be on its own viewport.
	for _, tag := range []string{"AAA", "BBB", "CCC"} {
		feed(t, stream, tag+" needle\r\n")
		for range 10 {
			feed(t, stream, "filler\r\n")
		}
	}
	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	search, err := screen.NewSearch("needle")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	defer search.Close()
	if err := search.SearchAll(); err != nil {
		t.Fatalf("SearchAll: %v", err)
	}

	state, err := NewRenderState()
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	defer state.Close()
	// Which match is selected is read off the screen: selecting one scrolls to
	// it, and the tag on its line says which it was.
	at := func() string {
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

	step := func(dir SearchDirection) string {
		t.Helper()
		ok, err := search.Select(dir)
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if !ok {
			t.Fatal("Select found nothing")
		}
		return at()
	}

	for i, want := range []string{"CCCneedle", "BBBneedle", "AAAneedle", "CCCneedle"} {
		if got := step(SearchDirectionNext); got != want {
			t.Errorf("next %d landed on %q, want %q", i, got, want)
		}
	}
	for i, want := range []string{"AAAneedle", "BBBneedle", "CCCneedle"} {
		if got := step(SearchDirectionPrev); got != want {
			t.Errorf("prev %d landed on %q, want %q", i, got, want)
		}
	}
}
