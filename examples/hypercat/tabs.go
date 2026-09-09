package main

import (
	"slices"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

func (app *terminalApp) current() *terminalTab {
	if len(app.tabs) == 0 {
		return nil
	}
	return app.tabs[app.active]
}

func (app *terminalApp) addTab() error {
	tab := app.newTab()
	if err := tab.start(); err != nil {
		return err
	}
	app.tabs = append(app.tabs, tab)
	app.selectTab(len(app.tabs) - 1)
	app.layoutTabs()
	return nil
}

// newTab builds a tab that opens looking like the one it was opened from. The
// fonts, the theme and the clipboard are the window's, so it only has to take
// the grid: a new tab starts at the size the visible one is, before Layout has
// measured it.
func (app *terminalApp) newTab() *terminalTab {
	tab := &terminalTab{
		clipboard: &app.clipboard, settings: app.settings,
		cols: initialCols, rows: initialRows,
	}
	if previous := app.current(); previous != nil {
		tab.cols, tab.rows = previous.cols, previous.rows
	}
	return tab
}

func (app *terminalApp) selectTab(index int) {
	if index < 0 || index >= len(app.tabs) {
		return
	}
	if old := app.current(); old != nil {
		old.deactivate()
	}
	app.active = index
	app.current().activate()
}

// cycleTab selects the tab `step` away from the active one, wrapping.
func (app *terminalApp) cycleTab(step int) {
	app.selectTab(wrap(app.active+step, len(app.tabs)))
}

// closeTab preserves the active tab's interaction state when a background
// shell exits. Only closing the active tab activates its replacement.
func (app *terminalApp) closeTab(index int) {
	if index < 0 || index >= len(app.tabs) {
		return
	}
	wasActive := index == app.active
	app.tabs[index].close()
	app.tabs = slices.Delete(app.tabs, index, index+1)
	if len(app.tabs) == 0 {
		app.active = 0
		return
	}
	if index < app.active {
		app.active--
	}
	app.active = min(app.active, len(app.tabs)-1)
	if wasActive {
		app.current().activate()
	}
}

// handleTabs consumes the window's own input -- tab shortcuts and the tab
// bar -- before it can reach a shell, and reports whether it did.
func (app *terminalApp) handleTabs(m keys.Mods) (bool, error) {
	if consumed, err := app.handleTabKeys(m, inpututil.IsKeyJustPressed); consumed || err != nil {
		return consumed, err
	}
	return app.handleTabBar()
}

// handleTabBar answers clicks and wheel turns over the bar.
func (app *terminalApp) handleTabBar() (bool, error) {
	x, y := ebiten.CursorPosition()
	if y < 0 || y >= int(app.barHeight()) {
		return false, nil
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		layout := app.tabBar.Layout(app.width, app.dsf, app.active, len(app.tabs))
		action := layout.Hit(float64(x), float64(y))
		switch action.Kind {
		case ui.TabAdd:
			return true, app.addTab()
		case ui.TabClose:
			app.closeTab(action.Index)
		case ui.TabSelect:
			app.selectTab(action.Index)
		}
		return true, nil
	}
	if _, wheel := ebiten.Wheel(); wheel > 0 {
		app.cycleTab(-1)
		return true, nil
	} else if wheel < 0 {
		app.cycleTab(1)
		return true, nil
	}
	return false, nil
}

// handleTabKeys consumes window shortcuts before they can reach a shell.
func (app *terminalApp) handleTabKeys(m keys.Mods, pressed func(ebiten.Key) bool) (bool, error) {
	if m.Shortcut() {
		switch {
		case pressed(ebiten.KeyT):
			return true, app.addTab()
		case pressed(ebiten.KeyW):
			app.closeTab(app.active)
			return true, nil
		}
		for i, key := range []ebiten.Key{ebiten.KeyDigit1, ebiten.KeyDigit2, ebiten.KeyDigit3, ebiten.KeyDigit4, ebiten.KeyDigit5, ebiten.KeyDigit6, ebiten.KeyDigit7, ebiten.KeyDigit8, ebiten.KeyDigit9} {
			if pressed(key) {
				if i == 8 {
					i = len(app.tabs) - 1
				}
				app.selectTab(i)
				return true, nil
			}
		}
	}
	if m.Ctrl && pressed(ebiten.KeyTab) {
		if m.Shift {
			app.cycleTab(-1)
		} else {
			app.cycleTab(1)
		}
		return true, nil
	}
	return false, nil
}

// syncTitle names the window after the tab the user is looking at; a background
// tab that renames itself is left to show up in the tab bar alone. The window
// opens under `appName`, which is what an untouched `windowTitle` comes to, so
// a shell that never sets a title never reaches the platform call at all.
func (app *terminalApp) syncTitle(tab *terminalTab) {
	if tab.title == app.titled && app.active == app.titledTab {
		return
	}
	app.titled, app.titledTab = tab.title, app.active
	ebiten.SetWindowTitle(tab.title.String())
}

func (app *terminalApp) drawTabBar(screen *ebiten.Image) {
	canvas := app.current().canvas(screen)
	canvas.Width, canvas.Height = app.width, app.barHeight()
	app.tabBar.Draw(canvas, app.active, len(app.tabs), app.tabTitle)
}

func (app *terminalApp) tabTitle(index int) string { return app.tabs[index].title.program }
