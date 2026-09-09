package main

import (
	"errors"
	"log"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// terminalApp owns the window and everything shared across tabs: the fonts and
// theme every tab is drawn with, and the clipboard. Each tab owns its terminal,
// shell, and UI state, and knows nothing about the window -- what a tab cannot
// do alone it reports, and this is where it is done.
type terminalApp struct {
	tabs               []*terminalTab
	active             int
	tabBar             ui.TabBar
	width, height, dsf float64
	content            *ebiten.Image
	settings           *appearance
	clipboard          sharedClipboard
	// The tab and the title the window is currently named after. Kept so that
	// the title is only formatted when a program actually renames itself:
	// Update runs sixty times a second, and `windowTitle.String` allocates.
	titled    windowTitle
	titledTab int
}

// newApp opens the window's shared state: the fonts are discovered once here,
// not once per tab.
func newApp() *terminalApp {
	dsf := deviceScale()
	return &terminalApp{dsf: dsf, settings: defaultAppearance(dsf)}
}

func (app *terminalApp) close() {
	for _, tab := range app.tabs {
		tab.close()
	}
	app.tabs = nil
	app.active = 0
	if app.content != nil {
		app.content.Deallocate()
		app.content = nil
	}
}

// Update runs one frame: every tab is serviced, the window takes the input that
// is its own, and what is left over goes to the tab the user is looking at.
func (app *terminalApp) Update() error {
	if err := app.serviceTabs(); err != nil {
		return err
	}
	if len(app.tabs) == 0 {
		return ebiten.Termination
	}

	m, focused := keys.Current(), ebiten.IsFocused()
	consumed := false
	if focused {
		var err error
		if consumed, err = app.handleTabs(m); err != nil {
			log.Printf("new tab: %v", err)
		}
		if len(app.tabs) == 0 {
			return ebiten.Termination
		}
	}
	for i, tab := range app.tabs {
		if err := tab.reportFocus(i == app.active && focused); err != nil {
			return err
		}
	}

	tab := app.current()
	app.syncTitle(tab)
	if consumed {
		return tab.refresh()
	}
	// The panels go before the shell does: an open one takes the keyboard, and
	// what it changes belongs to the window rather than to the tab it was
	// opened in.
	panelTook := false
	if focused {
		var err error
		if panelTook, err = app.applyPanel(tab, tab.panels.Handle(tab.panelInput(m))); err != nil {
			return err
		}
	}
	if err := tab.updateInput(m, panelTook); err != nil {
		return err
	}
	// Snapshot after input so selection is visible immediately. The cat then
	// walks on the same cells that Draw will render.
	if err := tab.refresh(); err != nil {
		return err
	}
	tab.updateCat()
	return nil
}

// serviceTabs feeds every tab what its shell wrote, background ones included,
// and drops the tabs whose shell has exited.
func (app *terminalApp) serviceTabs() error {
	for i := 0; i < len(app.tabs); {
		tab := app.tabs[i]
		fed, err := tab.readOutput()
		if errors.Is(err, ebiten.Termination) {
			app.closeTab(i)
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

// applyPanel gives a panel result its effect, in two halves: the tab does what
// is its own -- the native search -- and the window applies the settings step,
// because the font, the theme and the cat the panel steps through are shared by
// every tab.
func (app *terminalApp) applyPanel(tab *terminalTab, result ui.Actions) (bool, error) {
	consumed, err := tab.applyUIActions(result)
	if err != nil || result.SettingsDelta == 0 {
		return consumed, err
	}
	return consumed, app.settingsAdjust(tab.panels.Settings.Row, result.SettingsDelta)
}

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

func (app *terminalApp) Draw(screen *ebiten.Image) {
	tab := app.current()
	if tab == nil {
		return
	}
	w, h := screen.Bounds().Dx(), max(screen.Bounds().Dy()-int(app.barHeight()), 1)
	if app.content == nil || app.content.Bounds().Dx() != w || app.content.Bounds().Dy() != h {
		if app.content != nil {
			app.content.Deallocate()
		}
		app.content = ebiten.NewImage(w, h)
	}
	tab.Draw(app.content)
	app.drawPanels(tab)
	screen.Fill(tab.currentTheme().Panel)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(0, app.barHeight())
	screen.DrawImage(app.content, op)
	app.drawTabBar(screen)
}

// drawPanels paints whichever panel the tab has open over its content. The
// window draws it, because what the settings panel displays -- the font, the
// theme, the cat -- is the window's, and the labels are only formatted while it
// is open: a frame not showing them should not be building them.
func (app *terminalApp) drawPanels(tab *terminalTab) {
	values := ui.SettingsValues{}
	if tab.panels.Mode == ui.Settings {
		values = app.settingsValues()
	}
	tab.panels.Draw(tab.canvas(app.content), values)
}

func (app *terminalApp) drawTabBar(screen *ebiten.Image) {
	canvas := app.current().canvas(screen)
	canvas.Width, canvas.Height = app.width, app.barHeight()
	app.tabBar.Draw(canvas, app.active, len(app.tabs), app.tabTitle)
}

func (app *terminalApp) tabTitle(index int) string { return app.tabs[index].title.program }
