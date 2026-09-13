package main

import (
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// tabSearch is the tab's side of a scrollback search. The query and the count
// the user sees belong to the search bar in ui; this is the native handle
// behind it and the highlight the grid is drawn with.
type tabSearch struct {
	// The native search, nil when nothing is being looked for. It holds
	// positions in the scrollback, so a new query means a new one.
	handle *gostty.Search
	// Matches in the viewport, one bool per cell, refreshed with the cells.
	cells    []bool
	previous []bool             // the last mask, kept until its rows are marked for redraw
	viewport []gostty.Selection // scratch for ViewportMatches
}

// How many times a frame the scan is pushed forward: enough that a normal
// scrollback finishes in a frame or two, small enough that a huge one leaves
// time to draw.
const searchTicksPerFrame = 16

// closeSearch releases the handle before the terminal it belongs to. Searches
// are not adopted by the Session because every query replaces one.
func (tab *terminal) closeSearch() {
	if tab.search.handle != nil {
		_ = tab.search.handle.Close()
		tab.search.handle = nil
	}
	tab.panels.Search.Matches = 0
	tab.panels.Search.Failure = ""
}

// runSearch starts a search for the current query. The scan itself is driven
// a slice at a time from tickSearch, so a long scrollback does not stall the
// keystroke that started it.
func (tab *terminal) runSearch() error {
	tab.closeSearch()
	if len(tab.panels.Search.Query) == 0 {
		return nil
	}
	search, err := tab.vt.NewSearch(string(tab.panels.Search.Query))
	if err != nil {
		// A needle the search cannot take is the user's to see, not a reason
		// to stop the terminal.
		tab.panels.Search.Failure = err.Error()
		return nil
	}
	tab.search.handle = search
	return nil
}

// tickSearch pushes the scan forward by one frame's budget and refreshes the
// count. Feed hands it new scrollback and notices the viewport moved.
func (tab *terminal) tickSearch() error {
	if tab.search.handle == nil {
		return nil
	}
	if err := tab.search.handle.Feed(true); err != nil {
		return err
	}
	for range searchTicksPerFrame {
		progress, err := tab.search.handle.Tick()
		if err != nil {
			return err
		}
		if progress != gostty.SearchProgressProgress {
			break
		}
	}
	var err error
	tab.panels.Search.Matches, err = tab.search.handle.MatchCount()
	return err
}

// refreshMatches marks the viewport cells covered by a match. ViewportMatches
// rather than Matches: only what is on screen can be highlighted, and it
// answers before the scan has finished. Matches are in screen coordinates,
// which the viewport's top row turns into cells.
func (tab *terminal) refreshMatches() error {
	g := tab.grid()
	if tab.search.handle == nil {
		tab.search.resetMask(0)
		tab.markMatchChanges(g)
		return nil
	}
	top, err := tab.viewportTop()
	if err != nil {
		return err
	}
	// A match spans at least one cell, so one per viewport cell is enough.
	tab.search.viewport = grow(tab.search.viewport, len(tab.frame.cells))
	n, err := tab.search.handle.ViewportMatches(tab.search.viewport)
	if err != nil {
		return err
	}
	tab.search.resetMask(len(tab.frame.cells))
	defer tab.markMatchChanges(g)
	for _, m := range tab.search.viewport[:n] {
		if m.StartY < top || m.EndY >= top+uint32(g.rows) || m.StartY > m.EndY {
			continue
		}
		for y := m.StartY; y <= m.EndY; y++ {
			x0, x1 := 0, g.cols-1
			if y == m.StartY {
				x0 = int(m.StartX)
			}
			if y == m.EndY {
				x1 = int(m.EndX)
			}
			for x := x0; x <= x1 && x < g.cols; x++ {
				tab.search.cells[g.index(x, int(y-top))] = true
			}
		}
	}
	return nil
}

// resetMask swaps the masks and clears the new one, keeping the old
// highlights until their rows have been marked for redraw.
func (s *tabSearch) resetMask(size int) {
	s.cells, s.previous = s.previous, s.cells
	s.cells = grow(s.cells, size)
	clear(s.cells)
}

// markMatchChanges repaints the rows whose highlight differs from the last
// frame. The highlight is drawn here, not by the terminal, so its dirty
// flags do not know about it.
func (tab *terminal) markMatchChanges(g grid) {
	if g.cols == 0 {
		return
	}
	prev := tab.search.previous
	for i := range max(len(prev), len(tab.search.cells)) {
		was := i < len(prev) && prev[i]
		is := i < len(tab.search.cells) && tab.search.cells[i]
		if was != is {
			tab.frame.redraw.mark(g.row(i))
		}
	}
}

// moveMatch steps to the next or previous match. The match becomes the
// screen's selection, so it draws in the selection colours and copies with
// the usual gesture, and the viewport jumps to it only when it is off screen.
func (tab *terminal) moveMatch(dir gostty.SearchDirection) error {
	if tab.search.handle == nil || tab.panels.Search.Matches == 0 {
		return nil
	}
	ok, err := tab.search.handle.Select(dir, gostty.SearchScrollNone)
	if err != nil || !ok {
		return err
	}
	match, ok, err := tab.search.handle.SelectedMatch()
	if err != nil || !ok {
		return err
	}
	if err := tab.applySelection(match, true); err != nil {
		return err
	}
	return tab.revealRow(match.StartY)
}

// applyUIActions does the half of a panel result that is the terminal's: the
// native search. A settings step is the window's, since it reaches every tab.
func (tab *terminal) applyUIActions(result ui.Actions) (bool, error) {
	if result.ResetSearch {
		tab.closeSearch()
	}
	if result.QueryChanged {
		if err := tab.runSearch(); err != nil {
			return true, err
		}
	}
	if result.MatchStep != 0 {
		direction := gostty.SearchDirectionNext
		if result.MatchStep < 0 {
			direction = gostty.SearchDirectionPrev
		}
		if err := tab.moveMatch(direction); err != nil {
			return true, err
		}
	}
	return result.Consumed, nil
}
