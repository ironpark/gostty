package main

import (
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

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
