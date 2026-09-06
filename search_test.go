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
	s, err := term.NewSearch(needle)
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.All(); err != nil {
		t.Fatalf("All: %v", err)
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
	if ok, err := s.Select(SearchDirectionNext, SearchScrollNone); err != nil || ok {
		t.Errorf("Select() with no matches = %v, %v; want false, nil", ok, err)
	}
}

// Select moves the search's position and nothing else; the match comes out
// as a value the UI puts in the screen's selection itself.
func TestSearchSelect(t *testing.T) {
	term := newTerm(t, 20, 6)
	writeLines(t, term, "one needle", "two", "three needle")

	sc, s := newSearchOn(t, term, "needle")
	if n, err := s.MatchCount(); err != nil || n != 2 {
		t.Fatalf("MatchCount() = %d, %v; want 2, nil", n, err)
	}

	ok, err := s.Select(SearchDirectionNext, SearchScrollNone)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !ok {
		t.Fatal("Select() = false with matches present")
	}
	if has, err := sc.HasSelection(); err != nil || has {
		t.Fatalf("HasSelection() after Select = %v, %v; want false: Select does not touch the screen", has, err)
	}
	match, ok, err := s.SelectedMatch()
	if err != nil || !ok {
		t.Fatalf("SelectedMatch() = ok %v, %v", ok, err)
	}
	if ok, err := sc.SetSelection(match); err != nil || !ok {
		t.Fatalf("SetSelection(match) = %v, %v", ok, err)
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
	s, err := term.NewSearch("needle")
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
	s, err := term.NewSearch("x")
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
	first, err := term.NewSearch("a")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	second, err := term.NewSearch("b")
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

	search, err := term.NewSearch("needle")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	defer search.Close()
	if err := search.All(); err != nil {
		t.Fatalf("All: %v", err)
	}
	if n, err := search.MatchCount(); err != nil {
		t.Fatalf("MatchCount: %v", err)
	} else if n != 1 {
		t.Fatalf("matches = %d, want 1", n)
	}

	if ok, err := search.Select(SearchDirectionNext, SearchScrollNone); err != nil {
		t.Fatalf("Select: %v", err)
	} else if !ok {
		t.Fatal("Select found nothing")
	}
	// Select leaves the viewport where it was; showing the match is the
	// caller's move, made from the match's screen row.
	if bottom, err := screen.ViewportIsBottom(); err != nil || !bottom {
		t.Fatalf("ViewportIsBottom() after Select = %v, %v; want true", bottom, err)
	}
	match, ok, err := search.SelectedMatch()
	if err != nil || !ok {
		t.Fatalf("SelectedMatch() = ok %v, %v", ok, err)
	}
	if err := term.ScrollViewport(ScrollViewportRow(uint(match.StartY))); err != nil {
		t.Fatalf("ScrollViewport: %v", err)
	}
	if bottom, _ := screen.ViewportIsBottom(); bottom {
		t.Error("the viewport is still at the bottom after scrolling to the match")
	}
	if top, _ := screen.ViewportTop(); top != match.StartY {
		t.Errorf("ViewportTop() = %d after ScrollViewportRow(%d); the row is a screen row", top, match.StartY)
	}
	if ok, err := screen.SetSelection(match); err != nil || !ok {
		t.Fatalf("SetSelection(match) = %v, %v", ok, err)
	}
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
	search, err := term.NewSearch("needle")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	defer search.Close()
	if err := search.All(); err != nil {
		t.Fatalf("All: %v", err)
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
		ok, err := search.Select(dir, SearchScrollNone)
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if !ok {
			t.Fatal("Select found nothing")
		}
		match, ok, err := search.SelectedMatch()
		if err != nil || !ok {
			t.Fatalf("SelectedMatch() = ok %v, %v", ok, err)
		}
		if err := term.ScrollViewport(ScrollViewportRow(uint(match.StartY))); err != nil {
			t.Fatalf("ScrollViewport: %v", err)
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

// The incremental drive: Feed reads the terminal, Tick advances without it,
// and the status says when there is nothing left. A UI spends a slice of each
// frame here instead of blocking in All.
func TestSearchIncremental(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	for range 40 {
		feed(t, stream, "needle\r\n")
	}
	s, err := term.NewSearch("needle")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	defer s.Close()

	// A search that has never been driven reports feed_required, whatever the
	// constructor's own feed already found.
	if state, err := s.Status(); err != nil {
		t.Fatalf("Status: %v", err)
	} else if state == SearchStateComplete {
		t.Fatal("Status() = complete before any tick")
	}

	ticks := 0
	for range 1000 {
		state, err := s.Status()
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if state == SearchStateComplete {
			break
		}
		if _, err := s.Tick(); err != nil {
			t.Fatalf("Tick: %v", err)
		}
		if err := s.Feed(true); err != nil {
			t.Fatalf("Feed: %v", err)
		}
		ticks++
	}
	if state, err := s.Status(); err != nil || state != SearchStateComplete {
		t.Fatalf("Status() = %v, %v after %d ticks; want complete", state, err, ticks)
	}
	n, err := s.MatchCount()
	if err != nil {
		t.Fatalf("MatchCount: %v", err)
	}
	if n != 40 {
		t.Errorf("MatchCount() = %d, want 40", n)
	}
}

// ViewportMatches is the highlight read: it answers from the viewport's own
// searcher, so it is there before the scrollback scan has been driven at all.
//
// It reports matches on the pages the viewport covers, not strictly the rows
// on screen, so a small terminal whose whole scrollback is one page sees all
// of them. What it never does is walk the result list.
func TestSearchViewportMatches(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	for range 40 {
		feed(t, stream, "needle\r\n")
	}
	s, err := term.NewSearch("needle")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	defer s.Close()

	dst := make([]Selection, 64)
	n, err := s.ViewportMatches(dst)
	if err != nil {
		t.Fatalf("ViewportMatches: %v", err)
	}
	if n == 0 {
		t.Fatal("ViewportMatches() = 0 before any tick; the viewport is searched on its own")
	}
	// Every match is six cells wide on one row, wherever it landed.
	for _, m := range dst[:n] {
		if m.StartY != m.EndY || m.EndX != m.StartX+5 {
			t.Errorf("match %+v does not span %q on one row", m, "needle")
		}
	}
	if err := s.All(); err != nil {
		t.Fatalf("All: %v", err)
	}
	total, err := s.MatchCount()
	if err != nil {
		t.Fatalf("MatchCount: %v", err)
	}
	if uint(n) > total {
		t.Errorf("ViewportMatches() = %d, more than the %d total", n, total)
	}
}

// A short dst is not an error: it fills and stops, which is all a renderer
// with a fixed scratch buffer needs.
func TestSearchViewportMatchesShortBuffer(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	for range 10 {
		feed(t, stream, "needle\r\n")
	}
	s, err := term.NewSearch("needle")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	defer s.Close()
	dst := make([]Selection, 2)
	if n, err := s.ViewportMatches(dst); err != nil || n != 2 {
		t.Errorf("ViewportMatches(len 2) = %d, %v; want 2, nil", n, err)
	}
}

// The selected index is what a UI shows as "3 of 12". It counts in the order
// Matches writes, so index 0 is the match nearest the prompt.
func TestSearchSelectedIndex(t *testing.T) {
	term := newTerm(t, 20, 6)
	writeLines(t, term, "one needle", "two needle", "three needle")
	_, s := newSearchOn(t, term, "needle")

	if _, ok, err := s.SelectedIndex(); err != nil || ok {
		t.Fatalf("SelectedIndex() before Select = ok %v, %v; want false", ok, err)
	}
	for want := range uint(3) {
		if ok, err := s.Select(SearchDirectionNext, SearchScrollNone); err != nil || !ok {
			t.Fatalf("Select: %v, %v", ok, err)
		}
		got, ok, err := s.SelectedIndex()
		if err != nil || !ok {
			t.Fatalf("SelectedIndex() = ok %v, %v", ok, err)
		}
		if got != want {
			t.Errorf("SelectedIndex() = %d, want %d", got, want)
		}
	}
}

// Results are kept per screen, so a search survives a program taking over the
// alternate screen and giving it back.
func TestSearchAcrossScreens(t *testing.T) {
	term, stream := newStreamPair(t, 20, 4)
	feed(t, stream, "needle here\r\n")
	s, err := term.NewSearch("needle")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	defer s.Close()
	if err := s.All(); err != nil {
		t.Fatalf("All: %v", err)
	}
	if n, err := s.MatchCount(); err != nil || n != 1 {
		t.Fatalf("MatchCount() on the primary screen = %d, %v; want 1", n, err)
	}

	// Into the alternate screen, which has none of that text.
	feed(t, stream, "\x1b[?1049h")
	if err := s.All(); err != nil {
		t.Fatalf("All on the alternate screen: %v", err)
	}
	if n, err := s.MatchCount(); err != nil || n != 0 {
		t.Fatalf("MatchCount() on the alternate screen = %d, %v; want 0", n, err)
	}

	// And back. The primary screen's result is still there.
	feed(t, stream, "\x1b[?1049l")
	if err := s.All(); err != nil {
		t.Fatalf("All back on the primary screen: %v", err)
	}
	if n, err := s.MatchCount(); err != nil || n != 1 {
		t.Errorf("MatchCount() back on the primary screen = %d, %v; want 1", n, err)
	}
}

// Select with `if_needed` brings the viewport to a match in the scrollback,
// which is the whole reason the scroll policy is a parameter.
func TestSearchSelectScrolls(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "needle here\r\n")
	for range 20 {
		feed(t, stream, "filler\r\n")
	}
	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	s, err := term.NewSearch("needle")
	if err != nil {
		t.Fatalf("NewSearch: %v", err)
	}
	defer s.Close()
	if err := s.All(); err != nil {
		t.Fatalf("All: %v", err)
	}
	if ok, err := s.Select(SearchDirectionNext, SearchScrollIfNeeded); err != nil || !ok {
		t.Fatalf("Select = %v, %v", ok, err)
	}
	if bottom, err := screen.ViewportIsBottom(); err != nil || bottom {
		t.Errorf("ViewportIsBottom() after Select(IfNeeded) = %v, %v; want false", bottom, err)
	}
}
