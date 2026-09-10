package main

import (
	"log"

	"github.com/ironpark/gostty/examples/hypercat/internal/appearance"
	"github.com/ironpark/gostty/examples/hypercat/internal/desktop"
	"github.com/ironpark/gostty/examples/hypercat/internal/frontend"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// window connects the frame loop to tabs and shared window resources.
// Tab lifecycle lives in window_tabs.go; shared settings are applied in settings.go.
type window struct {
	tabs               []*terminal
	active             int
	tabBar             ui.TabBar
	width, height, dsf float64
	input              frontend.Input
	windowTitle        string
	settings           *appearance.State
	clipboard          desktop.Clipboard
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
	return &window{dsf: dsf, settings: appearance.Default(dsf), windowTitle: appName}
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

// routeInput applies input priority: tab bar/shortcuts, panels, then terminal.
// It runs after serviceTabs, while at least one terminal is alive. Closing the
// last tab returns Termination before any code can use the active terminal.
// The result is true when tab input consumed the frame; those frames refresh
// the visible snapshot without advancing the cat animation.
func (win *window) routeInput(m keys.Mods, focused bool) (bool, error) {
	consumed := false
	if focused {
		var err error
		if consumed, err = win.handleTabs(m); err != nil {
			log.Printf("new tab: %v", err)
		}
		if len(win.tabs) == 0 {
			return consumed, frontend.ErrClosed
		}
	}
	for i, tab := range win.tabs {
		tab.input = win.input
		if err := tab.reportFocus(i == win.active && focused); err != nil {
			return consumed, err
		}
	}

	tab := win.current()
	win.syncTitle(tab)
	if consumed {
		return true, nil
	}
	panelTook := false
	if focused {
		var err error
		if panelTook, err = win.applyPanel(tab, tab.panels.Handle(win.panelInput(tab))); err != nil {
			return false, err
		}
	}
	return false, tab.updateInput(m, panelTook)
}

// applyPanel gives a panel result its effect, in two halves: the tab does what
// is its own -- the native search -- and the window applies the settings step,
// because the font, the theme and the cat the panel steps through are shared by
// every tab.
func (win *window) applyPanel(tab *terminal, result ui.Actions) (bool, error) {
	consumed, err := tab.applyUIActions(result)
	if err != nil || result.SettingsDelta == 0 {
		return consumed, err
	}
	return consumed, win.settingsAdjust(tab.panels.Settings.Row, result.SettingsDelta)
}

// panelInput omits typing unless search already owned the keyboard this frame.
func (win *window) panelInput(tab *terminal) ui.Input {
	in := win.input.Panel
	if tab.panels.Mode != ui.Search {
		in.Chars = nil
	}
	return in
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
	if win.settings.Scale() != win.dsf {
		win.applyFont()
	}
	for _, tab := range win.tabs {
		tab.offsetY = int(win.barHeight())
		if win.width > 0 {
			tab.layout(win.width, max(win.height-win.barHeight(), 1))
		}
	}
}

var _ frontend.Application = (*window)(nil)

// Present supplies cached terminal data to the window. There are no gostty
// calls here: Ebitengine may draw without an intervening terminal update.
func (win *window) Present() frontend.Presentation {
	tab := win.current()
	if tab == nil {
		return frontend.Presentation{}
	}
	settings := ui.SettingsValues{}
	if tab.panels.Mode == ui.Settings {
		settings = win.settingsValues()
	}
	return frontend.Presentation{
		Renderer: &tab.renderer, Frame: tab.presentation(),
		Panels: &tab.panels, Settings: settings,
		TabBar: &win.tabBar, Active: win.active, Count: len(win.tabs), TabTitle: win.tabTitle,
		Title: win.windowTitle, LinkPointer: tab.reports.focused && tab.frame.link.valid(),
	}
}
