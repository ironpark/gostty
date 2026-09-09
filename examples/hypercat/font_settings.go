package main

import "github.com/ironpark/gostty/examples/hypercat/fonts"

// setFont swaps the faces the window draws with and lets every tab follow.
//
// Only the faces change here. The cell size comes out of them, so the next
// Layout works out how many columns and rows the window now holds and resizes
// each terminal and its pty to match -- which is the same path a window resize
// takes, so the programs are told the way they expect.
func (app *terminalApp) setFont(family int, size float64) {
	size = min(max(size, fonts.MinSize), fonts.MaxSize)
	if len(app.settings.families) == 0 {
		return
	}
	family = app.settings.clampFamily(family)
	if family == app.settings.family && size == app.settings.size {
		return
	}
	app.settings.family, app.settings.size = family, size
	app.applyFont()
}

// applyFont rebuilds the faces from what the settings hold, once for the window,
// and tells every tab that what it drew last frame no longer matches them.
//
// This is also called when the window moves to a display with a different scale
// factor, since the faces are built in device pixels.
func (app *terminalApp) applyFont() {
	app.settings.loadFonts(app.dsf)
	for _, tab := range app.tabs {
		tab.redraw.markAll()
		// The grid is measured in cells and the cell just changed shape, so the
		// window holds a different number of them. Layout is where that is
		// worked out; this only has to say that the answer it cached is stale,
		// because the column count can survive a size change while the pixel
		// geometry the image protocol measures in does not.
		tab.relayout = true
	}
}
