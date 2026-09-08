package main

import "github.com/ironpark/gostty/examples/hypercat/fonts"

// setFont swaps the faces and lets the window follow.
//
// Only the faces change here. The cell size comes out of them, so the next
// Layout works out how many columns and rows the window now holds and resizes
// the terminal and the pty to match -- which is the same path a window resize
// takes, so the program is told the way it expects.
func (tab *terminalTab) setFont(family int, size float64) error {
	size = min(max(size, fonts.MinSize), fonts.MaxSize)
	if len(tab.settings.families) == 0 {
		return nil
	}
	family = min(max(family, 0), len(tab.settings.families)-1)
	if family == tab.settings.family && size == tab.settings.size {
		return nil
	}
	tab.settings.family, tab.settings.size = family, size
	tab.applyFont()
	return nil
}

// applyFont rebuilds the faces from what the settings hold.
//
// The size in the panel is in device-independent pixels, because that is what a
// user means by "14px"; what the face is asked for is that times the display's
// scale factor. This is also called when the window moves to a display with a
// different one.
func (tab *terminalTab) applyFont() {
	tab.redrawAll = true
	var family *fonts.Family
	if len(tab.settings.families) > 0 {
		family = tab.settings.families[min(tab.settings.family, len(tab.settings.families)-1)]
	}
	tab.fonts = fonts.Load(family, tab.settings.size*tab.dsf)
	// The grid is measured in cells and the cell just changed shape, so the
	// window holds a different number of them. Layout is where that is worked
	// out; this only has to say that the answer it cached is stale, because the
	// column count can survive a size change while the pixel geometry the image
	// protocol measures in does not.
	tab.relayout = true
}
