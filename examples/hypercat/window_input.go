package main

import (
	"log"

	"github.com/ironpark/gostty/examples/hypercat/internal/frontend"
	"github.com/ironpark/gostty/examples/hypercat/keys"
)

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
