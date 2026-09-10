package main

import (
	"errors"
	"io"
	"slices"

	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/input"
)

func (win *window) current() *terminal {
	if len(win.tabs) == 0 {
		return nil
	}
	return win.tabs[win.active]
}

func (win *window) addTab() error {
	tab := win.newTab()
	if err := tab.start(); err != nil {
		return err
	}
	win.tabs = append(win.tabs, tab)
	win.selectTab(len(win.tabs) - 1)
	win.layoutTabs()
	return nil
}

// newTab builds a tab that opens looking like the one it was opened from. The
// fonts, the theme and the clipboard are the window's, so it only has to take
// the grid: a new tab starts at the size the visible one is, before Layout has
// measured it.
func (win *window) newTab() *terminal {
	tab := &terminal{
		clipboard: &win.clipboard, settings: win.settings,
		cols: initialCols, rows: initialRows,
	}
	if previous := win.current(); previous != nil {
		tab.cols, tab.rows = previous.cols, previous.rows
	}
	return tab
}

func (win *window) selectTab(index int) {
	if index < 0 || index >= len(win.tabs) {
		return
	}
	if old := win.current(); old != nil {
		old.deactivate()
	}
	win.active = index
	win.current().activate()
}

// cycleTab selects the tab `step` away from the active one, wrapping.
func (win *window) cycleTab(step int) {
	win.selectTab(wrap(win.active+step, len(win.tabs)))
}

// closeTab preserves the active tab's interaction state when a background
// shell exits. Only closing the active tab activates its replacement.
func (win *window) closeTab(index int) {
	if index < 0 || index >= len(win.tabs) {
		return
	}
	wasActive := index == win.active
	win.tabs[index].close()
	win.tabs = slices.Delete(win.tabs, index, index+1)
	if len(win.tabs) == 0 {
		win.active = 0
		return
	}
	if index < win.active {
		win.active--
	}
	win.active = min(win.active, len(win.tabs)-1)
	if wasActive {
		win.current().activate()
	}
}

// handleTabs consumes the window's own input -- tab shortcuts and the tab
// bar -- before it can reach a shell, and reports whether it did.
func (win *window) handleTabs(m keys.Mods) (bool, error) {
	if consumed, err := win.handleTabKeys(m, win.input.KeyPressed); consumed || err != nil {
		return consumed, err
	}
	return win.handleTabBar()
}

// handleTabBar answers clicks and wheel turns over the bar.
func (win *window) handleTabBar() (bool, error) {
	x, y := win.input.X, win.input.Y
	if y < 0 || y >= int(win.barHeight()) {
		return false, nil
	}
	if win.input.Left.Pressed {
		layout := win.tabBar.Layout(win.width, win.dsf, win.active, len(win.tabs))
		action := layout.Hit(float64(x), float64(y))
		switch action.Kind {
		case ui.TabAdd:
			return true, win.addTab()
		case ui.TabClose:
			win.closeTab(action.Index)
		case ui.TabSelect:
			win.selectTab(action.Index)
		}
		return true, nil
	}
	if wheel := win.input.Wheel; wheel > 0 {
		win.cycleTab(-1)
		return true, nil
	} else if wheel < 0 {
		win.cycleTab(1)
		return true, nil
	}
	return false, nil
}

// handleTabKeys consumes window shortcuts before they can reach a shell.
func (win *window) handleTabKeys(m keys.Mods, pressed func(input.Key) bool) (bool, error) {
	if m.Shortcut() {
		switch {
		case pressed(input.KeyKeyT):
			return true, win.addTab()
		case pressed(input.KeyKeyW):
			win.closeTab(win.active)
			return true, nil
		}
		for i, key := range []input.Key{input.KeyDigit1, input.KeyDigit2, input.KeyDigit3, input.KeyDigit4, input.KeyDigit5, input.KeyDigit6, input.KeyDigit7, input.KeyDigit8, input.KeyDigit9} {
			if pressed(key) {
				if i == 8 {
					i = len(win.tabs) - 1
				}
				win.selectTab(i)
				return true, nil
			}
		}
	}
	if m.Ctrl && pressed(input.KeyTab) {
		if m.Shift {
			win.cycleTab(-1)
		} else {
			win.cycleTab(1)
		}
		return true, nil
	}
	return false, nil
}

// syncTitle names the window after the tab the user is looking at; a background
// tab that renames itself is left to show up in the tab bar alone. The window
// opens under `appName`, which is what an untouched `windowTitle` comes to, so
// a shell that never sets a title never reaches the platform call at all.
func (win *window) syncTitle(tab *terminal) {
	if tab.title == win.titled && win.active == win.titledTab {
		return
	}
	win.titled, win.titledTab = tab.title, win.active
	win.windowTitle = tab.title.String()
}

func (win *window) tabTitle(index int) string { return win.tabs[index].title.program }

// serviceTabs feeds every tab what its shell wrote, background ones included,
// and drops the tabs whose shell has exited.
func (win *window) serviceTabs() error {
	for i := 0; i < len(win.tabs); {
		tab := win.tabs[i]
		fed, err := tab.readOutput()
		if errors.Is(err, io.EOF) {
			win.closeTab(i)
			continue
		}
		if err != nil {
			return err
		}
		// New output is new scrollback, which an open search has to rescan.
		if fed && tab.panels.Mode == ui.Search {
			if err := tab.runSearch(); err != nil {
				return err
			}
		}
		i++
	}
	return nil
}

// wrap brings an index back into [0, n).
func wrap(i, n int) int { return (i%n + n) % n }
