package main

import (
	"errors"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// terminalApp connects the frame loop to tabs and shared window resources.
// Tab-bar behavior lives in tabs.go; panel behavior lives in panels.go.
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
