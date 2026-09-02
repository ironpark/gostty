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
	s, err := sc.NewSearch([]byte(needle))
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
		if err := term.PrintString([]byte(line)); err != nil {
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
	if got, want := strings.TrimSpace(string(text)), "needle"; got != want {
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
	if err := term.PrintString([]byte("needle")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	sc, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	s, err := sc.NewSearch([]byte("needle"))
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
	s, err := sc.NewSearch([]byte("x"))
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
	first, err := sc.NewSearch([]byte("a"))
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	second, err := sc.NewSearch([]byte("b"))
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
