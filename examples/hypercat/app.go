package main

import (
	"errors"
	"log"

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
	// The last title given to the window, so an unchanged one is not set again.
	title string
}

// newApp opens the window's shared state: the fonts are discovered once here,
// not once per tab.
func newApp() *terminalApp {
	dsf := deviceScale()
	return &terminalApp{dsf: dsf, settings: defaultAppearance(dsf), title: appName}
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
		tab.close()
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
		old.sel.dragging = false
		old.reports.mouseGrabbed = false
		old.cat.ClearHover()
	}
	app.active = index
	tab := app.current()
	tab.reports.focusedFrames = 0
	tab.redraw.markAll()
}

// cycleTab selects the tab `step` away from the active one, wrapping.
func (app *terminalApp) cycleTab(step int) {
	app.selectTab(wrap(app.active+step, len(app.tabs)))
}

func (app *terminalApp) closeTab(index int) {
	if index < 0 || index >= len(app.tabs) {
		return
	}
	app.tabs[index].close()
	copy(app.tabs[index:], app.tabs[index+1:])
	app.tabs[len(app.tabs)-1] = nil
	app.tabs = app.tabs[:len(app.tabs)-1]
	if index < app.active {
		app.active--
	}
	app.active = min(app.active, len(app.tabs)-1)
	if len(app.tabs) > 0 {
		app.selectTab(app.active)
	} else {
		app.active = 0
	}
}

func (app *terminalApp) close() {
	for len(app.tabs) > 0 {
		app.closeTab(len(app.tabs) - 1)
	}
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
	if tab.reports.focused {
		var err error
		if panelTook, err = app.updatePanels(tab, m); err != nil {
			return err
		}
	}
	return tab.updateInput(m, panelTook)
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

// syncTitle names the window after the tab the user is looking at; a background
// tab that renames itself is left to show up in the tab bar alone.
func (app *terminalApp) syncTitle(tab *terminalTab) {
	if title := tab.title.String(); title != app.title {
		app.title = title
		ebiten.SetWindowTitle(title)
	}
}

// updatePanels gives the tab's open panel this frame's keyboard and makes the
// change it asked for.
func (app *terminalApp) updatePanels(tab *terminalTab, m keys.Mods) (bool, error) {
	return app.applyPanel(tab, tab.panels.Handle(tab.panelInput(m)))
}

// applyPanel gives a panel result its effect, in two halves: the tab does what
// is its own -- the native search -- and the window applies the settings step,
// because the font, the theme and the cat the panel steps through are shared by
// every tab.
func (app *terminalApp) applyPanel(tab *terminalTab, result ui.Actions) (bool, error) {
	consumed, step, err := tab.applyUIActions(result)
	if err != nil || step.delta == 0 {
		return consumed, err
	}
	return consumed, app.settingsAdjust(step.row, step.delta)
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
	shortcut := (m.Ctrl && m.Shift) || m.Super
	if shortcut {
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

func (app *terminalApp) barHeight() float64 { return float64(int(ui.TabBarHeight * app.dsf)) }

func (app *terminalApp) LayoutF(width, height float64) (float64, float64) {
	app.dsf = deviceScale()
	app.width, app.height = width*app.dsf, height*app.dsf
	app.layoutTabs()
	return app.width, app.height
}

func (app *terminalApp) Layout(width, height int) (int, int) {
	w, h := app.LayoutF(float64(width), float64(height))
	return int(w), int(h)
}

func (app *terminalApp) layoutTabs() {
	// The faces are built in device pixels, so a window that moved to a display
	// with a different scale factor needs them rebuilt -- once, for every tab.
	if app.settings.dsf != app.dsf {
		app.applyFont()
	}
	for _, tab := range app.tabs {
		tab.offsetY = int(app.barHeight())
		if app.width > 0 {
			tab.layout(app.width, max(app.height-app.barHeight(), 1))
		}
	}
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
	tab.Draw(app.content, app.panelValues(tab))
	screen.Fill(tab.currentTheme().Panel)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(0, app.barHeight())
	screen.DrawImage(app.content, op)
	app.drawTabBar(screen)
}

// panelValues builds the labels the settings panel shows, and only while it is
// open: they are formatted strings, and a frame not showing them should not be
// building them.
func (app *terminalApp) panelValues(tab *terminalTab) ui.SettingsValues {
	if tab.panels.Mode != ui.Settings {
		return ui.SettingsValues{}
	}
	return app.settingsValues()
}

func (app *terminalApp) drawTabBar(screen *ebiten.Image) {
	canvas := app.current().canvas(screen)
	canvas.Width, canvas.Height = app.width, app.barHeight()
	app.tabBar.Draw(canvas, app.active, len(app.tabs), app.tabTitle)
}

func (app *terminalApp) tabTitle(index int) string { return app.tabs[index].title.program }
