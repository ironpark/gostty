package main

import (
	"github.com/ironpark/gostty/examples/hypercat/internal/frontend"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// window connects the frame loop to tabs and shared window resources.
// window_* files own tabs, panel routing, and settings shared by every terminal.
type window struct {
	tabs               []*terminal
	active             int
	tabBar             ui.TabBar
	width, height, dsf float64
	input              frontend.Input
	windowTitle        string
	settings           *appearance
	clipboard          sharedClipboard
	// The tab and the title the window is currently named after. Kept so that
	// the title is only formatted when a program actually renames itself:
	// Update runs sixty times a second, and `windowTitle.String` allocates.
	titled    windowTitle
	titledTab int
}

// newWindow opens the window's shared state: the fonts are discovered once here,
// not once per tab.
func newWindow() *window {
	dsf := frontend.DeviceScale()
	return &window{dsf: dsf, settings: defaultAppearance(dsf), windowTitle: appName}
}

func (win *window) close() {
	for _, tab := range win.tabs {
		tab.close()
	}
	win.tabs = nil
	win.active = 0
}

// Update runs one frame: every tab is serviced, the window takes the input that
// is its own, and what is left over goes to the tab the user is looking at.
func (win *window) Update(in frontend.Input) error {
	win.input = in
	if err := win.serviceTabs(); err != nil {
		return err
	}
	if len(win.tabs) == 0 {
		return frontend.ErrClosed
	}

	tabInputConsumed, err := win.routeInput(in.Mods, in.Focused)
	if err != nil {
		return err
	}
	// Snapshot after input so selection is visible immediately. The cat then
	// walks on the same cells that Draw will render.
	tab := win.current()
	if err := tab.refreshSnapshot(); err != nil {
		return err
	}
	if !tabInputConsumed {
		tab.updateCat()
	}
	return nil
}

func (win *window) barHeight() float64 { return float64(int(ui.TabBarHeight * win.dsf)) }

// Resize receives content dimensions in device pixels from the frontend.
func (win *window) Resize(width, height, scale float64) {
	win.dsf, win.width, win.height = scale, width, height
	win.layoutTabs()
}

func (win *window) layoutTabs() {
	// The faces are built in device pixels, so a window that moved to a display
	// with a different scale factor needs them rebuilt -- once, for every tab.
	if win.settings.dsf != win.dsf {
		win.applyFont()
	}
	for _, tab := range win.tabs {
		tab.offsetY = int(win.barHeight())
		if win.width > 0 {
			tab.layout(win.width, max(win.height-win.barHeight(), 1))
		}
	}
}
