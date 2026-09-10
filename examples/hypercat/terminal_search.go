package main

import (
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// tabSearch is the tab's side of a scrollback search. The query and the count
// the user sees belong to the search bar, which is the UI's; this is the native
// handle behind it and the highlight the grid is drawn with.
type tabSearch struct {
	// The native search, nil when nothing is being looked for. It holds
	// positions in the scrollback, so a new query means a new one.
	handle *gostty.Search
	// Matches in the viewport, one bool per cell, refreshed with the cells so
	// the highlight never lags the text under it.
	cells    []bool
	previous []bool // reusable mask from the preceding refresh
	// Scratch for the viewport matches read each frame, kept so a frame with
	// highlights on screen does not allocate.
	viewport []gostty.Selection
}

// closeSearch releases the native handle before the terminal it belongs to.
func (tab *terminal) closeSearch() {
	if tab.search.handle != nil {
		_ = tab.search.handle.Close()
		tab.search.handle = nil
	}
	tab.panels.Search.Matches = 0
	tab.panels.Search.Failure = ""
}

// runSearch rebuilds the search for the current query.
//
// A search holds positions in the scrollback, so the query changing means a
// new one. It is not run here: the scan is driven a slice at a time from
// `tickSearch`, so a long scrollback does not stall the keystroke that
// started it.
func (tab *terminal) runSearch() error {
	tab.closeSearch()
	if len(tab.panels.Search.Query) == 0 {
		return nil
	}
	search, err := tab.vt.NewSearch(string(tab.panels.Search.Query))
	if err != nil {
		// A needle the search cannot take (too long for its window) is the
		// user's problem to see, not a reason to stop the terminal.
		tab.panels.Search.Failure = err.Error()
		return nil
	}
	tab.search.handle = search
	return nil
}

// tickSearch pushes the scan forward by one frame's worth and refreshes the
// count. `Tick` does not read the terminal, so most of the work here is the
// kind a real emulator would do off the IO thread; `Feed` is what hands it
// more scrollback and notices that the viewport moved.
func (tab *terminal) tickSearch() error {
	if tab.search.handle == nil {
		return nil
	}
	if err := tab.search.handle.Feed(true); err != nil {
		return err
	}
	// A budget rather than a loop to completion: whatever is not finished this
	// frame is finished on the next, and the matches already found are drawn
	// in the meantime.
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

// How many times a frame the search is pushed forward. Enough that a normal
// scrollback finishes in a frame or two, small enough that a huge one still
// leaves the frame time to draw.
const searchTicksPerFrame = 16

// refreshMatches marks the viewport cells covered by a search match.
//
// `ViewportMatches` rather than `Matches`: only the matches on screen can be
// highlighted, and asking for those is a cached read of the viewport alone,
// so it costs the same whether the scrollback holds ten matches or a million,
// and it answers before the scrollback scan has finished. Matches are in
// screen coordinates; the viewport's top row turns them into cells.
func (tab *terminal) refreshMatches() error {
	if tab.search.handle == nil {
		prev := tab.search.cells
		tab.search.cells = tab.search.cells[:0]
		tab.markMatchChanges(tab.grid(), prev)
		return nil
	}
	top, err := tab.viewportTop()
	if err != nil {
		return err
	}
	g := tab.grid()
	// The count is not known in advance. A match spans at least one cell, so
	// one per viewport cell cannot be exceeded by anything that is on screen.
	if cap(tab.search.viewport) < len(tab.frame.cells) {
		tab.search.viewport = make([]gostty.Selection, len(tab.frame.cells))
	}
	n, err := tab.search.handle.ViewportMatches(tab.search.viewport[:len(tab.frame.cells)])
	if err != nil {
		return err
	}
	prev := tab.search.resetMask(len(tab.frame.cells))
	defer tab.markMatchChanges(g, prev)
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

// resetMask swaps reusable masks and clears the new one, preserving the old
// highlights until their changed rows have been marked for redraw.
func (s *tabSearch) resetMask(size int) []bool {
	prev := s.cells
	s.cells, s.previous = s.previous, prev
	if cap(s.cells) < size {
		s.cells = make([]bool, size)
	}
	s.cells = s.cells[:size]
	clear(s.cells)
	return prev
}

// markMatchChanges redraws the rows whose highlight differs from the last
// frame. The highlight is drawn here, not by the terminal, so the render
// state's own dirty flags do not know about it.
func (tab *terminal) markMatchChanges(g grid, prev []bool) {
	if g.cols == 0 {
		return
	}
	for i := range max(len(prev), len(tab.search.cells)) {
		was := i < len(prev) && prev[i]
		is := i < len(tab.search.cells) && tab.search.cells[i]
		if was != is {
			tab.frame.redraw.mark(g.row(i))
		}
	}
}

// moveMatch steps to the next or previous match. The binding puts it in the
// screen's selection and brings the viewport to it.
func (tab *terminal) moveMatch(dir gostty.SearchDirection) error {
	if tab.search.handle == nil || tab.panels.Search.Matches == 0 {
		return nil
	}
	ok, err := tab.search.handle.Select(dir, gostty.SearchScrollNone)
	if err != nil || !ok {
		return err
	}
	// The binding only moves the search's position. Showing the match is
	// this UI's policy: it becomes the screen's selection, so it draws in the
	// selection colours and copies with the usual gesture, and the viewport
	// jumps to it only when it is off screen, so stepping between visible
	// matches does not move the page under the user.
	match, ok, err := tab.search.handle.SelectedMatch()
	if err != nil || !ok {
		return err
	}
	if err := tab.onScreen(func(screen *gostty.Screen) error {
		_, err := screen.SetSelection(match)
		return err
	}); err != nil {
		return err
	}
	return tab.revealRow(match.StartY)
}

// applyUIActions does the half of a panel result that is the terminal's: the
// native search. A settings step is left for `window.applyPanel`, which is
// the only place that can fan it out to every tab.
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
