package main

import "github.com/ironpark/gostty"

// closeSearch releases the native handle before the terminal it belongs to.
func (tab *terminalTab) closeSearch() {
	if tab.search != nil {
		_ = tab.search.Close()
		tab.search = nil
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
func (tab *terminalTab) runSearch() error {
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
	tab.search = search
	return nil
}

// tickSearch pushes the scan forward by one frame's worth and refreshes the
// count. `Tick` does not read the terminal, so most of the work here is the
// kind a real emulator would do off the IO thread; `Feed` is what hands it
// more scrollback and notices that the viewport moved.
func (tab *terminalTab) tickSearch() error {
	if tab.search == nil {
		return nil
	}
	if err := tab.search.Feed(true); err != nil {
		return err
	}
	// A budget rather than a loop to completion: whatever is not finished this
	// frame is finished on the next, and the matches already found are drawn
	// in the meantime.
	for range searchTicksPerFrame {
		progress, err := tab.search.Tick()
		if err != nil {
			return err
		}
		if progress != gostty.SearchProgressProgress {
			break
		}
	}
	var err error
	tab.panels.Search.Matches, err = tab.search.MatchCount()
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
func (tab *terminalTab) refreshMatches() error {
	if tab.search == nil {
		prev := tab.matchCells
		tab.matchCells = tab.matchCells[:0]
		tab.markMatchChanges(prev)
		return nil
	}
	screen, err := tab.vt.ActiveScreen()
	if err != nil {
		return err
	}
	top, err := screen.ViewportTop()
	if err != nil {
		return err
	}
	// The count is not known in advance. A match spans at least one cell, so
	// one per viewport cell cannot be exceeded by anything that is on screen.
	if cap(tab.viewportMatches) < len(tab.cells) {
		tab.viewportMatches = make([]gostty.Selection, len(tab.cells))
	}
	n, err := tab.search.ViewportMatches(tab.viewportMatches[:len(tab.cells)])
	if err != nil {
		return err
	}
	prev := append([]bool(nil), tab.matchCells...)
	if cap(tab.matchCells) < len(tab.cells) {
		tab.matchCells = make([]bool, len(tab.cells))
	}
	tab.matchCells = tab.matchCells[:len(tab.cells)]
	clear(tab.matchCells)
	defer tab.markMatchChanges(prev)
	for _, m := range tab.viewportMatches[:n] {
		if m.StartY < top || m.EndY >= top+uint32(tab.rows) || m.StartY > m.EndY {
			continue
		}
		for y := m.StartY; y <= m.EndY; y++ {
			x0, x1 := 0, tab.cols-1
			if y == m.StartY {
				x0 = int(m.StartX)
			}
			if y == m.EndY {
				x1 = int(m.EndX)
			}
			row := int(y-top) * tab.cols
			for x := x0; x <= x1 && x < tab.cols; x++ {
				tab.matchCells[row+x] = true
			}
		}
	}
	return nil
}

// markMatchChanges redraws the rows whose highlight differs from the last
// frame. The highlight is drawn here, not by the terminal, so the render
// state's own dirty flags do not know about it.
func (tab *terminalTab) markMatchChanges(prev []bool) {
	for i := range tab.matchCells {
		was := i < len(prev) && prev[i]
		if was != tab.matchCells[i] && tab.cols > 0 {
			row := i / tab.cols
			if row < len(tab.rowDirty) {
				tab.rowDirty[row] = true
			}
		}
	}
	for i := len(tab.matchCells); i < len(prev); i++ {
		if prev[i] && tab.cols > 0 && i/tab.cols < len(tab.rowDirty) {
			tab.rowDirty[i/tab.cols] = true
		}
	}
}

// moveMatch steps to the next or previous match. The binding puts it in the
// screen's selection and brings the viewport to it.
func (tab *terminalTab) moveMatch(dir gostty.SearchDirection) error {
	if tab.search == nil || tab.panels.Search.Matches == 0 {
		return nil
	}
	ok, err := tab.search.Select(dir, gostty.SearchScrollNone)
	if err != nil || !ok {
		return err
	}
	// The binding only moves the search's position. Showing the match is
	// this UI's policy: it becomes the screen's selection, so it draws in the
	// selection colours and copies with the usual gesture, and the viewport
	// jumps to it only when it is off screen, so stepping between visible
	// matches does not move the page under the user.
	match, ok, err := tab.search.SelectedMatch()
	if err != nil || !ok {
		return err
	}
	screen, err := tab.vt.ActiveScreen()
	if err != nil {
		return err
	}
	if _, err := screen.SetSelection(match); err != nil {
		return err
	}
	top, err := screen.ViewportTop()
	if err != nil {
		return err
	}
	if match.StartY < top || match.StartY >= top+uint32(tab.rows) {
		return tab.vt.ScrollViewport(gostty.ScrollViewportRow(uint(match.StartY)))
	}
	return nil
}
