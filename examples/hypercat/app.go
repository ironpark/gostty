package main

import (
	"errors"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

type clipboardState struct {
	clipboard       []byte
	systemClipboard bool
}

// terminalApp owns the window; each tab owns its terminal, shell, and UI state.
type terminalApp struct {
	tabs               []*terminalTab
	active             int
	tabBar             ui.TabBar
	width, height, dsf float64
	content            *ebiten.Image
	clipboardState
}

func (app *terminalApp) current() *terminalTab {
	if len(app.tabs) == 0 {
		return nil
	}
	return app.tabs[app.active]
}

func (app *terminalApp) addTab() error {
	previous := app.current()
	tab := &terminalTab{
		owner: app, clipboardState: &app.clipboardState,
		dsf: app.dsf, cols: initialCols, rows: initialRows,
	}
	if previous != nil {
		tab.settings = tabSettings{families: previous.settings.families, family: previous.settings.family, size: previous.settings.size, theme: previous.settings.theme}
		tab.fonts, tab.emoji = previous.fonts, previous.emoji
		tab.cols, tab.rows = previous.cols, previous.rows
	} else {
		tab.settings.families = fonts.Discover()
		family, index := fonts.DefaultFamily(tab.settings.families)
		tab.settings.family, tab.settings.size = index, fonts.DefaultSize
		tab.fonts = fonts.Load(family, fonts.DefaultSize*tab.dsf)
		tab.emoji = fonts.LoadEmoji()
	}
	if err := tab.start(); err != nil {
		tab.close()
		return err
	}
	if previous != nil {
		if tab.cat != nil && previous.cat != nil {
			tab.cat.SetEnabled(previous.cat.Enabled())
			tab.cat.SetHyper(previous.cat.Hyper())
		}
	}
	app.tabs = append(app.tabs, tab)
	app.selectTab(len(app.tabs) - 1)
	app.layoutTabs()
	return nil
}

func (app *terminalApp) selectTab(index int) {
	if index < 0 || index >= len(app.tabs) {
		return
	}
	if old := app.current(); old != nil {
		old.sel.dragging = false
		old.mouseGrabbed = false
		old.cat.ClearHover()
	}
	app.active = index
	tab := app.current()
	tab.focusedFrames = 0
	tab.redrawAll = true
	tab.retitle()
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

func (app *terminalApp) Update() error {
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
		if fed && tab.panels.Mode == ui.Search {
			if err := tab.runSearch(); err != nil {
				return err
			}
		}
		i++
	}
	if len(app.tabs) == 0 {
		return ebiten.Termination
	}
	consumed := false
	if ebiten.IsFocused() {
		var err error
		consumed, err = app.handleTabs(keys.Current())
		if err != nil {
			log.Printf("new tab: %v", err)
		}
	}
	if len(app.tabs) == 0 {
		return ebiten.Termination
	}
	for i, tab := range app.tabs {
		if err := tab.reportFocus(i == app.active && ebiten.IsFocused()); err != nil {
			return err
		}
	}
	tab := app.current()
	if consumed {
		return tab.refresh()
	}
	return tab.updateInput()
}

func (app *terminalApp) handleTabs(m keys.Mods) (bool, error) {
	if consumed, err := app.handleTabKeys(m, inpututil.IsKeyJustPressed); consumed || err != nil {
		return consumed, err
	}
	x, y := ebiten.CursorPosition()
	if y >= 0 && y < int(app.barHeight()) {
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
		_, wheel := ebiten.Wheel()
		if wheel != 0 {
			step := 1
			if wheel > 0 {
				step = -1
			}
			app.selectTab((app.active + step + len(app.tabs)) % len(app.tabs))
			return true, nil
		}
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
		step := 1
		if m.Shift {
			step = -1
		}
		app.selectTab((app.active + step + len(app.tabs)) % len(app.tabs))
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
	for _, tab := range app.tabs {
		tab.offsetY = int(app.barHeight())
		if app.width > 0 {
			tab.layout(app.width, max(app.height-app.barHeight(), 1), app.dsf)
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
	tab.Draw(app.content)
	screen.Fill(tab.currentTheme().Panel)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(0, app.barHeight())
	screen.DrawImage(app.content, op)
	app.drawTabBar(screen)
}

func (app *terminalApp) drawTabBar(screen *ebiten.Image) {
	canvas := app.current().canvas(screen)
	canvas.Width, canvas.Height = app.width, app.barHeight()
	app.tabBar.Draw(canvas, app.active, len(app.tabs), app.tabTitle)
}

func (app *terminalApp) tabTitle(index int) string { return app.tabs[index].title }
